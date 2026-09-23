-- Envelopes. Rows are immutable: there is no update but supersession, and no
-- delete.

-- The envelope in force for a site at an instant. Served by
-- envelopes_one_active_key (site_id, valid_from) WHERE superseded_at IS NULL.
--
-- name: GetCurrentEnvelope :one
SELECT * FROM envelopes
 WHERE site_id = sqlc.arg(site_id)
   AND superseded_at IS NULL
   AND valid_from <= sqlc.arg(at)
   AND valid_to > sqlc.arg(at)
 ORDER BY valid_from DESC
 LIMIT 1;

-- The envelopes of a site over a range, in time order. With
-- include_superseded, the superseded rows too: the audit trail.
--
-- name: ListEnvelopes :many
SELECT * FROM envelopes
 WHERE site_id = sqlc.arg(site_id)
   AND valid_from >= sqlc.arg(from_ts)
   AND valid_from < sqlc.arg(to_ts)
   AND (valid_from, id) > (sqlc.arg(after_valid_from)::timestamptz, sqlc.arg(after_id)::uuid)
   AND (sqlc.arg(include_superseded)::boolean OR superseded_at IS NULL)
 ORDER BY valid_from, id
 LIMIT sqlc.arg(page_size);

-- Everything a run published, for its export. Served by envelopes_run_idx
-- (envelope_run_id, valid_from, site_id).
--
-- name: ListRunEnvelopes :many
SELECT * FROM envelopes
 WHERE envelope_run_id = sqlc.arg(envelope_run_id)
   AND (valid_from, site_id) > (sqlc.arg(after_valid_from)::timestamptz, sqlc.arg(after_site_id)::uuid)
 ORDER BY valid_from, site_id
 LIMIT sqlc.arg(page_size);

-- The active envelopes of every site of a feeder over a range: the feeder's
-- total envelope through time.
--
-- name: ListFeederEnvelopes :many
SELECT e.*
  FROM envelopes e
  JOIN sites s ON s.id = e.site_id
 WHERE s.feeder_id = sqlc.arg(feeder_id)
   AND s.deleted_at IS NULL
   AND e.superseded_at IS NULL
   AND e.valid_from >= sqlc.arg(from_ts)
   AND e.valid_from < sqlc.arg(to_ts)
 ORDER BY e.valid_from, s.nmi;

-- Supersedes the active envelope of each (site, interval) pair, so its
-- successor can be inserted. The arrays are parallel.
--
-- name: SupersedeEnvelopes :execrows
UPDATE envelopes e
   SET superseded_at = now()
  FROM (SELECT unnest(sqlc.arg(site_ids)::uuid[])           AS site_id,
               unnest(sqlc.arg(valid_froms)::timestamptz[]) AS valid_from) AS k
 WHERE e.site_id = k.site_id
   AND e.valid_from = k.valid_from
   AND e.superseded_at IS NULL;

-- name: InsertEnvelopes :copyfrom
INSERT INTO envelopes (
  id, site_id, valid_from, valid_to, export_limit_w, import_limit_w, source,
  envelope_run_id, backstop_event_id, export_binding, export_binding_element,
  import_binding, import_binding_element
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);
