-- Alerts. The compliance service opens and resolves them; an operator can
-- acknowledge one. There is no general update and no delete.

-- name: GetAlert :one
SELECT * FROM alerts WHERE id = $1;

-- Newest first. A feeder's alerts, optionally of one site, one kind, or only
-- the open ones. Served by alerts_feeder_opened_idx, alerts_feeder_open_idx
-- and alerts_site_opened_idx.
--
-- name: ListAlerts :many
SELECT *
  FROM alerts
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND (opened_at, id) < (sqlc.arg(before_opened_at)::timestamptz, sqlc.arg(before_id)::uuid)
   AND (sqlc.narg(site_id)::uuid IS NULL OR site_id = sqlc.narg(site_id))
   AND (sqlc.narg(kind)::alert_kind IS NULL OR kind = sqlc.narg(kind))
   AND (NOT sqlc.arg(open_only)::boolean OR resolved_at IS NULL)
 ORDER BY opened_at DESC, id DESC
 LIMIT sqlc.arg(page_size);

-- Opens an alert, or, when the site already has an open alert of the kind,
-- raises that alert's peak. One open alert per site and kind: a breach that
-- continues is one alert, not one per reading.
--
-- name: OpenAlert :one
INSERT INTO alerts (site_id, feeder_id, device_id, kind, severity, opened_at, limit_w, peak_w, detail)
VALUES (sqlc.arg(site_id), sqlc.arg(feeder_id), sqlc.narg(device_id), sqlc.arg(kind), sqlc.arg(severity),
        sqlc.arg(opened_at), sqlc.narg(limit_w), sqlc.narg(peak_w), sqlc.arg(detail))
ON CONFLICT (site_id, kind) WHERE resolved_at IS NULL DO UPDATE
   SET peak_w = greatest(alerts.peak_w, excluded.peak_w),
       severity = greatest(alerts.severity, excluded.severity)
RETURNING *, (xmax = 0) AS opened;

-- Resolves the open alert of a site and kind, if there is one.
--
-- name: ResolveAlert :execrows
UPDATE alerts
   SET resolved_at = greatest(sqlc.arg(resolved_at)::timestamptz, opened_at)
 WHERE site_id = sqlc.arg(site_id)
   AND kind = sqlc.arg(kind)
   AND resolved_at IS NULL;

-- name: AcknowledgeAlert :one
UPDATE alerts
   SET acknowledged_at = now(),
       acknowledged_by = sqlc.arg(acknowledged_by)
 WHERE id = sqlc.arg(id) AND acknowledged_at IS NULL
RETURNING *;

-- Served by alerts_feeder_open_idx.
--
-- name: CountOpenAlerts :one
SELECT count(*) FROM alerts
 WHERE feeder_id = sqlc.arg(feeder_id) AND resolved_at IS NULL;
