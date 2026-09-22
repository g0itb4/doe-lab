-- Time series: hypertables, partitioned on their time column.
--
-- All times in these tables are FEEDER TIME: the clock of the simulation,
-- which equals wall-clock time at speed 1 and runs ahead of it when the demo
-- is accelerated. Audit columns (created_at, received_at) are wall-clock.

CREATE TABLE site_profiles (
  site_id           uuid             NOT NULL,
  ts                timestamptz      NOT NULL,
  load_w            double precision NOT NULL,
  pv_w              double precision NOT NULL,
  controlled_load_w double precision NOT NULL DEFAULT 0,
  CONSTRAINT site_profiles_pkey PRIMARY KEY (site_id, ts),
  CONSTRAINT site_profiles_site_fkey FOREIGN KEY (site_id) REFERENCES sites (id) ON DELETE CASCADE,
  CONSTRAINT site_profiles_not_negative CHECK (load_w >= 0 AND pv_w >= 0 AND controlled_load_w >= 0),
  -- Half-hourly data: every row starts on a half hour.
  CONSTRAINT site_profiles_on_half_hour CHECK (extract(epoch FROM ts)::bigint % 1800 = 0)
);

SELECT create_hypertable('site_profiles', by_range('ts', INTERVAL '30 days'));

COMMENT ON TABLE site_profiles IS
  'Half-hourly load and PV of each site, imported from the Ausgrid Solar Home data. It is the forecast the engine works from and what the simulated devices replay.';
COMMENT ON COLUMN site_profiles.ts IS 'Start of the half-hour interval, in the profile year (1 July 2010 to 30 June 2011, Sydney time), stored in UTC.';
COMMENT ON COLUMN site_profiles.load_w IS 'Average general consumption over the interval.';
COMMENT ON COLUMN site_profiles.pv_w IS 'Average gross PV generation over the interval, before pv_scale.';
COMMENT ON COLUMN site_profiles.controlled_load_w IS 'Average controlled load (off-peak hot water) over the interval.';

CREATE TYPE envelope_source AS ENUM ('engine', 'backstop');
CREATE TYPE binding_constraint AS ENUM ('none', 'voltage_high', 'voltage_low', 'transformer', 'line', 'site_cap');

CREATE TABLE envelopes (
  id                     uuid               NOT NULL DEFAULT uuidv7(),
  site_id                uuid               NOT NULL,
  valid_from             timestamptz        NOT NULL,
  valid_to               timestamptz        NOT NULL,
  export_limit_w         double precision   NOT NULL,
  import_limit_w         double precision   NOT NULL,
  source                 envelope_source    NOT NULL,
  envelope_run_id        uuid,
  backstop_event_id      uuid,
  export_binding         binding_constraint NOT NULL DEFAULT 'none',
  export_binding_element text               NOT NULL DEFAULT '',
  import_binding         binding_constraint NOT NULL DEFAULT 'none',
  import_binding_element text               NOT NULL DEFAULT '',
  superseded_at          timestamptz,
  created_at             timestamptz        NOT NULL DEFAULT now(),
  -- The partition column must be part of every unique key of a hypertable.
  CONSTRAINT envelopes_pkey PRIMARY KEY (site_id, valid_from, id),
  CONSTRAINT envelopes_site_fkey FOREIGN KEY (site_id) REFERENCES sites (id) ON DELETE CASCADE,
  CONSTRAINT envelopes_run_fkey FOREIGN KEY (envelope_run_id) REFERENCES envelope_runs (id) ON DELETE CASCADE,
  CONSTRAINT envelopes_backstop_fkey
    FOREIGN KEY (backstop_event_id) REFERENCES backstop_events (id) ON DELETE CASCADE,
  CONSTRAINT envelopes_limits_not_negative CHECK (export_limit_w >= 0 AND import_limit_w >= 0),
  CONSTRAINT envelopes_period_ordered CHECK (valid_to > valid_from),
  -- Intervals sit on a 5-minute grid and last 5, 15, 30 or 60 minutes. With
  -- one interval length per feeder, two active envelopes of a site cannot
  -- overlap without sharing a valid_from, which the unique index below
  -- forbids. (A hypertable cannot carry an exclusion constraint.)
  CONSTRAINT envelopes_on_grid CHECK (extract(epoch FROM valid_from)::bigint % 300 = 0),
  CONSTRAINT envelopes_period_allowed
    CHECK (valid_to - valid_from IN (interval '5 minutes', interval '15 minutes', interval '30 minutes', interval '60 minutes')),
  -- Exactly one origin, and it matches the source.
  CONSTRAINT envelopes_engine_has_run CHECK ((source = 'engine') = (envelope_run_id IS NOT NULL)),
  CONSTRAINT envelopes_backstop_has_event CHECK ((source = 'backstop') = (backstop_event_id IS NOT NULL))
);

