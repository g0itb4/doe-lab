-- Substations. Created by the import; read-only afterwards. Standard shapes:
-- Get, List (keyset on code), Create.

-- name: GetSubstation :one
SELECT * FROM substations WHERE id = $1;

-- name: GetSubstationByCode :one
SELECT * FROM substations WHERE code = $1;

-- name: ListSubstations :many
SELECT * FROM substations
 WHERE code > sqlc.arg(after_code)
 ORDER BY code
 LIMIT sqlc.arg(page_size);

-- name: CreateSubstation :one
INSERT INTO substations (code, name, dnsp, state, latitude_deg, longitude_deg)
VALUES (sqlc.arg(code), sqlc.arg(name), sqlc.arg(dnsp), sqlc.arg(state),
        sqlc.arg(latitude_deg), sqlc.arg(longitude_deg))
RETURNING *;
