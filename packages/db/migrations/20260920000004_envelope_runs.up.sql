-- The policy, and the engine runs that apply it.

CREATE TYPE envelope_policy AS ENUM ('equal', 'proportional');

CREATE TABLE envelope_configs (
  id                    uuid PRIMARY KEY DEFAULT uuidv7(),
  feeder_id             uuid             NOT NULL,
  version               integer          NOT NULL,
  policy                envelope_policy  NOT NULL,
  v_min_pu              double precision NOT NULL,
  v_max_pu              double precision NOT NULL,
  transformer_limit_pct double precision NOT NULL DEFAULT 100,
  line_limit_pct        double precision NOT NULL DEFAULT 100,
  pv_scale              double precision NOT NULL DEFAULT 1,
  static_limit_w        double precision NOT NULL DEFAULT 5000,
  interval_minutes      integer          NOT NULL DEFAULT 30,
  horizon_intervals     integer          NOT NULL DEFAULT 48,
  breach_grace_seconds  integer          NOT NULL DEFAULT 60,
  offline_after_seconds integer          NOT NULL DEFAULT 300,
  note                  text             NOT NULL DEFAULT '',
  created_by            text             NOT NULL,
  created_at            timestamptz      NOT NULL DEFAULT now(),
  CONSTRAINT envelope_configs_feeder_fkey FOREIGN KEY (feeder_id) REFERENCES feeders (id) ON DELETE RESTRICT,
  CONSTRAINT envelope_configs_version_key UNIQUE (feeder_id, version),
  -- Target for runs: "a config of THIS feeder".
  CONSTRAINT envelope_configs_feeder_id_key UNIQUE (feeder_id, id),
  CONSTRAINT envelope_configs_version_positive CHECK (version >= 1),
  CONSTRAINT envelope_configs_voltage_band CHECK (v_min_pu >= 0.8 AND v_min_pu < v_max_pu AND v_max_pu <= 1.2),
  CONSTRAINT envelope_configs_limits_range CHECK (
    transformer_limit_pct > 0 AND transformer_limit_pct <= 200 AND
    line_limit_pct > 0 AND line_limit_pct <= 200
  ),
  CONSTRAINT envelope_configs_pv_scale_range CHECK (pv_scale >= 0 AND pv_scale <= 20),
  CONSTRAINT envelope_configs_static_limit_not_negative CHECK (static_limit_w >= 0),
  CONSTRAINT envelope_configs_interval_allowed CHECK (interval_minutes IN (5, 15, 30, 60)),
  CONSTRAINT envelope_configs_horizon_range CHECK (horizon_intervals BETWEEN 1 AND 288),
  CONSTRAINT envelope_configs_breach_grace_range CHECK (breach_grace_seconds BETWEEN 0 AND 3600),
  CONSTRAINT envelope_configs_offline_after_range CHECK (offline_after_seconds BETWEEN 10 AND 86400),
  CONSTRAINT envelope_configs_created_by_present CHECK (created_by <> '')
);

COMMENT ON TABLE envelope_configs IS
  'The envelope policy of a feeder, as immutable versions. A change is a new row; the highest version is the active one. Rows cannot be updated or deleted.';
COMMENT ON COLUMN envelope_configs.policy IS 'equal: every site gets the same limit. proportional: every site gets the same fraction of its cap.';
COMMENT ON COLUMN envelope_configs.v_min_pu IS 'Lower voltage limit at a customer, per unit of the feeder''s nominal voltage.';
COMMENT ON COLUMN envelope_configs.v_max_pu IS 'Upper voltage limit at a customer, per unit of the feeder''s nominal voltage.';
COMMENT ON COLUMN envelope_configs.transformer_limit_pct IS 'Allowed transformer loading, as a percentage of its rating.';
COMMENT ON COLUMN envelope_configs.line_limit_pct IS 'Allowed conductor current, as a percentage of each line''s ampacity.';
COMMENT ON COLUMN envelope_configs.pv_scale IS 'Multiplier on the 2010-2013 PV profiles, to represent present-day uptake.';
COMMENT ON COLUMN envelope_configs.static_limit_w IS 'The fixed export limit that reports compare the envelopes with.';
COMMENT ON COLUMN envelope_configs.breach_grace_seconds IS 'How long a site may exceed its export limit before a constraint_breach alert opens.';
COMMENT ON COLUMN envelope_configs.offline_after_seconds IS 'How long a device may be silent before a device_offline alert opens.';

CREATE FUNCTION reject_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION '% is immutable (attempted %)', TG_TABLE_NAME, TG_OP
    USING ERRCODE = 'restrict_violation';
END $$ LANGUAGE plpgsql;

COMMENT ON FUNCTION reject_mutation() IS
  'Trigger function for tables whose rows never change. Raises restrict_violation.';

CREATE TRIGGER envelope_configs_immutable
  BEFORE UPDATE OR DELETE ON envelope_configs
  FOR EACH ROW EXECUTE FUNCTION reject_mutation();

