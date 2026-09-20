-- Customers and their DER.

-- The NMI checksum of AEMO's NMI Procedure (Luhn-10 over ASCII codes): from
-- the right, double every other character's code, starting with the
-- rightmost; add the digits of every value; the checksum brings the total to
-- a multiple of ten.
CREATE FUNCTION nmi_checksum(nmi10 text) RETURNS integer AS $$
  SELECT (10 - sum(digit)::integer % 10) % 10
    FROM (
      SELECT (string_to_table(
               (ascii(substr(nmi10, pos, 1)) * CASE WHEN (length(nmi10) - pos) % 2 = 0 THEN 2 ELSE 1 END)::text,
               NULL))::integer AS digit
        FROM generate_series(1, length(nmi10)) AS pos
    ) digits;
$$ LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE;

COMMENT ON FUNCTION nmi_checksum(text) IS
  'Checksum digit of a 10-character NMI, per AEMO''s NMI Procedure. nmi_checksum(''1234C6789A'') = 3.';

CREATE TABLE sites (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  nmi              text             NOT NULL,
  feeder_id        uuid             NOT NULL,
  node_id          uuid             NOT NULL,
  name             text             NOT NULL,
  phase            smallint         NOT NULL,
  pv_kw            double precision NOT NULL DEFAULT 0,
  inverter_kva     double precision NOT NULL DEFAULT 0,
  export_cap_w     double precision NOT NULL DEFAULT 0,
  import_cap_w     double precision NOT NULL DEFAULT 0,
  has_battery      boolean          NOT NULL DEFAULT false,
  battery_kwh      double precision,
  has_ev           boolean          NOT NULL DEFAULT false,
  profile_customer integer,
  created_at       timestamptz      NOT NULL DEFAULT now(),
  updated_at       timestamptz      NOT NULL DEFAULT now(),
  deleted_at       timestamptz,
  CONSTRAINT sites_feeder_fkey FOREIGN KEY (feeder_id) REFERENCES feeders (id) ON DELETE RESTRICT,
  -- The connection point is a node of the site's own feeder.
  CONSTRAINT sites_node_fkey
    FOREIGN KEY (feeder_id, node_id) REFERENCES feeder_nodes (feeder_id, id) ON DELETE RESTRICT,
  -- Not partial: an NMI is never reused, so a soft-deleted site keeps its
  -- NMI for good. This is the one deliberate exception to "uniqueness on a
  -- soft-deleted table is partial".
  CONSTRAINT sites_nmi_key UNIQUE (nmi),
  CONSTRAINT sites_name_key UNIQUE (feeder_id, name),
  CONSTRAINT sites_nmi_format CHECK (nmi ~ '^[A-Z0-9]{10}[0-9]$'),
  CONSTRAINT sites_nmi_checksum
    CHECK (nmi_checksum(substr(nmi, 1, 10)) = substr(nmi, 11, 1)::integer),
  CONSTRAINT sites_name_present CHECK (name <> ''),
  CONSTRAINT sites_phase_range CHECK (phase BETWEEN 1 AND 3),
  CONSTRAINT sites_ratings_not_negative
    CHECK (pv_kw >= 0 AND inverter_kva >= 0 AND export_cap_w >= 0 AND import_cap_w >= 0),
  CONSTRAINT sites_battery_sized CHECK (has_battery = (battery_kwh IS NOT NULL)),
  CONSTRAINT sites_battery_positive CHECK (battery_kwh > 0),
  CONSTRAINT sites_profile_customer_range CHECK (profile_customer BETWEEN 1 AND 300)
);

CREATE INDEX sites_feeder_idx ON sites (feeder_id, nmi) WHERE deleted_at IS NULL;
CREATE INDEX sites_node_idx ON sites (node_id);

COMMENT ON TABLE sites IS
  'A customer connection point: single phase, between one phase and the neutral of a bus. NMIs are synthetic.';
COMMENT ON COLUMN sites.nmi IS 'National Metering Identifier with its checksum digit: 11 characters. Synthetic, in a block allocated to no distributor.';
COMMENT ON COLUMN sites.name IS 'The connection''s name in the source dataset, for example Ld7_LOAD_A.';
COMMENT ON COLUMN sites.phase IS 'The phase the site is connected to: 1, 2 or 3.';
COMMENT ON COLUMN sites.pv_kw IS 'Installed PV capacity. 0 for a site with no PV.';
COMMENT ON COLUMN sites.export_cap_w IS 'The largest export limit the site may be given. 0 means the site is passive: it gets no export envelope and is forecast instead.';
COMMENT ON COLUMN sites.import_cap_w IS 'The largest import limit the site may be given. 0 means no import envelope.';
COMMENT ON COLUMN sites.profile_customer IS 'The Ausgrid Solar Home customer (1 to 300) whose half-hourly data this site replays. The pairing is synthetic.';

CREATE TYPE der_type AS ENUM ('solar', 'battery', 'ev');

CREATE TABLE devices (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  site_id    uuid             NOT NULL,
  der_type   der_type         NOT NULL,
  rated_w    double precision NOT NULL,
  created_at timestamptz      NOT NULL DEFAULT now(),
  updated_at timestamptz      NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CONSTRAINT devices_site_fkey FOREIGN KEY (site_id) REFERENCES sites (id) ON DELETE CASCADE,
  -- Target for readings: "a device and the site it belongs to".
  CONSTRAINT devices_id_site_key UNIQUE (id, site_id),
  CONSTRAINT devices_rated_positive CHECK (rated_w > 0)
);

-- One live device of each type per site. Partial, so a replaced inverter does
-- not block its successor.
CREATE UNIQUE INDEX devices_site_type_live_key ON devices (site_id, der_type) WHERE deleted_at IS NULL;

COMMENT ON TABLE devices IS
  'A distributed energy resource behind a site: an inverter, a battery or an EV charger. Each reports telemetry and receives the site''s envelope.';
COMMENT ON COLUMN devices.rated_w IS 'Nameplate power.';

-- Live state, deliberately unaudited: it changes with every reading, and the
-- readings are its history.
CREATE TABLE device_status (
  device_id    uuid PRIMARY KEY,
  last_seen_at timestamptz      NOT NULL,
  power_w      double precision NOT NULL,
  net_export_w double precision NOT NULL,
  CONSTRAINT device_status_device_fkey FOREIGN KEY (device_id) REFERENCES devices (id) ON DELETE CASCADE
);

COMMENT ON TABLE device_status IS
  'The latest reading of each device, kept beside the readings so "who is silent" and "what is the fleet doing now" need no scan.';
COMMENT ON COLUMN device_status.last_seen_at IS 'Feeder time of the latest reading.';

SELECT apply_conventions();
