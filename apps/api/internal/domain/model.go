package domain

import (
	"time"

	"github.com/google/uuid"
)

// Feeder mirrors the feeders table.
type Feeder struct {
	ID              uuid.UUID `db:"id"`
	Code            string    `db:"code"`
	Name            string    `db:"name"`
	NominalVoltageV float64   `db:"nominal_voltage_v"`
	TransformerKVA  float64   `db:"transformer_kva"`
	SourceVoltageV  float64   `db:"source_voltage_v"`
	SourceAngleDeg  float64   `db:"source_angle_deg"`
	SourceROhm      float64   `db:"source_r_ohm"`
	SourceXOhm      float64   `db:"source_x_ohm"`
	TapPU           float64   `db:"tap_pu"`
	Timezone        string    `db:"timezone"`
	Attribution     string    `db:"attribution"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

// FeederNode mirrors the feeder_nodes table.
type FeederNode struct {
	ID           uuid.UUID  `db:"id"`
	FeederID     uuid.UUID  `db:"feeder_id"`
	Name         string     `db:"name"`
	ParentNodeID *uuid.UUID `db:"parent_node_id"`
	GroundROhm   *float64   `db:"ground_r_ohm"`
	GroundXOhm   *float64   `db:"ground_x_ohm"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

// FeederLine mirrors the feeder_lines table. The matrices are 4x4,
// row-major: 16 values.
type FeederLine struct {
	ID             uuid.UUID       `db:"id"`
	FeederID       uuid.UUID       `db:"feeder_id"`
	Name           string          `db:"name"`
	FromNodeID     uuid.UUID       `db:"from_node_id"`
	ToNodeID       uuid.UUID       `db:"to_node_id"`
	Linecode       string          `db:"linecode"`
	LengthM        float64         `db:"length_m"`
	IsSwitch       bool            `db:"is_switch"`
	ROhm           []float64       `db:"r_ohm"`
	XOhm           []float64       `db:"x_ohm"`
	BS             []float64       `db:"b_s"`
	AmpacityA      *float64        `db:"ampacity_a"`
	AmpacitySource *AmpacitySource `db:"ampacity_source"`
	CreatedAt      time.Time       `db:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at"`
}

// Site mirrors the sites table.
type Site struct {
	ID              uuid.UUID  `db:"id"`
	NMI             string     `db:"nmi"`
	FeederID        uuid.UUID  `db:"feeder_id"`
	NodeID          uuid.UUID  `db:"node_id"`
	Name            string     `db:"name"`
	Phase           int16      `db:"phase"`
	PVKW            float64    `db:"pv_kw"`
	InverterKVA     float64    `db:"inverter_kva"`
	ExportCapW      float64    `db:"export_cap_w"`
	ImportCapW      float64    `db:"import_cap_w"`
	HasBattery      bool       `db:"has_battery"`
	BatteryKWh      *float64   `db:"battery_kwh"`
	HasEV           bool       `db:"has_ev"`
	ProfileCustomer *int32     `db:"profile_customer"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
	DeletedAt       *time.Time `db:"deleted_at"`
}

// Device mirrors the devices table.
type Device struct {
	ID        uuid.UUID  `db:"id"`
	SiteID    uuid.UUID  `db:"site_id"`
	DERType   DERType    `db:"der_type"`
	RatedW    float64    `db:"rated_w"`
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at"`
}

// DeviceStatus mirrors the device_status table: the latest reading of a
// device.
type DeviceStatus struct {
	DeviceID   uuid.UUID `db:"device_id"`
	LastSeenAt time.Time `db:"last_seen_at"`
	PowerW     float64   `db:"power_w"`
	NetExportW float64   `db:"net_export_w"`
}

// SiteProfile mirrors the site_profiles table.
type SiteProfile struct {
	SiteID          uuid.UUID `db:"site_id"`
	TS              time.Time `db:"ts"`
	LoadW           float64   `db:"load_w"`
	PVW             float64   `db:"pv_w"`
	ControlledLoadW float64   `db:"controlled_load_w"`
}

// EnvelopeConfig mirrors the envelope_configs table. A config is immutable; a
// change is a new version.
type EnvelopeConfig struct {
	ID                  uuid.UUID      `db:"id"`
	FeederID            uuid.UUID      `db:"feeder_id"`
	Version             int32          `db:"version"`
	Policy              EnvelopePolicy `db:"policy"`
	VMinPU              float64        `db:"v_min_pu"`
	VMaxPU              float64        `db:"v_max_pu"`
	TransformerLimitPct float64        `db:"transformer_limit_pct"`
	LineLimitPct        float64        `db:"line_limit_pct"`
	PVScale             float64        `db:"pv_scale"`
	StaticLimitW        float64        `db:"static_limit_w"`
	IntervalMinutes     int32          `db:"interval_minutes"`
	HorizonIntervals    int32          `db:"horizon_intervals"`
	BreachGraceSeconds  int32          `db:"breach_grace_seconds"`
	OfflineAfterSeconds int32          `db:"offline_after_seconds"`
	Note                string         `db:"note"`
	CreatedBy           string         `db:"created_by"`
	CreatedAt           time.Time      `db:"created_at"`
}

// EnvelopeRun mirrors the envelope_runs table.
type EnvelopeRun struct {
	ID               uuid.UUID  `db:"id"`
	FeederID         uuid.UUID  `db:"feeder_id"`
	EnvelopeConfigID uuid.UUID  `db:"envelope_config_id"`
	Status           RunStatus  `db:"status"`
	IdempotencyKey   string     `db:"idempotency_key"`
	HorizonFrom      time.Time  `db:"horizon_from"`
	HorizonTo        time.Time  `db:"horizon_to"`
	StartedAt        time.Time  `db:"started_at"`
	CompletedAt      *time.Time `db:"completed_at"`
	DurationMS       *int32     `db:"duration_ms"`
	SiteCount        int32      `db:"site_count"`
	IntervalCount    int32      `db:"interval_count"`
	EnvelopeCount    int32      `db:"envelope_count"`
	EngineVersion    string     `db:"engine_version"`
	Error            *string    `db:"error"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
}

// IdempotencyKey mirrors the idempotency_keys table.
type IdempotencyKey struct {
	Scope         string     `db:"scope"`
	Key           string     `db:"key"`
	RequestHash   []byte     `db:"request_hash"`
	EnvelopeRunID *uuid.UUID `db:"envelope_run_id"`
	CreatedAt     time.Time  `db:"created_at"`
	ExpiresAt     time.Time  `db:"expires_at"`
}

// Envelope mirrors the envelopes table: the limits of one site for one
// interval. Rows are immutable; a newer envelope supersedes an older one.
type Envelope struct {
	ID                   uuid.UUID         `db:"id"`
	SiteID               uuid.UUID         `db:"site_id"`
	ValidFrom            time.Time         `db:"valid_from"`
	ValidTo              time.Time         `db:"valid_to"`
	ExportLimitW         float64           `db:"export_limit_w"`
	ImportLimitW         float64           `db:"import_limit_w"`
	Source               EnvelopeSource    `db:"source"`
	EnvelopeRunID        *uuid.UUID        `db:"envelope_run_id"`
	BackstopEventID      *uuid.UUID        `db:"backstop_event_id"`
	ExportBinding        BindingConstraint `db:"export_binding"`
	ExportBindingElement string            `db:"export_binding_element"`
	ImportBinding        BindingConstraint `db:"import_binding"`
	ImportBindingElement string            `db:"import_binding_element"`
	SupersededAt         *time.Time        `db:"superseded_at"`
	CreatedAt            time.Time         `db:"created_at"`
}

// BackstopEvent mirrors the backstop_events table.
type BackstopEvent struct {
	ID           uuid.UUID  `db:"id"`
	FeederID     uuid.UUID  `db:"feeder_id"`
	Reason       string     `db:"reason"`
	ExportLimitW float64    `db:"export_limit_w"`
	TriggeredBy  string     `db:"triggered_by"`
	TriggeredAt  time.Time  `db:"triggered_at"`
	ClearedBy    *string    `db:"cleared_by"`
	ClearedAt    *time.Time `db:"cleared_at"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

// BackstopEventSite mirrors the backstop_event_sites table.
type BackstopEventSite struct {
	BackstopEventID uuid.UUID `db:"backstop_event_id"`
	SiteID          uuid.UUID `db:"site_id"`
}

// Alert mirrors the alerts table.
type Alert struct {
	ID             uuid.UUID     `db:"id"`
	SiteID         uuid.UUID     `db:"site_id"`
	FeederID       uuid.UUID     `db:"feeder_id"`
	DeviceID       *uuid.UUID    `db:"device_id"`
	Kind           AlertKind     `db:"kind"`
	Severity       AlertSeverity `db:"severity"`
	OpenedAt       time.Time     `db:"opened_at"`
	ResolvedAt     *time.Time    `db:"resolved_at"`
	AcknowledgedAt *time.Time    `db:"acknowledged_at"`
	AcknowledgedBy *string       `db:"acknowledged_by"`
	LimitW         *float64      `db:"limit_w"`
	PeakW          *float64      `db:"peak_w"`
	Detail         string        `db:"detail"`
	CreatedAt      time.Time     `db:"created_at"`
	UpdatedAt      time.Time     `db:"updated_at"`
}

// Reading mirrors the readings table.
type Reading struct {
	DeviceID   uuid.UUID `db:"device_id"`
	SiteID     uuid.UUID `db:"site_id"`
	TS         time.Time `db:"ts"`
	PowerW     float64   `db:"power_w"`
	NetExportW float64   `db:"net_export_w"`
	SOCPct     *float64  `db:"soc_pct"`
	VoltageV   *float64  `db:"voltage_v"`
	ReceivedAt time.Time `db:"received_at"`
}

// Page is a keyset page request. Size is the largest number of rows wanted;
// Token is the opaque position after the last row of the previous page, and
// empty for the first page.
type Page struct {
	Size  int32
	Token string
}

// EnvelopeRunInterval mirrors the envelope_run_intervals table: the forecast
// state of the whole feeder for one interval of a run.
type EnvelopeRunInterval struct {
	EnvelopeRunID        uuid.UUID         `db:"envelope_run_id"`
	FeederID             uuid.UUID         `db:"feeder_id"`
	ValidFrom            time.Time         `db:"valid_from"`
	ValidTo              time.Time         `db:"valid_to"`
	ForecastNetLoadW     float64           `db:"forecast_net_load_w"`
	ForecastLoadingPct   float64           `db:"forecast_loading_pct"`
	ForecastVMinPU       float64           `db:"forecast_v_min_pu"`
	ForecastVMaxPU       float64           `db:"forecast_v_max_pu"`
	ExportLimitTotalW    float64           `db:"export_limit_total_w"`
	ImportLimitTotalW    float64           `db:"import_limit_total_w"`
	StaticLimitTotalW    float64           `db:"static_limit_total_w"`
	StaticVMaxPU         float64           `db:"static_v_max_pu"`
	StaticBinding        BindingConstraint `db:"static_binding"`
	StaticBindingElement string            `db:"static_binding_element"`
	CreatedAt            time.Time         `db:"created_at"`
}

// SitePower mirrors the site_power_1m continuous aggregate: one minute of a
// site's telemetry.
type SitePower struct {
	SiteID        uuid.UUID `db:"site_id"`
	Bucket        time.Time `db:"bucket"`
	AvgNetExportW float64   `db:"avg_net_export_w"`
	MaxNetExportW float64   `db:"max_net_export_w"`
	AvgSOCPct     *float64  `db:"avg_soc_pct"`
	AvgVoltageV   *float64  `db:"avg_voltage_v"`
	ReadingCount  int64     `db:"reading_count"`
}

// FleetMinute mirrors the fleet_1m view: one minute of a feeder's fleet.
type FleetMinute struct {
	FeederID       uuid.UUID `db:"feeder_id"`
	Bucket         time.Time `db:"bucket"`
	ExportW        float64   `db:"export_w"`
	ImportW        float64   `db:"import_w"`
	AvgSOCPct      *float64  `db:"avg_soc_pct"`
	ReportingSites int64     `db:"reporting_sites"`
	ReadingCount   int64     `db:"reading_count"`
}

// DeviceState is a device with its latest reading, when it has sent one: a
// row of devices joined to device_status.
type DeviceState struct {
	DeviceID uuid.UUID
	SiteID   uuid.UUID
	DERType  DERType
	NMI      string
	// LastSeenAt is nil for a device that has never reported.
	LastSeenAt *time.Time
	PowerW     float64
	NetExportW float64
}
