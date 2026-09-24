-- What the engine saw, interval by interval: the state of the whole feeder
-- that the envelopes of a run were computed against.
--
-- An envelope says what a site may do. It does not say why the feeder needed
-- it: how loaded the transformer was forecast to be, how close the voltage was
-- to the band, and what a fixed export limit would have done instead. Those
-- come from the same power flows that produced the envelopes, and the engine
-- records them here, one row per run and interval.

CREATE TABLE envelope_run_intervals (
  envelope_run_id        uuid               NOT NULL,
  feeder_id              uuid               NOT NULL,
  valid_from             timestamptz        NOT NULL,
  valid_to               timestamptz        NOT NULL,
  forecast_net_load_w    double precision   NOT NULL,
  forecast_loading_pct   double precision   NOT NULL,
  forecast_v_min_pu      double precision   NOT NULL,
  forecast_v_max_pu      double precision   NOT NULL,
  export_limit_total_w   double precision   NOT NULL,
  import_limit_total_w   double precision   NOT NULL,
  static_limit_total_w   double precision   NOT NULL,
  static_v_max_pu        double precision   NOT NULL,
  static_binding         binding_constraint NOT NULL,
  static_binding_element text               NOT NULL DEFAULT '',
  created_at             timestamptz        NOT NULL DEFAULT now(),
  CONSTRAINT envelope_run_intervals_pkey PRIMARY KEY (envelope_run_id, valid_from),
  -- The run is one of the same feeder's.
  CONSTRAINT envelope_run_intervals_run_fkey
    FOREIGN KEY (envelope_run_id) REFERENCES envelope_runs (id) ON DELETE CASCADE,
  CONSTRAINT envelope_run_intervals_feeder_fkey
    FOREIGN KEY (feeder_id) REFERENCES feeders (id) ON DELETE CASCADE,
  CONSTRAINT envelope_run_intervals_period_ordered CHECK (valid_to > valid_from),
  CONSTRAINT envelope_run_intervals_on_grid CHECK (extract(epoch FROM valid_from)::bigint % 300 = 0),
  CONSTRAINT envelope_run_intervals_voltage_ordered
    CHECK (forecast_v_min_pu > 0 AND forecast_v_min_pu <= forecast_v_max_pu AND static_v_max_pu > 0),
  CONSTRAINT envelope_run_intervals_not_negative CHECK (
    forecast_loading_pct >= 0 AND export_limit_total_w >= 0 AND
    import_limit_total_w >= 0 AND static_limit_total_w >= 0
  )
);

-- "The latest view of each interval of a feeder": the feeder's series.
CREATE INDEX envelope_run_intervals_feeder_idx
  ON envelope_run_intervals (feeder_id, valid_from, created_at DESC);

COMMENT ON TABLE envelope_run_intervals IS
  'The forecast state of the whole feeder for each interval of a run, as the engine solved it. Rows are written with the run and never change.';
COMMENT ON COLUMN envelope_run_intervals.valid_from IS 'Start of the interval, inclusive, in feeder time.';
COMMENT ON COLUMN envelope_run_intervals.forecast_net_load_w IS 'Forecast power through the transformer with every site at its forecast: positive is supply to the feeder, negative is reverse flow.';
COMMENT ON COLUMN envelope_run_intervals.forecast_loading_pct IS 'Forecast transformer loading, as a percentage of its rating.';
COMMENT ON COLUMN envelope_run_intervals.forecast_v_min_pu IS 'Lowest customer voltage with every site at its forecast, per unit of the nominal voltage.';
COMMENT ON COLUMN envelope_run_intervals.forecast_v_max_pu IS 'Highest customer voltage with every site at its forecast, per unit of the nominal voltage.';
COMMENT ON COLUMN envelope_run_intervals.export_limit_total_w IS 'Sum of the export limits the run gave for the interval.';
COMMENT ON COLUMN envelope_run_intervals.import_limit_total_w IS 'Sum of the import limits the run gave for the interval.';
COMMENT ON COLUMN envelope_run_intervals.static_limit_total_w IS 'Sum of what the enrolled sites could export under the config''s fixed limit instead.';
COMMENT ON COLUMN envelope_run_intervals.static_v_max_pu IS 'Highest customer voltage if every enrolled site exported at the fixed limit.';
COMMENT ON COLUMN envelope_run_intervals.static_binding IS 'The network limit that the fixed export limit would break, or none.';
COMMENT ON COLUMN envelope_run_intervals.static_binding_element IS 'The site, line or transformer where static_binding applies.';

CREATE TRIGGER envelope_run_intervals_immutable
  BEFORE UPDATE ON envelope_run_intervals
  FOR EACH ROW EXECUTE FUNCTION reject_mutation();
