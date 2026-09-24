-- Backstop events. Created by a trigger, ended by a clear; never deleted.

-- name: GetBackstopEvent :one
SELECT * FROM backstop_events WHERE id = $1;

-- The backstop in force for a feeder, if any. Served by
-- backstop_events_one_active_key.
--
-- name: GetActiveBackstopEvent :one
SELECT * FROM backstop_events
 WHERE feeder_id = sqlc.arg(feeder_id) AND cleared_at IS NULL;

-- Newest first. Served by backstop_events_feeder_triggered_idx.
--
-- name: ListBackstopEvents :many
SELECT * FROM backstop_events
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND (triggered_at, id) < (sqlc.arg(before_triggered_at)::timestamptz, sqlc.arg(before_id)::uuid)
 ORDER BY triggered_at DESC, id DESC
 LIMIT sqlc.arg(page_size);

-- name: CreateBackstopEvent :one
INSERT INTO backstop_events (feeder_id, reason, export_limit_w, triggered_by, triggered_at)
VALUES (sqlc.arg(feeder_id), sqlc.arg(reason), sqlc.arg(export_limit_w), sqlc.arg(triggered_by), sqlc.arg(triggered_at))
RETURNING *;

-- name: InsertBackstopEventSites :exec
INSERT INTO backstop_event_sites (backstop_event_id, site_id)
SELECT sqlc.arg(backstop_event_id), unnest(sqlc.arg(site_ids)::uuid[]);

-- name: ListBackstopEventSiteIDs :many
SELECT site_id FROM backstop_event_sites
 WHERE backstop_event_id = sqlc.arg(backstop_event_id)
 ORDER BY site_id;

-- Only an active backstop can be cleared.
--
-- name: ClearBackstopEvent :one
UPDATE backstop_events
   SET cleared_at = greatest(sqlc.arg(cleared_at)::timestamptz, triggered_at + interval '1 microsecond'),
       cleared_by = sqlc.arg(cleared_by)
 WHERE id = sqlc.arg(id) AND cleared_at IS NULL
RETURNING *;

-- The envelope that the engine last gave each site for each interval from an
-- instant on, whether or not it is still active: what to restore when a
-- backstop that superseded it is cleared.
--
-- name: ListLatestEngineEnvelopes :many
SELECT DISTINCT ON (site_id, valid_from) *
  FROM envelopes
 WHERE site_id = ANY (sqlc.arg(site_ids)::uuid[])
   AND source = 'engine'
   AND valid_to > sqlc.arg(from_ts)
 ORDER BY site_id, valid_from, created_at DESC, id DESC;

-- The intervals from an instant on in which a site has an active envelope:
-- what a backstop has to take over.
--
-- name: ListActiveEnvelopes :many
SELECT * FROM envelopes
 WHERE site_id = ANY (sqlc.arg(site_ids)::uuid[])
   AND superseded_at IS NULL
   AND valid_to > sqlc.arg(from_ts)
 ORDER BY site_id, valid_from;
