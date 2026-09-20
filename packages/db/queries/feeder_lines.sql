-- Feeder lines. Created by the import; an operator may correct a rating.

-- name: GetFeederLine :one
SELECT * FROM feeder_lines WHERE id = $1;

-- name: ListFeederLines :many
SELECT * FROM feeder_lines
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND name > sqlc.arg(after_name)
 ORDER BY name
 LIMIT sqlc.arg(page_size);

-- name: ListAllFeederLines :many
SELECT * FROM feeder_lines
 WHERE feeder_id = sqlc.arg(feeder_id)
 ORDER BY name;

-- name: CreateFeederLine :one
INSERT INTO feeder_lines (
  feeder_id, name, from_node_id, to_node_id, linecode, length_m, is_switch,
  r_ohm, x_ohm, b_s, ampacity_a, ampacity_source
) VALUES (
  sqlc.arg(feeder_id), sqlc.arg(name), sqlc.arg(from_node_id), sqlc.arg(to_node_id),
  sqlc.arg(linecode), sqlc.arg(length_m), sqlc.arg(is_switch),
  sqlc.arg(r_ohm), sqlc.arg(x_ohm), sqlc.arg(b_s),
  sqlc.narg(ampacity_a), sqlc.narg(ampacity_source)
)
RETURNING *;

-- A rating set here is the operator's, whatever it was before. Clearing it
-- (NULL) makes the line unrated.
--
-- name: UpdateFeederLineAmpacity :one
UPDATE feeder_lines
   SET ampacity_a      = sqlc.narg(ampacity_a),
       ampacity_source = CASE WHEN sqlc.narg(ampacity_a)::double precision IS NULL
                              THEN NULL ELSE 'operator'::ampacity_source END
 WHERE id = sqlc.arg(id)
RETURNING *;
