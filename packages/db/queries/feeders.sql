-- Feeders. Standard shapes: Get, List (keyset on code), Create, Update.

-- name: GetFeeder :one
SELECT * FROM feeders WHERE id = $1;

-- name: GetFeederByCode :one
SELECT * FROM feeders WHERE code = $1;

-- name: ListFeeders :many
SELECT * FROM feeders
 WHERE code > sqlc.arg(after_code)
 ORDER BY code
 LIMIT sqlc.arg(page_size);

-- name: CreateFeeder :one
INSERT INTO feeders (
  code, name, nominal_voltage_v, transformer_kva, source_voltage_v, source_angle_deg,
  source_r_ohm, source_x_ohm, tap_pu, timezone, attribution, substation_id
) VALUES (
  sqlc.arg(code), sqlc.arg(name), sqlc.arg(nominal_voltage_v), sqlc.arg(transformer_kva),
  sqlc.arg(source_voltage_v), sqlc.arg(source_angle_deg), sqlc.arg(source_r_ohm),
  sqlc.arg(source_x_ohm), sqlc.arg(tap_pu), sqlc.arg(timezone), sqlc.arg(attribution),
  sqlc.narg(substation_id)
)
RETURNING *;

-- Only what an operator may change: the display name and the tap.
--
-- name: UpdateFeeder :one
UPDATE feeders
   SET name   = sqlc.arg(name),
       tap_pu = sqlc.arg(tap_pu)
 WHERE id = sqlc.arg(id)
RETURNING *;
