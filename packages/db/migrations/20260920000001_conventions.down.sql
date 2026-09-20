-- The guards have to go before the table they protect: reject_row_history_mutation
-- would otherwise refuse the DROP's implicit work.
DROP TRIGGER IF EXISTS row_history_no_truncate ON row_history;
DROP TRIGGER IF EXISTS row_history_append_only ON row_history;

-- Convention triggers live on tables owned by later migrations, which have
-- already been reverted by the time this runs. Nothing to unpick here.
DROP FUNCTION IF EXISTS prune_row_history();
DROP FUNCTION IF EXISTS assert_conventions();
DROP FUNCTION IF EXISTS apply_conventions();
DROP FUNCTION IF EXISTS drop_trigger_if_present(text, text);
DROP FUNCTION IF EXISTS ensure_trigger(text, text, text);
DROP FUNCTION IF EXISTS convention_targets();
DROP FUNCTION IF EXISTS changed_predicate(oid);
DROP FUNCTION IF EXISTS has_trigger(text, text);
DROP FUNCTION IF EXISTS has_column(oid, text);

DROP FUNCTION IF EXISTS cascade_soft_delete();
DROP FUNCTION IF EXISTS reject_row_history_mutation();
DROP FUNCTION IF EXISTS record_truncate_history();
DROP FUNCTION IF EXISTS record_row_history();
DROP FUNCTION IF EXISTS set_actor(text, text);
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS row_history;
DROP TABLE IF EXISTS row_history_retention;
DROP TABLE IF EXISTS audit_redactions;
DROP TABLE IF EXISTS convention_exemptions;

DROP TYPE IF EXISTS row_op;
DROP TYPE IF EXISTS convention;

-- timescaledb and uuidv7() are deliberately not reversed: they are
-- environment, not schema.
