-- Envelope run intervals: the feeder's forecast state per interval of a run.

-- name: InsertEnvelopeRunIntervals :copyfrom
INSERT INTO envelope_run_intervals (
  envelope_run_id, feeder_id, valid_from, valid_to,
  forecast_net_load_w, forecast_loading_pct, forecast_v_min_pu, forecast_v_max_pu,
  export_limit_total_w, import_limit_total_w, static_limit_total_w,
  static_v_max_pu, static_binding, static_binding_element, envelope_v_max_pu
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: ListEnvelopeRunIntervals :many
SELECT * FROM envelope_run_intervals
 WHERE envelope_run_id = sqlc.arg(envelope_run_id)
 ORDER BY valid_from;

-- The feeder's series: for each interval of the range, the row of the latest
-- run that covered it. Served by envelope_run_intervals_feeder_idx
-- (feeder_id, valid_from, created_at DESC).
--
-- name: ListFeederIntervals :many
SELECT DISTINCT ON (valid_from) *
  FROM envelope_run_intervals
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND valid_from >= sqlc.arg(from_ts)
   AND valid_from < sqlc.arg(to_ts)
 ORDER BY valid_from, created_at DESC;
