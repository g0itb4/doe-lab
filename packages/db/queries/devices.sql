-- Devices. Standard shapes: Get, List (keyset on id), Create, Update,
-- SoftDelete.

-- name: GetDevice :one
SELECT * FROM devices WHERE id = $1 AND deleted_at IS NULL;

-- Served by devices_site_type_live_key (site_id, der_type) WHERE deleted_at
-- IS NULL when a site is given, and by the primary key otherwise.
--
-- name: ListDevices :many
SELECT * FROM devices
 WHERE deleted_at IS NULL
   AND id > sqlc.arg(after_id)
   AND (sqlc.narg(site_id)::uuid IS NULL OR site_id = sqlc.narg(site_id))
   AND (sqlc.narg(der_type)::der_type IS NULL OR der_type = sqlc.narg(der_type))
 ORDER BY id
 LIMIT sqlc.arg(page_size);

-- Every device of a feeder with its site's NMI, for the simulator.
--
-- name: ListFeederDevices :many
SELECT d.*, s.nmi
  FROM devices d
  JOIN sites s ON s.id = d.site_id
 WHERE s.feeder_id = sqlc.arg(feeder_id)
   AND d.deleted_at IS NULL
   AND s.deleted_at IS NULL
 ORDER BY s.nmi, d.der_type;

-- name: CreateDevice :one
INSERT INTO devices (site_id, der_type, rated_w)
VALUES (sqlc.arg(site_id), sqlc.arg(der_type), sqlc.arg(rated_w))
RETURNING *;

-- name: UpdateDevice :one
UPDATE devices
   SET rated_w = sqlc.arg(rated_w)
 WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteDevice :one
UPDATE devices SET deleted_at = now()
 WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;
