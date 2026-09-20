-- The network model: a feeder, its buses and the line into each bus.
--
-- A low-voltage feeder is a tree below one transformer. The schema makes
-- anything else unstorable: one root per feeder, one line into each bus, the
-- line's far end is the bus's parent, and no bus is its own ancestor.

CREATE TABLE feeders (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  code              text             NOT NULL,
  name              text             NOT NULL,
  nominal_voltage_v double precision NOT NULL DEFAULT 230,
  transformer_kva   double precision NOT NULL,
  source_voltage_v  double precision NOT NULL,
  source_angle_deg  double precision NOT NULL DEFAULT 0,
  source_r_ohm      double precision NOT NULL,
  source_x_ohm      double precision NOT NULL,
  tap_pu            double precision NOT NULL DEFAULT 1,
  timezone          text             NOT NULL DEFAULT 'Australia/Sydney',
  attribution       text             NOT NULL,
  created_at        timestamptz      NOT NULL DEFAULT now(),
  updated_at        timestamptz      NOT NULL DEFAULT now(),
  CONSTRAINT feeders_code_key UNIQUE (code),
  CONSTRAINT feeders_code_format CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{0,31}$'),
  CONSTRAINT feeders_name_present CHECK (name <> ''),
  CONSTRAINT feeders_ratings_positive
    CHECK (nominal_voltage_v > 0 AND transformer_kva > 0 AND source_voltage_v > 0),
  CONSTRAINT feeders_source_impedance_positive
    CHECK (source_r_ohm >= 0 AND source_x_ohm >= 0 AND source_r_ohm + source_x_ohm > 0),
  CONSTRAINT feeders_source_angle_range CHECK (source_angle_deg > -360 AND source_angle_deg < 360),
  CONSTRAINT feeders_tap_range CHECK (tap_pu >= 0.85 AND tap_pu <= 1.15),
  CONSTRAINT feeders_timezone_format CHECK (timezone ~ '^[A-Za-z_]+(/[A-Za-z_+-]+)+$|^UTC$'),
  CONSTRAINT feeders_attribution_present CHECK (attribution <> '')
);

COMMENT ON TABLE feeders IS
  'A low-voltage feeder below one distribution transformer. The transformer is stored as its equivalent seen from the low-voltage terminals: a voltage source per phase behind a series impedance.';
COMMENT ON COLUMN feeders.code IS 'Short stable identifier, for example LV10.';
COMMENT ON COLUMN feeders.nominal_voltage_v IS 'Phase-to-neutral nominal voltage. Per-unit voltage limits are against this.';
COMMENT ON COLUMN feeders.source_voltage_v IS 'Open-circuit phase-to-earth voltage at the low-voltage terminals at the nominal tap.';
COMMENT ON COLUMN feeders.source_angle_deg IS 'Angle of phase 1. Phases 2 and 3 lag by 120 and 240 degrees.';
COMMENT ON COLUMN feeders.source_r_ohm IS 'Series resistance per phase, referred to the low-voltage side.';
COMMENT ON COLUMN feeders.source_x_ohm IS 'Series reactance per phase, referred to the low-voltage side.';
COMMENT ON COLUMN feeders.tap_pu IS 'Transformer tap as a multiplier of source_voltage_v. 0.975 is one 2.5 % step down.';
COMMENT ON COLUMN feeders.timezone IS 'IANA zone the feeder is in. Storage is UTC; this is for display.';
COMMENT ON COLUMN feeders.attribution IS 'Licence and attribution text of the dataset the feeder came from.';

CREATE TABLE feeder_nodes (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  feeder_id      uuid             NOT NULL,
  name           text             NOT NULL,
  parent_node_id uuid,
  ground_r_ohm   double precision,
  ground_x_ohm   double precision,
  created_at     timestamptz      NOT NULL DEFAULT now(),
  updated_at     timestamptz      NOT NULL DEFAULT now(),
  CONSTRAINT feeder_nodes_feeder_fkey
    FOREIGN KEY (feeder_id) REFERENCES feeders (id) ON DELETE CASCADE,
  CONSTRAINT feeder_nodes_name_key UNIQUE (feeder_id, name),
  -- Targets for composite foreign keys: "a node of THIS feeder", and "a node
  -- and its parent".
  CONSTRAINT feeder_nodes_feeder_id_key UNIQUE (feeder_id, id),
  CONSTRAINT feeder_nodes_id_parent_key UNIQUE (id, parent_node_id),
  -- The parent is a node of the same feeder.
  CONSTRAINT feeder_nodes_parent_fkey
    FOREIGN KEY (feeder_id, parent_node_id) REFERENCES feeder_nodes (feeder_id, id) ON DELETE CASCADE,
  CONSTRAINT feeder_nodes_name_present CHECK (name <> ''),
  CONSTRAINT feeder_nodes_not_own_parent CHECK (parent_node_id <> id),
  CONSTRAINT feeder_nodes_ground_complete
    CHECK ((ground_r_ohm IS NULL) = (ground_x_ohm IS NULL)),
  CONSTRAINT feeder_nodes_ground_positive
    CHECK (ground_r_ohm >= 0 AND ground_x_ohm >= 0 AND ground_r_ohm + ground_x_ohm > 0)
);

-- Exactly one root per feeder: the transformer's low-voltage bus.
CREATE UNIQUE INDEX feeder_nodes_one_root_key ON feeder_nodes (feeder_id) WHERE parent_node_id IS NULL;
-- "The children of this node", and the cascade from a parent.
CREATE INDEX feeder_nodes_parent_idx ON feeder_nodes (parent_node_id);

COMMENT ON TABLE feeder_nodes IS
  'A bus of the feeder tree. Every bus has four conductors: three phases and the neutral.';
