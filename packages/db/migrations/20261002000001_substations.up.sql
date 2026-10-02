-- Where the fleet is: the zone substations, the feeders below each, and the
-- location of a site.
--
-- A feeder is a network model, and its buses have no coordinates: the source
-- dataset gives none. What has a place on a map is the substation a feeder
-- hangs from, and the customer's connection point.

CREATE TABLE substations (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  code          text             NOT NULL,
  name          text             NOT NULL,
  dnsp          text             NOT NULL,
  state         text             NOT NULL,
  latitude_deg  double precision NOT NULL,
  longitude_deg double precision NOT NULL,
  created_at    timestamptz      NOT NULL DEFAULT now(),
  updated_at    timestamptz      NOT NULL DEFAULT now(),
  CONSTRAINT substations_code_key UNIQUE (code),
  CONSTRAINT substations_code_format CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{0,31}$'),
  CONSTRAINT substations_name_present CHECK (name <> ''),
  CONSTRAINT substations_dnsp_present CHECK (dnsp <> ''),
  CONSTRAINT substations_state_format CHECK (state ~ '^[A-Z]{2,3}$'),
  CONSTRAINT substations_location_range
    CHECK (latitude_deg BETWEEN -90 AND 90 AND longitude_deg BETWEEN -180 AND 180)
);

COMMENT ON TABLE substations IS
  'A zone substation: the place on the map that the feeders below it are drawn from. It is not part of the power flow, which solves each low-voltage feeder on its own.';
COMMENT ON COLUMN substations.code IS 'Short stable identifier, for example SUB-001.';
COMMENT ON COLUMN substations.dnsp IS 'The distribution network service provider that owns the substation.';
COMMENT ON COLUMN substations.state IS 'The Australian state or territory, as its abbreviation: NSW, VIC, SA.';
COMMENT ON COLUMN substations.latitude_deg IS 'WGS 84 latitude, in degrees. South is negative.';
COMMENT ON COLUMN substations.longitude_deg IS 'WGS 84 longitude, in degrees. East is positive.';

-- Nullable: a feeder imported on its own has no substation.
ALTER TABLE feeders
  ADD COLUMN substation_id uuid,
  ADD CONSTRAINT feeders_substation_fkey
    FOREIGN KEY (substation_id) REFERENCES substations (id) ON DELETE RESTRICT;

-- "The feeders of this substation", and the check behind the foreign key.
CREATE INDEX feeders_substation_idx ON feeders (substation_id);

COMMENT ON COLUMN feeders.substation_id IS 'The zone substation the feeder hangs from. NULL for a feeder that has not been placed.';

-- Nullable, and set together: a passive customer of the source dataset has no
-- place, and is in the power flow without being on the map.
ALTER TABLE sites
  ADD COLUMN latitude_deg  double precision,
  ADD COLUMN longitude_deg double precision,
  ADD CONSTRAINT sites_location_complete CHECK ((latitude_deg IS NULL) = (longitude_deg IS NULL)),
  ADD CONSTRAINT sites_location_range
    CHECK (latitude_deg BETWEEN -90 AND 90 AND longitude_deg BETWEEN -180 AND 180);

-- ListLocatedSites: the sites a map draws, of every feeder, in NMI order.
CREATE INDEX sites_located_idx ON sites (nmi) WHERE latitude_deg IS NOT NULL AND deleted_at IS NULL;

COMMENT ON COLUMN sites.latitude_deg IS 'WGS 84 latitude of the connection point, in degrees. NULL for a site with no place on the map. Synthetic, like the NMI.';
COMMENT ON COLUMN sites.longitude_deg IS 'WGS 84 longitude of the connection point, in degrees. Set together with latitude_deg.';

SELECT apply_conventions();
