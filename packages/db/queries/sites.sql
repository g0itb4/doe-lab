-- Sites. Standard shapes: Get, List (keyset on nmi), Create, Update,
-- SoftDelete. A soft-deleted site is invisible to every query here.

-- name: GetSite :one
SELECT * FROM sites WHERE id = $1 AND deleted_at IS NULL;

-- name: GetSiteByNMI :one
SELECT * FROM sites WHERE nmi = $1 AND deleted_at IS NULL;

-- Served by sites_feeder_idx (feeder_id, nmi) WHERE deleted_at IS NULL.
--
-- name: ListSites :many
SELECT * FROM sites
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND deleted_at IS NULL
   AND nmi > sqlc.arg(after_nmi)
   AND (sqlc.narg(phase)::smallint IS NULL OR phase = sqlc.narg(phase))
 ORDER BY nmi
 LIMIT sqlc.arg(page_size);

-- Every site of a feeder, for the engine and the simulator.
--
-- name: ListAllSites :many
SELECT * FROM sites
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND deleted_at IS NULL
 ORDER BY nmi;

-- The sites that have a place on the map, of every feeder. Served by
-- sites_located_idx (nmi) WHERE latitude_deg IS NOT NULL AND deleted_at IS
-- NULL.
--
-- name: ListLocatedSites :many
SELECT * FROM sites
 WHERE latitude_deg IS NOT NULL
   AND deleted_at IS NULL
   AND nmi > sqlc.arg(after_nmi)
 ORDER BY nmi
 LIMIT sqlc.arg(page_size);

-- name: CreateSite :one
INSERT INTO sites (
  nmi, feeder_id, node_id, name, phase, pv_kw, inverter_kva, export_cap_w, import_cap_w,
  has_battery, battery_kwh, has_ev, profile_customer, latitude_deg, longitude_deg
) VALUES (
  sqlc.arg(nmi), sqlc.arg(feeder_id), sqlc.arg(node_id), sqlc.arg(name), sqlc.arg(phase),
  sqlc.arg(pv_kw), sqlc.arg(inverter_kva), sqlc.arg(export_cap_w), sqlc.arg(import_cap_w),
  sqlc.arg(has_battery), sqlc.narg(battery_kwh), sqlc.arg(has_ev), sqlc.narg(profile_customer),
  sqlc.narg(latitude_deg), sqlc.narg(longitude_deg)
)
RETURNING *;

-- The DER of a site and its caps. The NMI, the feeder, the node, the phase
-- and the location are where the site IS; they do not change.
--
-- name: UpdateSite :one
UPDATE sites
   SET pv_kw        = sqlc.arg(pv_kw),
       inverter_kva = sqlc.arg(inverter_kva),
       export_cap_w = sqlc.arg(export_cap_w),
       import_cap_w = sqlc.arg(import_cap_w),
       has_battery  = sqlc.arg(has_battery),
       battery_kwh  = sqlc.narg(battery_kwh),
       has_ev       = sqlc.arg(has_ev)
 WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteSite :one
UPDATE sites SET deleted_at = now()
 WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;
