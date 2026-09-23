-- Idempotency keys: replay protection for writes that must happen once.

-- Claims a key. No row comes back when the key is already taken; the caller
-- then reads it to compare the request hash.
--
-- name: ClaimIdempotencyKey :one
INSERT INTO idempotency_keys (scope, key, request_hash, envelope_run_id)
VALUES (sqlc.arg(scope), sqlc.arg(key), sqlc.arg(request_hash), sqlc.narg(envelope_run_id))
ON CONFLICT (scope, key) DO NOTHING
RETURNING *;

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE scope = sqlc.arg(scope) AND key = sqlc.arg(key);

-- Served by idempotency_keys_expires_idx.
--
-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys WHERE expires_at < sqlc.arg(now);
