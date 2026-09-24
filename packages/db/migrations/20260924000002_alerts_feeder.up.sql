-- Alerts are read by feeder, newest first: the operator's alert list and the
-- count of open alerts on the dashboard. With the feeder known only through
-- the site, that read is a join and a sort. So an alert carries its feeder,
-- and the indexes that serve the lists lead with it.
--
-- The column cannot drift from the site: the foreign key is on the pair.

ALTER TABLE sites ADD CONSTRAINT sites_id_feeder_key UNIQUE (id, feeder_id);

ALTER TABLE alerts ADD COLUMN feeder_id uuid;
UPDATE alerts a SET feeder_id = s.feeder_id FROM sites s WHERE s.id = a.site_id;
ALTER TABLE alerts
  ALTER COLUMN feeder_id SET NOT NULL,
  DROP CONSTRAINT alerts_site_fkey,
  ADD CONSTRAINT alerts_site_fkey
    FOREIGN KEY (site_id, feeder_id) REFERENCES sites (id, feeder_id) ON DELETE RESTRICT;

DROP INDEX alerts_opened_idx;
DROP INDEX alerts_open_idx;
-- ListAlerts: a feeder's alerts, newest first; and the open ones, which is
-- also CountOpenAlerts.
CREATE INDEX alerts_feeder_opened_idx ON alerts (feeder_id, opened_at DESC, id DESC);
CREATE INDEX alerts_feeder_open_idx ON alerts (feeder_id, opened_at DESC, id DESC) WHERE resolved_at IS NULL;

COMMENT ON COLUMN alerts.feeder_id IS 'The feeder of the alert''s site, kept on the row so that a feeder''s alerts are read from one index.';
