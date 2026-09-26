ALTER TABLE envelope_run_intervals
  DROP CONSTRAINT envelope_run_intervals_envelope_voltage_positive,
  DROP COLUMN envelope_v_max_pu;
