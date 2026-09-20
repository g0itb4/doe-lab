-- Feeder nodes. Created by the import; read-only afterwards.

-- name: GetFeederNode :one
SELECT * FROM feeder_nodes WHERE id = $1;

-- name: ListFeederNodes :many
SELECT * FROM feeder_nodes
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND name > sqlc.arg(after_name)
 ORDER BY name
 LIMIT sqlc.arg(page_size);

-- The whole tree, for the engine. A feeder has a few hundred nodes.
--
-- name: ListFeederTree :many
SELECT * FROM feeder_nodes
 WHERE feeder_id = sqlc.arg(feeder_id)
 ORDER BY name;

-- name: CreateFeederNode :one
INSERT INTO feeder_nodes (feeder_id, name, parent_node_id, ground_r_ohm, ground_x_ohm)
VALUES (sqlc.arg(feeder_id), sqlc.arg(name), sqlc.narg(parent_node_id),
        sqlc.narg(ground_r_ohm), sqlc.narg(ground_x_ohm))
RETURNING *;
