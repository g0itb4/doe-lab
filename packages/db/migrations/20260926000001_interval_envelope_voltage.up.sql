-- The third state of an interval: the feeder with every enrolled site
-- exporting at its envelope.
--
-- The forecast says where the voltage would go with no limits, and
-- static_v_max_pu where a fixed limit would take it. This column closes the
-- comparison: where the voltage is when the fleet uses everything the
-- envelopes allow. It is the engine's own check on its answer, recorded.
--
-- Nullable: a row written before the column existed has no value, and no
-- honest way to get one.

ALTER TABLE envelope_run_intervals
  ADD COLUMN envelope_v_max_pu double precision,
  ADD CONSTRAINT envelope_run_intervals_envelope_voltage_positive CHECK (envelope_v_max_pu > 0);

COMMENT ON COLUMN envelope_run_intervals.envelope_v_max_pu IS
  'Highest customer voltage if every enrolled site exported at its envelope, per unit of the nominal voltage. Null for a row from before the column existed.';
