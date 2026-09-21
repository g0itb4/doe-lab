package domain

// The enums of the schema. Each value is spelled as the Postgres enum spells
// it.

// AmpacitySource says where a line's current rating came from.
type AmpacitySource string

// The ampacity sources.
const (
	AmpacityAssumed  AmpacitySource = "assumed"
	AmpacityOperator AmpacitySource = "operator"
)

// DERType is the kind of distributed energy resource a device is.
type DERType string

// The DER types.
const (
	DERSolar   DERType = "solar"
	DERBattery DERType = "battery"
	DEREV      DERType = "ev"
)

// EnvelopePolicy decides how capacity is shared between sites.
type EnvelopePolicy string

// The envelope policies.
const (
	PolicyEqual        EnvelopePolicy = "equal"
	PolicyProportional EnvelopePolicy = "proportional"
)

// RunStatus is the state of an engine run.
type RunStatus string

// The run statuses.
const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
)

// EnvelopeSource says what produced an envelope.
type EnvelopeSource string

// The envelope sources.
const (
	SourceEngine   EnvelopeSource = "engine"
	SourceBackstop EnvelopeSource = "backstop"
)

// BindingConstraint names what stops an envelope from being larger.
type BindingConstraint string

// The binding constraints.
const (
	BindingNone        BindingConstraint = "none"
	BindingVoltageHigh BindingConstraint = "voltage_high"
	BindingVoltageLow  BindingConstraint = "voltage_low"
	BindingTransformer BindingConstraint = "transformer"
	BindingLine        BindingConstraint = "line"
	BindingSiteCap     BindingConstraint = "site_cap"
)

// AlertKind is what an alert is about.
type AlertKind string

// The alert kinds.
const (
	AlertConstraintBreach AlertKind = "constraint_breach"
	AlertDeviceOffline    AlertKind = "device_offline"
)

// AlertSeverity ranks an alert.
type AlertSeverity string

// The alert severities.
const (
	SeverityInfo     AlertSeverity = "info"
	SeverityWarning  AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
)
