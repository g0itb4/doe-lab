-- What the engine solved, bus by bus and line by line.
--
-- envelope_run_intervals holds the state of the whole feeder for each
-- interval of a run: its lowest and highest voltage, and the power through
-- the transformer. A drawing of the feeder needs the same operating points
-- where they happen: the voltage at each bus and the current in each line.
--
-- These tables hold the latest view of each interval, not one row per run:
-- a run replaces what an earlier run stored for the intervals it covers. A
-- row per run, bus and interval would be a million rows for a day of one
-- feeder, to answer a question nobody asks. Like device_status they are
-- state, not history, and are not audited.

-- Target for a composite foreign key: "a line of THIS feeder".
ALTER TABLE feeder_lines ADD CONSTRAINT feeder_lines_feeder_id_key UNIQUE (feeder_id, id);

CREATE TABLE feeder_node_states (
  feeder_id       uuid               NOT NULL,
  node_id         uuid               NOT NULL,
  valid_from      timestamptz        NOT NULL,
  valid_to        timestamptz        NOT NULL,
  envelope_run_id uuid               NOT NULL,
  forecast_v_pu   double precision[] NOT NULL,
  envelope_v_pu   double precision[] NOT NULL,
  static_v_pu     double precision[] NOT NULL,
  CONSTRAINT feeder_node_states_pkey PRIMARY KEY (feeder_id, valid_from, node_id),
  -- The node is one of the same feeder's.
  CONSTRAINT feeder_node_states_node_fkey
    FOREIGN KEY (feeder_id, node_id) REFERENCES feeder_nodes (feeder_id, id) ON DELETE CASCADE,
  -- A state goes with the run that solved it: retention removes a run once
  -- its horizon has passed.
  CONSTRAINT feeder_node_states_run_fkey
    FOREIGN KEY (envelope_run_id) REFERENCES envelope_runs (id) ON DELETE CASCADE,
  CONSTRAINT feeder_node_states_period_ordered CHECK (valid_to > valid_from),
  CONSTRAINT feeder_node_states_on_grid CHECK (extract(epoch FROM valid_from)::bigint % 300 = 0),
  -- Phases 1 to 3, each to the neutral.
  CONSTRAINT feeder_node_states_three_phases CHECK (
    array_ndims(forecast_v_pu) = 1 AND cardinality(forecast_v_pu) = 3 AND
    array_ndims(envelope_v_pu) = 1 AND cardinality(envelope_v_pu) = 3 AND
    array_ndims(static_v_pu) = 1 AND cardinality(static_v_pu) = 3 AND
    array_position(forecast_v_pu, NULL) IS NULL AND
    array_position(envelope_v_pu, NULL) IS NULL AND
    array_position(static_v_pu, NULL) IS NULL
  )
);

CREATE INDEX feeder_node_states_run_idx ON feeder_node_states (envelope_run_id);

COMMENT ON TABLE feeder_node_states IS
  'The voltage at a bus for one interval, as the latest run that covered the interval solved it, at three operating points: the forecast, every enrolled site at its envelope, and every enrolled site at the fixed limit.';
COMMENT ON COLUMN feeder_node_states.valid_from IS 'Start of the interval, inclusive, in feeder time.';
COMMENT ON COLUMN feeder_node_states.envelope_run_id IS 'The run that solved the interval last.';
COMMENT ON COLUMN feeder_node_states.forecast_v_pu IS 'Phase-to-neutral voltage of phases 1 to 3 with every site at its forecast, per unit of the nominal voltage.';
COMMENT ON COLUMN feeder_node_states.envelope_v_pu IS 'The same with every enrolled site exporting at its envelope.';
COMMENT ON COLUMN feeder_node_states.static_v_pu IS 'The same with every enrolled site exporting at the fixed limit.';

CREATE TABLE feeder_line_states (
  feeder_id          uuid               NOT NULL,
  line_id            uuid               NOT NULL,
  valid_from         timestamptz        NOT NULL,
  valid_to           timestamptz        NOT NULL,
  envelope_run_id    uuid               NOT NULL,
  forecast_current_a double precision[] NOT NULL,
  envelope_current_a double precision[] NOT NULL,
  static_current_a   double precision[] NOT NULL,
  forecast_power_w   double precision   NOT NULL,
  envelope_power_w   double precision   NOT NULL,
  static_power_w     double precision   NOT NULL,
  CONSTRAINT feeder_line_states_pkey PRIMARY KEY (feeder_id, valid_from, line_id),
  CONSTRAINT feeder_line_states_line_fkey
    FOREIGN KEY (feeder_id, line_id) REFERENCES feeder_lines (feeder_id, id) ON DELETE CASCADE,
  CONSTRAINT feeder_line_states_run_fkey
    FOREIGN KEY (envelope_run_id) REFERENCES envelope_runs (id) ON DELETE CASCADE,
  CONSTRAINT feeder_line_states_period_ordered CHECK (valid_to > valid_from),
  CONSTRAINT feeder_line_states_on_grid CHECK (extract(epoch FROM valid_from)::bigint % 300 = 0),
  -- Phases 1 to 3, then the neutral.
  CONSTRAINT feeder_line_states_four_conductors CHECK (
    array_ndims(forecast_current_a) = 1 AND cardinality(forecast_current_a) = 4 AND
    array_ndims(envelope_current_a) = 1 AND cardinality(envelope_current_a) = 4 AND
    array_ndims(static_current_a) = 1 AND cardinality(static_current_a) = 4 AND
    array_position(forecast_current_a, NULL) IS NULL AND
    array_position(envelope_current_a, NULL) IS NULL AND
    array_position(static_current_a, NULL) IS NULL
  )
);

CREATE INDEX feeder_line_states_run_idx ON feeder_line_states (envelope_run_id);

COMMENT ON TABLE feeder_line_states IS
  'The flow in a line for one interval, as the latest run that covered the interval solved it, at the same three operating points as feeder_node_states.';
COMMENT ON COLUMN feeder_line_states.valid_from IS 'Start of the interval, inclusive, in feeder time.';
COMMENT ON COLUMN feeder_line_states.envelope_run_id IS 'The run that solved the interval last.';
COMMENT ON COLUMN feeder_line_states.forecast_current_a IS 'Current in each conductor with every site at its forecast: phases 1 to 3, then the neutral. Amperes, so that a rating an operator changes is judged against the same numbers.';
COMMENT ON COLUMN feeder_line_states.envelope_current_a IS 'The same with every enrolled site exporting at its envelope.';
COMMENT ON COLUMN feeder_line_states.static_current_a IS 'The same with every enrolled site exporting at the fixed limit.';
COMMENT ON COLUMN feeder_line_states.forecast_power_w IS 'Power the line carries towards its far end with every site at its forecast. Negative is power flowing back towards the transformer.';
COMMENT ON COLUMN feeder_line_states.envelope_power_w IS 'The same with every enrolled site exporting at its envelope.';
COMMENT ON COLUMN feeder_line_states.static_power_w IS 'The same with every enrolled site exporting at the fixed limit.';
