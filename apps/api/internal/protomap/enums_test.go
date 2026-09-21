package protomap

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
)

// Every value of every proto enum, except UNSPECIFIED, maps to a domain value
// and back. A value added to a .proto file and not to the map fails here.
func TestEnumMapsAreComplete(t *testing.T) {
	t.Parallel()

	check := func(name string, descriptor protoreflect.EnumDescriptor, toProto, fromProto reflect.Value) {
		values := descriptor.Values()
		if toProto.Len() != values.Len()-1 || fromProto.Len() != values.Len()-1 {
			t.Errorf("%s: the proto enum has %d values besides UNSPECIFIED, the map has %d (and %d back)",
				name, values.Len()-1, toProto.Len(), fromProto.Len())
		}
		for i := range values.Len() {
			number := values.Get(i).Number()
			key := reflect.ValueOf(number).Convert(fromProto.Type().Key())
			mapped := fromProto.MapIndex(key)
			if number == 0 {
				if mapped.IsValid() {
					t.Errorf("%s: UNSPECIFIED maps to %v; it must map to nothing", name, mapped)
				}
				continue
			}
			if !mapped.IsValid() || mapped.String() == "" {
				t.Errorf("%s: %s has no domain value", name, values.Get(i).Name())
				continue
			}
			if back := toProto.MapIndex(mapped); !back.IsValid() || back.Int() != int64(number) {
				t.Errorf("%s: %s does not round-trip", name, values.Get(i).Name())
			}
		}
	}
	each := func(name string, descriptor protoreflect.EnumDescriptor, m any) {
		v := reflect.ValueOf(m)
		check(name, descriptor, v.FieldByName("toProto"), v.FieldByName("fromProto"))
	}

	each("AmpacitySource", doelabv1.AmpacitySource(0).Descriptor(), ampacitySources)
	each("DerType", doelabv1.DerType(0).Descriptor(), derTypes)
	each("EnvelopePolicy", doelabv1.EnvelopePolicy(0).Descriptor(), envelopePolicies)
	each("RunStatus", doelabv1.RunStatus(0).Descriptor(), runStatuses)
	each("EnvelopeSource", doelabv1.EnvelopeSource(0).Descriptor(), envelopeSources)
	each("BindingConstraint", doelabv1.BindingConstraint(0).Descriptor(), bindingConstraints)
	each("AlertKind", doelabv1.AlertKind(0).Descriptor(), alertKinds)
	each("AlertSeverity", doelabv1.AlertSeverity(0).Descriptor(), alertSeverities)
}

func TestEnumFunctions(t *testing.T) {
	t.Parallel()

	if DERTypeToProto(domain.DERBattery) != doelabv1.DerType_DER_TYPE_BATTERY || DERTypeFromProto(doelabv1.DerType_DER_TYPE_EV) != domain.DEREV {
		t.Error("DER type")
	}
	if EnvelopePolicyToProto(domain.PolicyProportional) != doelabv1.EnvelopePolicy_ENVELOPE_POLICY_PROPORTIONAL ||
		EnvelopePolicyFromProto(doelabv1.EnvelopePolicy_ENVELOPE_POLICY_EQUAL) != domain.PolicyEqual {
		t.Error("envelope policy")
	}
	if RunStatusToProto(domain.RunFailed) != doelabv1.RunStatus_RUN_STATUS_FAILED || RunStatusFromProto(doelabv1.RunStatus_RUN_STATUS_RUNNING) != domain.RunRunning {
		t.Error("run status")
	}
	if EnvelopeSourceToProto(domain.SourceBackstop) != doelabv1.EnvelopeSource_ENVELOPE_SOURCE_BACKSTOP {
		t.Error("envelope source")
	}
	if BindingConstraintToProto(domain.BindingLine) != doelabv1.BindingConstraint_BINDING_CONSTRAINT_LINE ||
		BindingConstraintFromProto(doelabv1.BindingConstraint_BINDING_CONSTRAINT_SITE_CAP) != domain.BindingSiteCap {
		t.Error("binding constraint")
	}
	if AlertKindToProto(domain.AlertDeviceOffline) != doelabv1.AlertKind_ALERT_KIND_DEVICE_OFFLINE ||
		AlertKindFromProto(doelabv1.AlertKind_ALERT_KIND_CONSTRAINT_BREACH) != domain.AlertConstraintBreach {
		t.Error("alert kind")
	}
	if AlertSeverityToProto(domain.SeverityCritical) != doelabv1.AlertSeverity_ALERT_SEVERITY_CRITICAL {
		t.Error("alert severity")
	}

	// An unspecified or unknown proto value is the empty domain value, which
	// the schema refuses; an unknown domain value is UNSPECIFIED.
	if DERTypeFromProto(doelabv1.DerType_DER_TYPE_UNSPECIFIED) != "" || DERTypeFromProto(99) != "" {
		t.Error("an unspecified DER type maps to something")
	}
	if DERTypeToProto("wind") != doelabv1.DerType_DER_TYPE_UNSPECIFIED {
		t.Error("an unknown DER type maps to something")
	}
}

func TestParseID(t *testing.T) {
	t.Parallel()
	if got := parseID("not-a-uuid"); got.String() != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("parseID of a malformed id = %s, want the nil UUID", got)
	}
}
