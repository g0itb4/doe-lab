-- Readings: device telemetry.

-- A batch, as parallel arrays. A reading that is already stored (the same
-- device and time) is skipped, which is what makes ingest safe to retry. The
-- site of each reading comes from its device, so a reading cannot name a site
-- its device does not belong to.
--
-- sqlc types the arrays as []float64, which has no way to say NULL, so an
-- absent state of charge or voltage travels as NaN and becomes NULL here.
--
-- name: InsertReadings :execrows
INSERT INTO readings (device_id, site_id, ts, power_w, net_export_w, soc_pct, voltage_v)
SELECT r.device_id, d.site_id, r.ts, r.power_w, r.net_export_w,
       NULLIF(r.soc_pct, 'NaN'::double precision), NULLIF(r.voltage_v, 'NaN'::double precision)
  FROM (SELECT unnest(sqlc.arg(device_ids)::uuid[])              AS device_id,
               unnest(sqlc.arg(tss)::timestamptz[])              AS ts,
               unnest(sqlc.arg(power_ws)::double precision[])    AS power_w,
               unnest(sqlc.arg(net_export_ws)::double precision[]) AS net_export_w,
               unnest(sqlc.arg(soc_pcts)::double precision[])    AS soc_pct,
               unnest(sqlc.arg(voltage_vs)::double precision[])  AS voltage_v) AS r
  JOIN devices d ON d.id = r.device_id AND d.deleted_at IS NULL
ON CONFLICT (device_id, ts) DO NOTHING;

-- The latest reading of each device of the batch. An older reading that
-- arrives late does not move a device's status back.
--
-- name: UpsertDeviceStatus :exec
INSERT INTO device_status (device_id, last_seen_at, power_w, net_export_w)
SELECT r.device_id, r.ts, r.power_w, r.net_export_w
  FROM (SELECT unnest(sqlc.arg(device_ids)::uuid[])                AS device_id,
               unnest(sqlc.arg(tss)::timestamptz[])                AS ts,
               unnest(sqlc.arg(power_ws)::double precision[])      AS power_w,
               unnest(sqlc.arg(net_export_ws)::double precision[]) AS net_export_w) AS r
  JOIN devices d ON d.id = r.device_id AND d.deleted_at IS NULL
ON CONFLICT (device_id) DO UPDATE
   SET last_seen_at = excluded.last_seen_at,
       power_w      = excluded.power_w,
       net_export_w = excluded.net_export_w
 WHERE excluded.last_seen_at > device_status.last_seen_at;

-- Served by readings_pkey (device_id, ts).
--
-- name: ListReadings :many
SELECT * FROM readings
 WHERE device_id = sqlc.arg(device_id)
   AND ts >= sqlc.arg(from_ts)
   AND ts < sqlc.arg(to_ts)
   AND ts > sqlc.arg(after_ts)
 ORDER BY ts
 LIMIT sqlc.arg(page_size);

-- One site's telemetry by the minute, for its chart.
--
-- name: ListSitePower :many
SELECT * FROM site_power_1m
 WHERE site_id = sqlc.arg(site_id)
   AND bucket >= sqlc.arg(from_ts)
   AND bucket < sqlc.arg(to_ts)
 ORDER BY bucket;

-- The fleet by the minute, for the overview chart.
--
-- name: ListFleetSeries :many
SELECT * FROM fleet_1m
 WHERE feeder_id = sqlc.arg(feeder_id)
   AND bucket >= sqlc.arg(from_ts)
   AND bucket < sqlc.arg(to_ts)
 ORDER BY bucket;

-- The live state of every device of a feeder: its site, and its latest
-- reading when it has sent one.
--
-- name: ListDeviceStates :many
SELECT d.id AS device_id, d.site_id, d.der_type, s.nmi,
       st.last_seen_at, st.power_w, st.net_export_w
  FROM devices d
  JOIN sites s ON s.id = d.site_id
  LEFT JOIN device_status st ON st.device_id = d.id
 WHERE s.feeder_id = sqlc.arg(feeder_id)
   AND d.deleted_at IS NULL
   AND s.deleted_at IS NULL
 ORDER BY s.nmi, d.der_type;
