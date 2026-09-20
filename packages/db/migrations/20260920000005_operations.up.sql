-- What operators do and see: emergency backstops and alerts.

CREATE TABLE backstop_events (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  feeder_id      uuid             NOT NULL,
  reason         text             NOT NULL,
  export_limit_w double precision NOT NULL DEFAULT 0,
  triggered_by   text             NOT NULL,
  triggered_at   timestamptz      NOT NULL,
  cleared_by     text,
  cleared_at     timestamptz,
  created_at     timestamptz      NOT NULL DEFAULT now(),
  updated_at     timestamptz      NOT NULL DEFAULT now(),
  CONSTRAINT backstop_events_feeder_fkey FOREIGN KEY (feeder_id) REFERENCES feeders (id) ON DELETE RESTRICT,
  CONSTRAINT backstop_events_reason_present CHECK (reason <> ''),
  CONSTRAINT backstop_events_limit_not_negative CHECK (export_limit_w >= 0),
  CONSTRAINT backstop_events_triggered_by_present CHECK (triggered_by <> ''),
  CONSTRAINT backstop_events_cleared_complete CHECK ((cleared_at IS NULL) = (cleared_by IS NULL)),
  CONSTRAINT backstop_events_cleared_after_trigger CHECK (cleared_at > triggered_at)
);

-- One active backstop per feeder. A second trigger while one is active is a
-- conflict, not a second event.
CREATE UNIQUE INDEX backstop_events_one_active_key ON backstop_events (feeder_id) WHERE cleared_at IS NULL;
-- ListBackstopEvents: newest first.
CREATE INDEX backstop_events_feeder_triggered_idx ON backstop_events (feeder_id, triggered_at DESC, id DESC);

COMMENT ON TABLE backstop_events IS
  'An emergency backstop: an operator overrides the envelopes of a set of sites with a fixed export limit until it is cleared.';
COMMENT ON COLUMN backstop_events.export_limit_w IS 'The export limit imposed on every affected site. 0 stops export.';
COMMENT ON COLUMN backstop_events.triggered_at IS 'Feeder time the backstop took effect.';
COMMENT ON COLUMN backstop_events.cleared_at IS 'Feeder time the backstop ended. NULL while it is active.';

CREATE TABLE backstop_event_sites (
  backstop_event_id uuid NOT NULL,
  site_id           uuid NOT NULL,
  CONSTRAINT backstop_event_sites_pkey PRIMARY KEY (backstop_event_id, site_id),
  CONSTRAINT backstop_event_sites_event_fkey
    FOREIGN KEY (backstop_event_id) REFERENCES backstop_events (id) ON DELETE CASCADE,
  CONSTRAINT backstop_event_sites_site_fkey FOREIGN KEY (site_id) REFERENCES sites (id) ON DELETE RESTRICT
);

-- "Is this site under a backstop?"
CREATE INDEX backstop_event_sites_site_idx ON backstop_event_sites (site_id);

COMMENT ON TABLE backstop_event_sites IS 'The sites a backstop covers.';

CREATE TYPE alert_kind AS ENUM ('constraint_breach', 'device_offline');
CREATE TYPE alert_severity AS ENUM ('info', 'warning', 'critical');

CREATE TABLE alerts (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  site_id         uuid           NOT NULL,
  device_id       uuid,
  kind            alert_kind     NOT NULL,
  severity        alert_severity NOT NULL,
  opened_at       timestamptz    NOT NULL,
  resolved_at     timestamptz,
  acknowledged_at timestamptz,
  acknowledged_by text,
  limit_w         double precision,
  peak_w          double precision,
  detail          text           NOT NULL DEFAULT '',
  created_at      timestamptz    NOT NULL DEFAULT now(),
  updated_at      timestamptz    NOT NULL DEFAULT now(),
  CONSTRAINT alerts_site_fkey FOREIGN KEY (site_id) REFERENCES sites (id) ON DELETE RESTRICT,
  -- The device, when there is one, belongs to the alert's site.
  CONSTRAINT alerts_device_fkey FOREIGN KEY (device_id, site_id) REFERENCES devices (id, site_id) ON DELETE RESTRICT,
  CONSTRAINT alerts_resolved_after_opened CHECK (resolved_at >= opened_at),
  CONSTRAINT alerts_acknowledged_complete CHECK ((acknowledged_at IS NULL) = (acknowledged_by IS NULL)),
  -- A breach records the limit and the worst export seen against it.
  CONSTRAINT alerts_breach_measured
    CHECK (kind <> 'constraint_breach' OR (limit_w IS NOT NULL AND peak_w IS NOT NULL AND peak_w >= limit_w)),
  CONSTRAINT alerts_limit_not_negative CHECK (limit_w >= 0),
  -- An offline alert names the device that went silent.
  CONSTRAINT alerts_offline_names_device CHECK (kind <> 'device_offline' OR device_id IS NOT NULL)
);

-- One open alert per site and kind: a continuing breach updates its alert
-- rather than opening a new one every reading.
CREATE UNIQUE INDEX alerts_one_open_key ON alerts (site_id, kind) WHERE resolved_at IS NULL;
-- ListAlerts: newest first; the open ones; one site's.
CREATE INDEX alerts_opened_idx ON alerts (opened_at DESC, id DESC);
CREATE INDEX alerts_open_idx ON alerts (opened_at DESC, id DESC) WHERE resolved_at IS NULL;
CREATE INDEX alerts_site_opened_idx ON alerts (site_id, opened_at DESC, id DESC);
CREATE INDEX alerts_device_idx ON alerts (device_id);

COMMENT ON TABLE alerts IS
  'A compliance or health alert on a site. The compliance service opens and resolves alerts; an operator can only acknowledge one.';
COMMENT ON COLUMN alerts.opened_at IS 'Feeder time the condition began.';
COMMENT ON COLUMN alerts.resolved_at IS 'Feeder time the condition cleared. NULL while the alert is open.';
COMMENT ON COLUMN alerts.limit_w IS 'For a breach: the export limit in force.';
COMMENT ON COLUMN alerts.peak_w IS 'For a breach: the largest net export seen while it lasted.';

SELECT apply_conventions();
