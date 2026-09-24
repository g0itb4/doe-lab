DROP INDEX IF EXISTS alerts_feeder_open_idx;
DROP INDEX IF EXISTS alerts_feeder_opened_idx;
CREATE INDEX alerts_opened_idx ON alerts (opened_at DESC, id DESC);
CREATE INDEX alerts_open_idx ON alerts (opened_at DESC, id DESC) WHERE resolved_at IS NULL;

ALTER TABLE alerts
  DROP CONSTRAINT alerts_site_fkey,
  ADD CONSTRAINT alerts_site_fkey FOREIGN KEY (site_id) REFERENCES sites (id) ON DELETE RESTRICT,
  DROP COLUMN feeder_id;

ALTER TABLE sites DROP CONSTRAINT sites_id_feeder_key;
