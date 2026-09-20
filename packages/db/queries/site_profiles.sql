-- Site profiles: the imported half-hourly load and PV.

-- name: InsertSiteProfiles :copyfrom
INSERT INTO site_profiles (site_id, ts, load_w, pv_w, controlled_load_w)
VALUES ($1, $2, $3, $4, $5);

-- name: ListSiteProfiles :many
SELECT * FROM site_profiles
 WHERE site_id = sqlc.arg(site_id)
   AND ts >= sqlc.arg(from_ts)
   AND ts < sqlc.arg(to_ts)
 ORDER BY ts
 LIMIT sqlc.arg(page_size);

-- The forecast of every site of a feeder over a range, for the engine.
--
-- name: ListFeederProfiles :many
SELECT p.*
  FROM site_profiles p
  JOIN sites s ON s.id = p.site_id
 WHERE s.feeder_id = sqlc.arg(feeder_id)
   AND s.deleted_at IS NULL
   AND p.ts >= sqlc.arg(from_ts)
   AND p.ts < sqlc.arg(to_ts)
 ORDER BY p.ts, s.nmi;

-- name: CountSiteProfiles :one
SELECT count(*) FROM site_profiles WHERE site_id = sqlc.arg(site_id);

-- name: DeleteSiteProfiles :exec
DELETE FROM site_profiles WHERE site_id = sqlc.arg(site_id);
