// Package protomap converts between domain types and their proto messages.
// It is the only place where the two meet: a controller calls it, a service
// never sees a proto message.
//
// Each resource message mirrors a table, so each function here is one field
// after another. When a column is added, this is the third edit, after the
// migration and the .proto file; the parity test fails until it is made.
package protomap

import (
	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
)

// enumMap holds both directions of one enum.
type enumMap[D ~string, P comparable] struct {
	toProto   map[D]P
	fromProto map[P]D
}

func newEnumMap[D ~string, P comparable](pairs map[D]P) enumMap[D, P] {
	m := enumMap[D, P]{toProto: pairs, fromProto: make(map[P]D, len(pairs))}
	for d, p := range pairs {
		m.fromProto[p] = d
	}
	return m
}

// The enums. A new value must be added to the Postgres type, the domain
// constants, the .proto file and the map here; enums_test.go fails when one
// of them is missed.
var (
	ampacitySources = newEnumMap(map[domain.AmpacitySource]doelabv1.AmpacitySource{
		domain.AmpacityAssumed:  doelabv1.AmpacitySource_AMPACITY_SOURCE_ASSUMED,
		domain.AmpacityOperator: doelabv1.AmpacitySource_AMPACITY_SOURCE_OPERATOR,
	})
	derTypes = newEnumMap(map[domain.DERType]doelabv1.DerType{
		domain.DERSolar:   doelabv1.DerType_DER_TYPE_SOLAR,
		domain.DERBattery: doelabv1.DerType_DER_TYPE_BATTERY,
		domain.DEREV:      doelabv1.DerType_DER_TYPE_EV,
	})
	envelopePolicies = newEnumMap(map[domain.EnvelopePolicy]doelabv1.EnvelopePolicy{
		domain.PolicyEqual:        doelabv1.EnvelopePolicy_ENVELOPE_POLICY_EQUAL,
		domain.PolicyProportional: doelabv1.EnvelopePolicy_ENVELOPE_POLICY_PROPORTIONAL,
	})
	runStatuses = newEnumMap(map[domain.RunStatus]doelabv1.RunStatus{
		domain.RunRunning:   doelabv1.RunStatus_RUN_STATUS_RUNNING,
		domain.RunCompleted: doelabv1.RunStatus_RUN_STATUS_COMPLETED,
		domain.RunFailed:    doelabv1.RunStatus_RUN_STATUS_FAILED,
	})
	envelopeSources = newEnumMap(map[domain.EnvelopeSource]doelabv1.EnvelopeSource{
		domain.SourceEngine:   doelabv1.EnvelopeSource_ENVELOPE_SOURCE_ENGINE,
		domain.SourceBackstop: doelabv1.EnvelopeSource_ENVELOPE_SOURCE_BACKSTOP,
	})
	bindingConstraints = newEnumMap(map[domain.BindingConstraint]doelabv1.BindingConstraint{
		domain.BindingNone:        doelabv1.BindingConstraint_BINDING_CONSTRAINT_NONE,
		domain.BindingVoltageHigh: doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_HIGH,
		domain.BindingVoltageLow:  doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_LOW,
		domain.BindingTransformer: doelabv1.BindingConstraint_BINDING_CONSTRAINT_TRANSFORMER,
		domain.BindingLine:        doelabv1.BindingConstraint_BINDING_CONSTRAINT_LINE,
		domain.BindingSiteCap:     doelabv1.BindingConstraint_BINDING_CONSTRAINT_SITE_CAP,
	})
	alertKinds = newEnumMap(map[domain.AlertKind]doelabv1.AlertKind{
		domain.AlertConstraintBreach: doelabv1.AlertKind_ALERT_KIND_CONSTRAINT_BREACH,
		domain.AlertDeviceOffline:    doelabv1.AlertKind_ALERT_KIND_DEVICE_OFFLINE,
	})
	alertSeverities = newEnumMap(map[domain.AlertSeverity]doelabv1.AlertSeverity{
		domain.SeverityInfo:     doelabv1.AlertSeverity_ALERT_SEVERITY_INFO,
		domain.SeverityWarning:  doelabv1.AlertSeverity_ALERT_SEVERITY_WARNING,
		domain.SeverityCritical: doelabv1.AlertSeverity_ALERT_SEVERITY_CRITICAL,
	})
)

// DERTypeToProto maps a DER type to its proto value.
func DERTypeToProto(d domain.DERType) doelabv1.DerType { return derTypes.toProto[d] }

// DERTypeFromProto maps a proto DER type to the domain. An unspecified or
// unknown value maps to the empty string, which the schema refuses.
func DERTypeFromProto(p doelabv1.DerType) domain.DERType { return derTypes.fromProto[p] }

// EnvelopePolicyToProto maps a policy to its proto value.
func EnvelopePolicyToProto(d domain.EnvelopePolicy) doelabv1.EnvelopePolicy {
	return envelopePolicies.toProto[d]
}

// EnvelopePolicyFromProto maps a proto policy to the domain.
func EnvelopePolicyFromProto(p doelabv1.EnvelopePolicy) domain.EnvelopePolicy {
	return envelopePolicies.fromProto[p]
}

// RunStatusToProto maps a run status to its proto value.
func RunStatusToProto(d domain.RunStatus) doelabv1.RunStatus { return runStatuses.toProto[d] }

// RunStatusFromProto maps a proto run status to the domain.
func RunStatusFromProto(p doelabv1.RunStatus) domain.RunStatus { return runStatuses.fromProto[p] }

// EnvelopeSourceToProto maps an envelope source to its proto value.
func EnvelopeSourceToProto(d domain.EnvelopeSource) doelabv1.EnvelopeSource {
	return envelopeSources.toProto[d]
}

// BindingConstraintToProto maps a binding constraint to its proto value.
func BindingConstraintToProto(d domain.BindingConstraint) doelabv1.BindingConstraint {
	return bindingConstraints.toProto[d]
}

// BindingConstraintFromProto maps a proto binding constraint to the domain.
func BindingConstraintFromProto(p doelabv1.BindingConstraint) domain.BindingConstraint {
	return bindingConstraints.fromProto[p]
}

// AlertKindToProto maps an alert kind to its proto value.
func AlertKindToProto(d domain.AlertKind) doelabv1.AlertKind { return alertKinds.toProto[d] }

// AlertKindFromProto maps a proto alert kind to the domain.
func AlertKindFromProto(p doelabv1.AlertKind) domain.AlertKind { return alertKinds.fromProto[p] }

// AlertSeverityToProto maps an alert severity to its proto value.
func AlertSeverityToProto(d domain.AlertSeverity) doelabv1.AlertSeverity {
	return alertSeverities.toProto[d]
}
