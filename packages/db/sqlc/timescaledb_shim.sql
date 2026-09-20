-- Read by sqlc only, never applied to a database.
--
-- sqlc type-checks queries against the migrations, and the migrations call
-- TimescaleDB functions that sqlc's built-in catalog does not have. These
-- declarations give it their signatures. The bodies are irrelevant.

CREATE FUNCTION time_bucket(bucket_width interval, ts timestamptz) RETURNS timestamptz
  AS 'SELECT ts' LANGUAGE sql IMMUTABLE;

CREATE FUNCTION by_range(column_name text, partition_interval interval) RETURNS text
  AS 'SELECT column_name' LANGUAGE sql IMMUTABLE;

CREATE FUNCTION create_hypertable(relation text, dimension text) RETURNS text
  AS 'SELECT relation' LANGUAGE sql VOLATILE;

CREATE FUNCTION add_continuous_aggregate_policy(
  continuous_aggregate text, start_offset interval, end_offset interval, schedule_interval interval
) RETURNS integer
  AS 'SELECT 1' LANGUAGE sql VOLATILE;
