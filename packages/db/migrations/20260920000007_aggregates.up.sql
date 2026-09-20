-- Rollups for the fleet view and for compliance.

-- One-minute rollup of telemetry per site. A site's devices all report the
-- same net export (they share the meter), so the average over their readings
-- is the site's.
CREATE MATERIALIZED VIEW site_power_1m
WITH (timescaledb.continuous) AS
SELECT site_id,
       time_bucket(INTERVAL '1 minute', ts) AS bucket,
       avg(net_export_w)::double precision  AS avg_net_export_w,
       max(net_export_w)::double precision  AS max_net_export_w,
       avg(soc_pct)::double precision       AS avg_soc_pct,
       avg(voltage_v)::double precision     AS avg_voltage_v,
       count(*)::bigint                     AS reading_count
  FROM readings
 GROUP BY site_id, time_bucket(INTERVAL '1 minute', ts)
WITH NO DATA;

-- Real-time: a query sees the rows that the refresh policy has not
-- materialised yet. Feeder time can run ahead of the wall clock, so the
-- policy's window is open at both ends rather than relative to now().
ALTER MATERIALIZED VIEW site_power_1m SET (timescaledb.materialized_only = false);

SELECT add_continuous_aggregate_policy('site_power_1m',
  start_offset      => NULL,
  end_offset        => NULL,
  schedule_interval => INTERVAL '1 minute');

COMMENT ON VIEW site_power_1m IS
  'Continuous aggregate: one-minute net export, state of charge and voltage per site.';

-- The fleet, per feeder and minute.
CREATE VIEW fleet_1m AS
SELECT s.feeder_id,
       p.bucket,
       sum(greatest(p.avg_net_export_w, 0))::double precision  AS export_w,
       sum(greatest(-p.avg_net_export_w, 0))::double precision AS import_w,
       avg(p.avg_soc_pct)::double precision                    AS avg_soc_pct,
       count(*)::bigint                                        AS reporting_sites,
       sum(p.reading_count)::bigint                            AS reading_count
  FROM site_power_1m p
  JOIN sites s ON s.id = p.site_id
 GROUP BY s.feeder_id, p.bucket;

COMMENT ON VIEW fleet_1m IS
  'One-minute fleet totals per feeder: export, import, average state of charge, and how many sites reported.';

-- Each site's minute against the envelope that was active for it.
CREATE VIEW site_compliance_1m AS
SELECT p.site_id,
       p.bucket,
       p.max_net_export_w,
       e.export_limit_w,
       greatest(p.max_net_export_w - e.export_limit_w, 0)::double precision AS over_limit_w,
       e.source                                                             AS envelope_source
  FROM site_power_1m p
  JOIN envelopes e
    ON e.site_id = p.site_id
   AND e.superseded_at IS NULL
   AND p.bucket >= e.valid_from
   AND p.bucket < e.valid_to;

COMMENT ON VIEW site_compliance_1m IS
  'One-minute net export of each site beside the export limit that was in force, and the excess over it.';