COMMENT ON COLUMN feeder_nodes.parent_node_id IS 'The bus towards the transformer. NULL for the root, the transformer''s low-voltage bus.';
COMMENT ON COLUMN feeder_nodes.ground_r_ohm IS 'Resistance from the neutral to earth, when the neutral is earthed at this bus.';
COMMENT ON COLUMN feeder_nodes.ground_x_ohm IS 'Reactance from the neutral to earth. Set together with ground_r_ohm.';

-- A node must not be its own ancestor. The foreign key cannot say this: A's
-- parent B and B's parent A satisfy it. Walk up from the new parent; meeting
-- the node itself means the edge would close a loop.
CREATE FUNCTION reject_feeder_node_cycle() RETURNS trigger AS $$
BEGIN
  IF NEW.parent_node_id IS NOT NULL AND EXISTS (
    WITH RECURSIVE ancestors AS (
      SELECT n.id, n.parent_node_id FROM feeder_nodes n WHERE n.id = NEW.parent_node_id
      UNION
      SELECT n.id, n.parent_node_id FROM feeder_nodes n JOIN ancestors a ON n.id = a.parent_node_id
    )
    SELECT 1 FROM ancestors WHERE id = NEW.id
  ) THEN
    RAISE EXCEPTION 'feeder node % would be its own ancestor: a feeder is a tree', NEW.name
      USING ERRCODE = 'check_violation', CONSTRAINT = 'feeder_nodes_no_cycle';
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER feeder_nodes_no_cycle
  BEFORE INSERT OR UPDATE OF parent_node_id ON feeder_nodes
  FOR EACH ROW EXECUTE FUNCTION reject_feeder_node_cycle();

CREATE TYPE ampacity_source AS ENUM ('assumed', 'operator');

CREATE TABLE feeder_lines (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  feeder_id       uuid               NOT NULL,
  name            text               NOT NULL,
  from_node_id    uuid               NOT NULL,
  to_node_id      uuid               NOT NULL,
  linecode        text               NOT NULL,
  length_m        double precision   NOT NULL,
  is_switch       boolean            NOT NULL DEFAULT false,
  r_ohm           double precision[] NOT NULL,
  x_ohm           double precision[] NOT NULL,
  b_s             double precision[] NOT NULL,
  ampacity_a      double precision,
  ampacity_source ampacity_source,
  created_at      timestamptz        NOT NULL DEFAULT now(),
  updated_at      timestamptz        NOT NULL DEFAULT now(),
  CONSTRAINT feeder_lines_feeder_fkey
    FOREIGN KEY (feeder_id) REFERENCES feeders (id) ON DELETE CASCADE,
  CONSTRAINT feeder_lines_name_key UNIQUE (feeder_id, name),
  -- One line into each bus. With one root and no cycles, that is a tree.
  CONSTRAINT feeder_lines_to_node_key UNIQUE (to_node_id),
  -- The far end is a node of the same feeder ...
  CONSTRAINT feeder_lines_to_node_fkey
    FOREIGN KEY (feeder_id, to_node_id) REFERENCES feeder_nodes (feeder_id, id) ON DELETE CASCADE,
  -- ... and the near end is that node's parent. A line cannot join two buses
  -- that the tree does not join, and the root (parent NULL) has no line in.
  CONSTRAINT feeder_lines_joins_parent_fkey
    FOREIGN KEY (to_node_id, from_node_id) REFERENCES feeder_nodes (id, parent_node_id),
  CONSTRAINT feeder_lines_name_present CHECK (name <> ''),
  CONSTRAINT feeder_lines_ends_differ CHECK (from_node_id <> to_node_id),
  CONSTRAINT feeder_lines_length_positive CHECK (length_m > 0),
  -- 4x4, row-major: phases 1 to 3, then the neutral.
  CONSTRAINT feeder_lines_matrices_4x4 CHECK (
    array_ndims(r_ohm) = 1 AND cardinality(r_ohm) = 16 AND
    array_ndims(x_ohm) = 1 AND cardinality(x_ohm) = 16 AND
    array_ndims(b_s) = 1 AND cardinality(b_s) = 16 AND
    array_position(r_ohm, NULL) IS NULL AND
    array_position(x_ohm, NULL) IS NULL AND
    array_position(b_s, NULL) IS NULL
  ),
  CONSTRAINT feeder_lines_ampacity_positive CHECK (ampacity_a > 0),
  CONSTRAINT feeder_lines_ampacity_sourced CHECK ((ampacity_a IS NULL) = (ampacity_source IS NULL))
);

CREATE INDEX feeder_lines_from_node_idx ON feeder_lines (from_node_id);

COMMENT ON TABLE feeder_lines IS
  'The four-conductor line, or closed switch, into a bus from its parent. Impedances are for the whole length.';
COMMENT ON COLUMN feeder_lines.linecode IS 'Cable type, as the OpenDSS linecode name of the source dataset.';
COMMENT ON COLUMN feeder_lines.r_ohm IS 'Series resistance with earth return, 4x4 row-major (16 values), in ohms.';
COMMENT ON COLUMN feeder_lines.x_ohm IS 'Series reactance with earth return, 4x4 row-major (16 values), in ohms.';
COMMENT ON COLUMN feeder_lines.b_s IS 'Shunt susceptance, 4x4 row-major (16 values), in siemens. Half sits at each end.';
COMMENT ON COLUMN feeder_lines.ampacity_a IS 'Continuous current rating per conductor. NULL means unrated: the line is not checked.';
COMMENT ON COLUMN feeder_lines.ampacity_source IS 'Where the rating came from: assumed from typical catalogue values, or set by an operator.';

SELECT apply_conventions();
