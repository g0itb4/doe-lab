-- The updated_at trigger of a table names every column in its WHEN clause, so
-- it goes before the columns do, and apply_conventions() puts it back without
-- them.
SELECT drop_trigger_if_present('sites_set_updated_at', 'sites');
SELECT drop_trigger_if_present('feeders_set_updated_at', 'feeders');

DROP INDEX IF EXISTS sites_located_idx;
ALTER TABLE sites
  DROP CONSTRAINT IF EXISTS sites_location_range,
  DROP CONSTRAINT IF EXISTS sites_location_complete,
  DROP COLUMN IF EXISTS longitude_deg,
  DROP COLUMN IF EXISTS latitude_deg;

DROP INDEX IF EXISTS feeders_substation_idx;
ALTER TABLE feeders
  DROP CONSTRAINT IF EXISTS feeders_substation_fkey,
  DROP COLUMN IF EXISTS substation_id;

DROP TABLE IF EXISTS substations;

SELECT apply_conventions();
