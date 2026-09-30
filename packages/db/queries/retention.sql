-- Retention. The cutoffs are in feeder time; see the migration that defines
-- purge_before for what goes and what never does.

-- name: PurgeBefore :one
SELECT purge_before(
  sqlc.arg(readings_before)::timestamptz,
  sqlc.arg(envelopes_before)::timestamptz,
  sqlc.arg(alerts_before)::timestamptz
)::bigint AS deleted;
