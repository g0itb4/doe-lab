-- Feeder states: the latest solved voltage at each bus and flow in each line,
-- per interval. A run replaces the intervals it covers: delete, then copy.

-- name: DeleteFeederNodeStates :exec
DELETE FROM feeder_node_states
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND valid_from = ANY(sqlc.arg(valid_froms)::timestamptz[]);

-- name: InsertFeederNodeStates :copyfrom
INSERT INTO feeder_node_states (
  feeder_id, node_id, valid_from, valid_to, envelope_run_id,
  forecast_v_pu, envelope_v_pu, static_v_pu
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: DeleteFeederLineStates :exec
DELETE FROM feeder_line_states
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND valid_from = ANY(sqlc.arg(valid_froms)::timestamptz[]);

-- name: InsertFeederLineStates :copyfrom
INSERT INTO feeder_line_states (
  feeder_id, line_id, valid_from, valid_to, envelope_run_id,
  forecast_current_a, envelope_current_a, static_current_a,
  forecast_power_w, envelope_power_w, static_power_w
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- The states of the interval that holds an instant. The interval is found
-- first, from the primary key (feeder_id, valid_from, node_id): the latest
-- start at or before the instant. A feeder whose latest interval ended before
-- the instant has none.
--
-- name: ListFeederNodeStates :many
SELECT * FROM feeder_node_states s
 WHERE s.feeder_id = sqlc.arg(feeder_id)
   AND s.valid_from = (
     SELECT max(i.valid_from) FROM feeder_node_states i
      WHERE i.feeder_id = sqlc.arg(feeder_id) AND i.valid_from <= sqlc.arg(at)
   )
   AND s.valid_to > sqlc.arg(at)
 ORDER BY s.node_id;

-- name: ListFeederLineStates :many
SELECT * FROM feeder_line_states s
 WHERE s.feeder_id = sqlc.arg(feeder_id)
   AND s.valid_from = (
     SELECT max(i.valid_from) FROM feeder_line_states i
      WHERE i.feeder_id = sqlc.arg(feeder_id) AND i.valid_from <= sqlc.arg(at)
   )
   AND s.valid_to > sqlc.arg(at)
 ORDER BY s.line_id;
