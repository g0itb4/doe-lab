DELETE FROM row_history_retention WHERE table_name IN ('envelope_runs', 'alerts', 'backstop_events');
DROP FUNCTION purge_before(timestamptz, timestamptz, timestamptz);