CREATE TYPE run_status AS ENUM ('running', 'completed', 'failed');

CREATE TABLE envelope_runs (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  feeder_id          uuid        NOT NULL,
  envelope_config_id uuid        NOT NULL,
  status             run_status  NOT NULL DEFAULT 'running',
  idempotency_key    text        NOT NULL,
  horizon_from       timestamptz NOT NULL,
  horizon_to         timestamptz NOT NULL,
  started_at         timestamptz NOT NULL DEFAULT now(),
  completed_at       timestamptz,
  duration_ms        integer,
  site_count         integer     NOT NULL DEFAULT 0,
  interval_count     integer     NOT NULL DEFAULT 0,
  envelope_count     integer     NOT NULL DEFAULT 0,
  engine_version     text        NOT NULL,
  error              text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT envelope_runs_feeder_fkey FOREIGN KEY (feeder_id) REFERENCES feeders (id) ON DELETE RESTRICT,
  -- The config is one of the same feeder's.
  CONSTRAINT envelope_runs_config_fkey
    FOREIGN KEY (feeder_id, envelope_config_id) REFERENCES envelope_configs (feeder_id, id) ON DELETE RESTRICT,
  CONSTRAINT envelope_runs_idempotency_key_key UNIQUE (idempotency_key),
  CONSTRAINT envelope_runs_idempotency_key_format CHECK (idempotency_key ~ '^[A-Za-z0-9._:-]{8,128}$'),
  CONSTRAINT envelope_runs_horizon_ordered CHECK (horizon_to > horizon_from),
  -- A run is finished exactly when it is no longer running, and carries an
  -- error exactly when it failed.
  CONSTRAINT envelope_runs_completed_when_done CHECK ((status = 'running') = (completed_at IS NULL)),
  CONSTRAINT envelope_runs_duration_when_done CHECK ((status = 'running') = (duration_ms IS NULL)),
  CONSTRAINT envelope_runs_error_when_failed CHECK ((status = 'failed') = (error IS NOT NULL)),
  CONSTRAINT envelope_runs_completed_after_start CHECK (completed_at >= started_at),
  CONSTRAINT envelope_runs_counts_not_negative
    CHECK (duration_ms >= 0 AND site_count >= 0 AND interval_count >= 0 AND envelope_count >= 0),
  CONSTRAINT envelope_runs_engine_version_present CHECK (engine_version <> '')
);

-- ListEnvelopeRuns: newest first, per feeder, optionally by status.
CREATE INDEX envelope_runs_feeder_started_idx ON envelope_runs (feeder_id, started_at DESC, id DESC);
CREATE INDEX envelope_runs_config_idx ON envelope_runs (envelope_config_id);

COMMENT ON TABLE envelope_runs IS
  'One run of the engine: the config version it used, the horizon it covered, and how it ended.';
COMMENT ON COLUMN envelope_runs.idempotency_key IS 'Chosen by the engine. Creating a run again with the same key returns the first run.';
COMMENT ON COLUMN envelope_runs.horizon_from IS 'Start of the first interval of the run, in feeder time.';
COMMENT ON COLUMN envelope_runs.horizon_to IS 'End of the last interval of the run, in feeder time.';
COMMENT ON COLUMN envelope_runs.started_at IS 'Wall-clock time the run was created.';
COMMENT ON COLUMN envelope_runs.envelope_count IS 'Envelopes published by the run so far.';

CREATE TABLE idempotency_keys (
  scope           text        NOT NULL,
  key             text        NOT NULL,
  request_hash    bytea       NOT NULL,
  envelope_run_id uuid,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL DEFAULT now() + interval '7 days',
  CONSTRAINT idempotency_keys_pkey PRIMARY KEY (scope, key),
  CONSTRAINT idempotency_keys_run_fkey
    FOREIGN KEY (envelope_run_id) REFERENCES envelope_runs (id) ON DELETE CASCADE,
  CONSTRAINT idempotency_keys_scope_present CHECK (scope <> ''),
  CONSTRAINT idempotency_keys_key_format CHECK (key ~ '^[A-Za-z0-9._:-]{8,128}$'),
  -- SHA-256 of the request.
  CONSTRAINT idempotency_keys_hash_length CHECK (octet_length(request_hash) = 32),
  CONSTRAINT idempotency_keys_expires_after_created CHECK (expires_at > created_at)
);

CREATE INDEX idempotency_keys_expires_idx ON idempotency_keys (expires_at);
CREATE INDEX idempotency_keys_run_idx ON idempotency_keys (envelope_run_id);

COMMENT ON TABLE idempotency_keys IS
  'Replay protection for writes that must happen once. A replay with the same key and the same request is a no-op; with a different request it is a conflict.';
COMMENT ON COLUMN idempotency_keys.scope IS 'The operation the key belongs to, for example publish_envelopes.';
COMMENT ON COLUMN idempotency_keys.request_hash IS 'SHA-256 of the request the key was first used with.';

SELECT apply_conventions();
