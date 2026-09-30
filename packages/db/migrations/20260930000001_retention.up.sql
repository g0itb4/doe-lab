-- Retention: what the feeder's history is allowed to grow to.
--
-- The time-series tables grow with every reading and every run of the
-- engine, and a demo that runs feeder time at sixty times the wall clock
-- writes a day of them every 24 minutes. Nothing else in the schema ever
-- removes a row.
--
-- TimescaleDB's own retention policy cannot do this: it compares a chunk
-- with now(), and feeder time is not the wall clock. The cutoffs are given
-- by the caller instead, in feeder time, and the API calls purge_before on a
-- timer.

CREATE FUNCTION purge_before(
  readings_before  timestamptz,
  envelopes_before timestamptz,
  alerts_before    timestamptz
) RETURNS bigint AS $$
DECLARE
  deleted bigint := 0;
  n       bigint;
BEGIN
  -- Whole chunks, so what goes is at most a chunk older than the cutoff says:
  -- a day of readings, a week of envelopes. A chunk is dropped as a table,
  -- which is how an immutable envelope leaves: no row is ever deleted from
  -- one.
  PERFORM drop_chunks('readings', older_than => readings_before);
  PERFORM drop_chunks('site_power_1m', older_than => readings_before);
  PERFORM drop_chunks('envelopes', older_than => envelopes_before);

  -- A run goes once its horizon has passed and its envelopes have gone with
  -- their chunks. Its intervals and idempotency keys go with it.
  DELETE FROM envelope_runs r
   WHERE r.horizon_to < envelopes_before
     AND NOT EXISTS (SELECT 1 FROM envelopes e WHERE e.envelope_run_id = r.id);
  GET DIAGNOSTICS n = ROW_COUNT;
  deleted := deleted + n;

  -- An alert goes once it has been resolved for long enough. An open alert
  -- never does.
  DELETE FROM alerts a WHERE a.resolved_at < alerts_before;
  GET DIAGNOSTICS n = ROW_COUNT;
  deleted := deleted + n;

  -- A backstop goes the same way, once cleared and once its envelopes have
  -- gone. An active backstop never does.
  DELETE FROM backstop_events b
   WHERE b.cleared_at < alerts_before
     AND NOT EXISTS (SELECT 1 FROM envelopes e WHERE e.backstop_event_id = b.id);
  GET DIAGNOSTICS n = ROW_COUNT;
  deleted := deleted + n;

  -- The audit log of the rows above, by the wall clock.
  PERFORM prune_row_history();
  RETURN deleted;
END $$ LANGUAGE plpgsql;

COMMENT ON FUNCTION purge_before(timestamptz, timestamptz, timestamptz) IS
  'Removes history older than the cutoffs, which are in feeder time: chunks of readings and of envelopes, then the runs, resolved alerts and cleared backstops that nothing refers to any more. Returns the number of runs, alerts and backstops deleted.';

-- The audit log of the tables that change with every run and every alert.
-- The network model and the configs are kept forever: they change by hand.
INSERT INTO row_history_retention (table_name, keep, reason) VALUES
  ('envelope_runs',   interval '14 days', 'A row per run of the engine, updated as it publishes; the run itself is the record.'),
  ('alerts',          interval '90 days', 'Opened and resolved by the compliance check many times a day.'),
  ('backstop_events', interval '365 days', 'Operator actions: kept for a year.');
