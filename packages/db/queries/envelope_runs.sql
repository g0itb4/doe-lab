-- Envelope runs. Standard shapes: Get, List (keyset, newest first), Create.
-- A run is finished by CompleteEnvelopeRun, not by a general update.

-- name: GetEnvelopeRun :one
SELECT * FROM envelope_runs WHERE id = $1;

-- name: GetEnvelopeRunByIdempotencyKey :one
SELECT * FROM envelope_runs WHERE idempotency_key = $1;

-- Newest first. Served by envelope_runs_feeder_started_idx
-- (feeder_id, started_at DESC, id DESC).
--
-- name: ListEnvelopeRuns :many
SELECT * FROM envelope_runs
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND (started_at, id) < (sqlc.arg(before_started_at)::timestamptz, sqlc.arg(before_id)::uuid)
   AND (sqlc.narg(status)::run_status IS NULL OR status = sqlc.narg(status))
 ORDER BY started_at DESC, id DESC
 LIMIT sqlc.arg(page_size);

-- Creating a run again with the same idempotency key creates nothing and
-- returns no row; the caller then reads the first run by its key.
--
-- name: CreateEnvelopeRun :one
INSERT INTO envelope_runs (
  feeder_id, envelope_config_id, idempotency_key, horizon_from, horizon_to, engine_version
) VALUES (
  sqlc.arg(feeder_id), sqlc.arg(envelope_config_id), sqlc.arg(idempotency_key),
  sqlc.arg(horizon_from), sqlc.arg(horizon_to), sqlc.arg(engine_version)
)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- Only a running run can be completed, so completing twice changes nothing
-- the second time.
--
-- name: CompleteEnvelopeRun :one
UPDATE envelope_runs
   SET status         = sqlc.arg(status),
       completed_at   = now(),
       duration_ms    = sqlc.arg(duration_ms),
       site_count     = sqlc.arg(site_count),
       interval_count = sqlc.arg(interval_count),
       error          = sqlc.narg(error)
 WHERE id = sqlc.arg(id) AND status = 'running'
RETURNING *;

-- name: AddEnvelopeRunCount :exec
UPDATE envelope_runs
   SET envelope_count = envelope_count + sqlc.arg(added)
 WHERE id = sqlc.arg(id);
