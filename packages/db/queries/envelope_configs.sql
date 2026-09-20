-- Envelope configs: immutable versions. There is no update and no delete; the
-- schema refuses both.

-- name: GetEnvelopeConfig :one
SELECT * FROM envelope_configs WHERE id = $1;

-- The active config is the highest version.
--
-- name: GetActiveEnvelopeConfig :one
SELECT * FROM envelope_configs
 WHERE feeder_id = sqlc.arg(feeder_id)
 ORDER BY version DESC
 LIMIT 1;

-- Newest first. Served by envelope_configs_version_key (feeder_id, version).
--
-- name: ListEnvelopeConfigs :many
SELECT * FROM envelope_configs
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND version < sqlc.arg(before_version)
 ORDER BY version DESC
 LIMIT sqlc.arg(page_size);

-- The next version is computed in the same statement. Two concurrent creates
-- can pick the same number; envelope_configs_version_key makes one of them
-- fail, and the service retries it.
--
-- name: CreateEnvelopeConfig :one
INSERT INTO envelope_configs (
  feeder_id, version, policy, v_min_pu, v_max_pu, transformer_limit_pct, line_limit_pct,
  pv_scale, static_limit_w, interval_minutes, horizon_intervals, breach_grace_seconds,
  offline_after_seconds, note, created_by
)
SELECT sqlc.arg(feeder_id),
       COALESCE(max(c.version), 0) + 1,
       sqlc.arg(policy), sqlc.arg(v_min_pu), sqlc.arg(v_max_pu),
       sqlc.arg(transformer_limit_pct), sqlc.arg(line_limit_pct), sqlc.arg(pv_scale),
       sqlc.arg(static_limit_w), sqlc.arg(interval_minutes), sqlc.arg(horizon_intervals),
       sqlc.arg(breach_grace_seconds), sqlc.arg(offline_after_seconds), sqlc.arg(note),
       sqlc.arg(created_by)
  FROM envelope_configs c
 WHERE c.feeder_id = sqlc.arg(feeder_id)
RETURNING *;