SELECT create_hypertable('envelopes', by_range('valid_from', INTERVAL '7 days'));

-- One ACTIVE envelope per site and interval. Publishing again supersedes the
-- old row first; the old row stays for audit.
CREATE UNIQUE INDEX envelopes_one_active_key ON envelopes (site_id, valid_from) WHERE superseded_at IS NULL;
-- ListEnvelopes by run, and the export of a run.
CREATE INDEX envelopes_run_idx ON envelopes (envelope_run_id, valid_from, site_id);
CREATE INDEX envelopes_backstop_idx ON envelopes (backstop_event_id) WHERE backstop_event_id IS NOT NULL;

COMMENT ON TABLE envelopes IS
  'The operating envelope of a site for one interval: the most it may export and import at its connection point. Rows are immutable; a newer envelope supersedes an older one, which is kept for audit.';
COMMENT ON COLUMN envelopes.valid_from IS 'Start of the interval, inclusive, in feeder time.';
COMMENT ON COLUMN envelopes.valid_to IS 'End of the interval, exclusive, in feeder time.';
COMMENT ON COLUMN envelopes.export_limit_w IS 'Largest net export at the connection point. CSIP-AUS opModExpLimW.';
COMMENT ON COLUMN envelopes.import_limit_w IS 'Largest net import at the connection point. CSIP-AUS opModImpLimW.';
COMMENT ON COLUMN envelopes.export_binding IS 'What stops the export limit from being larger.';
COMMENT ON COLUMN envelopes.export_binding_element IS 'The site, line or transformer where export_binding applies.';
COMMENT ON COLUMN envelopes.import_binding IS 'What stops the import limit from being larger.';
COMMENT ON COLUMN envelopes.import_binding_element IS 'The site, line or transformer where import_binding applies.';
COMMENT ON COLUMN envelopes.superseded_at IS 'Wall-clock time a newer envelope replaced this one. NULL while it is the active envelope.';

-- An envelope is a record of what was sent to a device. The only change it
-- may undergo is being superseded, once.
CREATE FUNCTION envelopes_supersede_only() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'envelopes are immutable (attempted DELETE)' USING ERRCODE = 'restrict_violation';
  END IF;
  IF OLD.superseded_at IS NOT NULL
     OR NEW.superseded_at IS NULL
     OR (to_jsonb(NEW) - 'superseded_at') IS DISTINCT FROM (to_jsonb(OLD) - 'superseded_at') THEN
    RAISE EXCEPTION 'envelopes are immutable: only superseded_at may be set, once'
      USING ERRCODE = 'restrict_violation';
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER envelopes_immutable
  BEFORE UPDATE OR DELETE ON envelopes
  FOR EACH ROW EXECUTE FUNCTION envelopes_supersede_only();

CREATE TABLE readings (
  device_id    uuid             NOT NULL,
  site_id      uuid             NOT NULL,
  ts           timestamptz      NOT NULL,
  power_w      double precision NOT NULL,
  net_export_w double precision NOT NULL,
  soc_pct      double precision,
  voltage_v    double precision,
  received_at  timestamptz      NOT NULL DEFAULT now(),
  -- Also what makes ingest idempotent: a reading sent twice is one row.
  CONSTRAINT readings_pkey PRIMARY KEY (device_id, ts),
  -- The device belongs to the site the reading names.
  CONSTRAINT readings_device_fkey FOREIGN KEY (device_id, site_id) REFERENCES devices (id, site_id) ON DELETE CASCADE,
  CONSTRAINT readings_soc_range CHECK (soc_pct BETWEEN 0 AND 100),
  CONSTRAINT readings_voltage_positive CHECK (voltage_v > 0),
  CONSTRAINT readings_power_plausible CHECK (abs(power_w) <= 1e6 AND abs(net_export_w) <= 1e6)
);

SELECT create_hypertable('readings', by_range('ts', INTERVAL '1 day'));

-- ListReadings by site, and the compliance join.
CREATE INDEX readings_site_ts_idx ON readings (site_id, ts DESC);

COMMENT ON TABLE readings IS
  'Telemetry from a device: its own power, and the net flow at the site''s connection point as its meter sees it.';
COMMENT ON COLUMN readings.ts IS 'Feeder time of the measurement.';
COMMENT ON COLUMN readings.power_w IS 'The device''s own power. Positive is towards the grid: PV generating, a battery discharging. An EV charging is negative.';
COMMENT ON COLUMN readings.net_export_w IS 'Net power at the site''s connection point. Positive is export. This is what an export limit applies to.';
COMMENT ON COLUMN readings.soc_pct IS 'State of charge, for a battery or an EV.';
COMMENT ON COLUMN readings.voltage_v IS 'Phase-to-neutral voltage at the device.';
COMMENT ON COLUMN readings.received_at IS 'Wall-clock time the API stored the reading.';
