DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS envelope_runs;
DROP TYPE IF EXISTS run_status;
DROP TRIGGER IF EXISTS envelope_configs_immutable ON envelope_configs;
DROP TABLE IF EXISTS envelope_configs;
DROP FUNCTION IF EXISTS reject_mutation();
DROP TYPE IF EXISTS envelope_policy;
