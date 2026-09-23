package pg_test

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
	"doelab/api/internal/testutil"
)

// resource ties a table to the Go struct and the proto message that mirror it.
type resource struct {
	table  string
	domain any
	// proto is nil for a table that has no message yet.
	proto protoreflect.MessageDescriptor
	// notOnWire names columns that are deliberately absent from the message,
	// each with its reason.
	notOnWire map[string]string
}

// softDelete is why deleted_at is not on the wire.
var softDelete = map[string]string{"deleted_at": "a deleted row is not returned, so the field would always be empty"}

var resources = []resource{
	{table: "feeders", domain: domain.Feeder{}, proto: (&doelabv1.Feeder{}).ProtoReflect().Descriptor()},
	{table: "feeder_nodes", domain: domain.FeederNode{}, proto: (&doelabv1.FeederNode{}).ProtoReflect().Descriptor()},
	{table: "feeder_lines", domain: domain.FeederLine{}, proto: (&doelabv1.FeederLine{}).ProtoReflect().Descriptor()},
	{table: "sites", domain: domain.Site{}, proto: (&doelabv1.Site{}).ProtoReflect().Descriptor(), notOnWire: softDelete},
	{table: "devices", domain: domain.Device{}, proto: (&doelabv1.Device{}).ProtoReflect().Descriptor(), notOnWire: softDelete},
	{table: "site_profiles", domain: domain.SiteProfile{}, proto: (&doelabv1.SiteProfile{}).ProtoReflect().Descriptor()},
	{table: "envelope_configs", domain: domain.EnvelopeConfig{}, proto: (&doelabv1.EnvelopeConfig{}).ProtoReflect().Descriptor()},
	{table: "device_status", domain: domain.DeviceStatus{}},
	{table: "envelope_runs", domain: domain.EnvelopeRun{}, proto: (&doelabv1.EnvelopeRun{}).ProtoReflect().Descriptor()},
	{table: "idempotency_keys", domain: domain.IdempotencyKey{}},
	{table: "envelopes", domain: domain.Envelope{}, proto: (&doelabv1.Envelope{}).ProtoReflect().Descriptor()},
	{table: "backstop_events", domain: domain.BackstopEvent{}},
	{table: "backstop_event_sites", domain: domain.BackstopEventSite{}},
	{table: "alerts", domain: domain.Alert{}},
	{table: "readings", domain: domain.Reading{}},
}

type column struct {
	nullable bool
	kind     string // the Postgres type, as udt_name
}

// The schema is the source of truth. For every resource, the table's columns,
// the domain struct's fields and the proto message's fields must be the same
// set, with the same nullability and compatible types. Adding a column
// without the other two edits fails here.
func TestParity(t *testing.T) {
	t.Parallel()
	pool := testutil.Postgres(t)
	ctx := context.Background()

	covered := map[string]bool{}
	for _, r := range resources {
		covered[r.table] = true
		t.Run(r.table, func(t *testing.T) {
			columns := map[string]column{}
			rows, err := pool.Query(ctx,
				`SELECT column_name::text, is_nullable = 'YES', udt_name::text
				   FROM information_schema.columns
				  WHERE table_schema = 'public' AND table_name = $1`, r.table)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var name string
				var c column
				if err := rows.Scan(&name, &c.nullable, &c.kind); err != nil {
					t.Fatal(err)
				}
				columns[name] = c
			}
			if len(columns) == 0 {
				t.Fatalf("table %s has no columns: does it exist?", r.table)
			}

			checkDomain(t, r, columns)
			if r.proto != nil {
				checkProto(t, r, columns)
			}
		})
	}

	// Every table of the schema is a resource here, apart from the audit
	// machinery of the conventions migration.
	infrastructure := map[string]bool{
		"convention_exemptions": true, "audit_redactions": true, "row_history_retention": true,
		"row_history": true, "schema_migrations": true,
	}
	rows, err := pool.Query(ctx,
		`SELECT table_name::text FROM information_schema.tables
		  WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		if !covered[table] && !infrastructure[table] {
			t.Errorf("table %s has no entry in the parity test: add its domain struct and proto message", table)
		}
	}
}

// goKinds maps a Postgres type to the Go types that may hold it.
var goKinds = map[string][]string{
	"uuid":        {"uuid.UUID"},
	"text":        {"string"},
	"bool":        {"bool"},
	"int2":        {"int16"},
	"int4":        {"int32"},
	"float8":      {"float64"},
	"_float8":     {"[]float64"},
	"bytea":       {"[]uint8"},
	"timestamptz": {"time.Time"},
}

func checkDomain(t *testing.T, r resource, columns map[string]column) {
	t.Helper()
	typ := reflect.TypeOf(r.domain)
	seen := map[string]bool{}
	for i := range typ.NumField() {
		field := typ.Field(i)
		name := field.Tag.Get("db")
		if name == "" {
			t.Errorf("domain.%s.%s has no db tag", typ.Name(), field.Name)
			continue
		}
		seen[name] = true
		col, ok := columns[name]
		if !ok {
			t.Errorf("domain.%s.%s names column %s, which %s does not have", typ.Name(), field.Name, name, r.table)
			continue
		}

		goType := field.Type
		pointer := goType.Kind() == reflect.Pointer
		if pointer {
			goType = goType.Elem()
		}
		if pointer != col.nullable {
			t.Errorf("%s.%s is nullable=%v, but domain.%s.%s is pointer=%v",
				r.table, name, col.nullable, typ.Name(), field.Name, pointer)
		}
		want, known := goKinds[col.kind]
		if !known {
			// An enum: the Go type is a named string in package domain.
			if goType.Kind() != reflect.String || goType.PkgPath() != typ.PkgPath() {
				t.Errorf("%s.%s is the enum %s, but domain.%s.%s is %s", r.table, name, col.kind, typ.Name(), field.Name, goType)
			}
			continue
		}
		if !contains(want, goType.String()) {
			t.Errorf("%s.%s is %s, but domain.%s.%s is %s", r.table, name, col.kind, typ.Name(), field.Name, goType)
		}
	}
	for name := range columns {
		if !seen[name] {
			t.Errorf("%s.%s has no field in domain.%s", r.table, name, typ.Name())
		}
	}
}

// protoKinds maps a Postgres type to the proto kind that carries it.
var protoKinds = map[string]protoreflect.Kind{
	"uuid":    protoreflect.StringKind,
	"text":    protoreflect.StringKind,
	"bool":    protoreflect.BoolKind,
	"int2":    protoreflect.Int32Kind,
	"int4":    protoreflect.Int32Kind,
	"float8":  protoreflect.DoubleKind,
	"_float8": protoreflect.DoubleKind,
	"bytea":   protoreflect.BytesKind,
}

func checkProto(t *testing.T, r resource, columns map[string]column) {
	t.Helper()
	fields := r.proto.Fields()
	seen := map[string]bool{}
	for i := range fields.Len() {
		field := fields.Get(i)
		name := string(field.Name())
		seen[name] = true
		col, ok := columns[name]
		if !ok {
			t.Errorf("%s.%s is not a column of %s", r.proto.Name(), name, r.table)
			continue
		}
		if reason, hidden := r.notOnWire[name]; hidden {
			t.Errorf("%s.%s is on the wire, but the test says it must not be (%s)", r.proto.Name(), name, reason)
		}

		switch col.kind {
		case "timestamptz":
			if field.Kind() != protoreflect.MessageKind || field.Message().FullName() != "google.protobuf.Timestamp" {
				t.Errorf("%s.%s is timestamptz, but %s.%s is %s", r.table, name, r.proto.Name(), name, field.Kind())
			}
			// A message field always has presence; nothing more to compare.
			continue
		case "_float8":
			if !field.IsList() || field.Kind() != protoreflect.DoubleKind {
				t.Errorf("%s.%s is double precision[], but %s.%s is not repeated double", r.table, name, r.proto.Name(), name)
			}
			continue
		}

		want, known := protoKinds[col.kind]
		if !known {
			want = protoreflect.EnumKind
			if field.Kind() == protoreflect.EnumKind {
				checkEnumName(t, col.kind, field.Enum())
			}
		}
		if field.Kind() != want {
			t.Errorf("%s.%s is %s, but %s.%s is %s, want %s", r.table, name, col.kind, r.proto.Name(), name, field.Kind(), want)
		}
		// A nullable column is an `optional` field, and only a nullable one.
		if field.HasOptionalKeyword() != col.nullable {
			t.Errorf("%s.%s is nullable=%v, but %s.%s is optional=%v",
				r.table, name, col.nullable, r.proto.Name(), name, field.HasOptionalKeyword())
		}
	}
	for name := range columns {
		if _, hidden := r.notOnWire[name]; !seen[name] && !hidden {
			t.Errorf("%s.%s has no field in %s", r.table, name, r.proto.Name())
		}
	}
	for name := range r.notOnWire {
		if _, ok := columns[name]; !ok {
			t.Errorf("the test hides %s.%s from the wire, but there is no such column", r.table, name)
		}
	}
}

// checkEnumName checks that a column of a Postgres enum type is carried by
// the proto enum of the same name: der_type by DerType.
func checkEnumName(t *testing.T, pgType string, enum protoreflect.EnumDescriptor) {
	t.Helper()
	if got := snake(string(enum.Name())); got != pgType {
		t.Errorf("a column of type %s is carried by the proto enum %s", pgType, enum.Name())
	}
}

// snake turns DerType into der_type.
func snake(camel string) string {
	var b strings.Builder
	for i, r := range camel {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Every Postgres enum type has a proto enum with the same values, and the
// other way round. The domain constants are tied to the proto values by
// protomap's own test.
func TestEnumParity(t *testing.T) {
	t.Parallel()
	pool := testutil.Postgres(t)

	inPostgres := map[string][]string{}
	rows, err := pool.Query(context.Background(),
		`SELECT t.typname::text, e.enumlabel::text
		   FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
		   JOIN pg_namespace n ON n.oid = t.typnamespace
		  WHERE n.nspname = 'public'
		  ORDER BY t.typname, e.enumsortorder`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var typ, label string
		if err := rows.Scan(&typ, &label); err != nil {
			t.Fatal(err)
		}
		inPostgres[typ] = append(inPostgres[typ], label)
	}
	// The audit machinery's own enums are not part of the API.
	delete(inPostgres, "convention")
	delete(inPostgres, "row_op")

	inProto := map[string][]string{}
	enums := doelabv1.File_doelab_v1_common_proto.Enums()
	for i := range enums.Len() {
		enum := enums.Get(i)
		name := snake(string(enum.Name()))
		prefix := strings.ToUpper(name) + "_"
		for j := range enum.Values().Len() {
			value := string(enum.Values().Get(j).Name())
			if !strings.HasPrefix(value, prefix) {
				t.Errorf("%s.%s does not start with %s", enum.Name(), value, prefix)
			}
			if label := strings.ToLower(strings.TrimPrefix(value, prefix)); label != "unspecified" {
				inProto[name] = append(inProto[name], label)
			}
		}
	}

	names := map[string]bool{}
	for name := range inPostgres {
		names[name] = true
	}
	for name := range inProto {
		names[name] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		pgValues, protoValues := inPostgres[name], inProto[name]
		// Same values, in the same order: the order is the proto numbering.
		if !reflect.DeepEqual(pgValues, protoValues) {
			t.Errorf("enum %s: Postgres has %v, proto has %v", name, pgValues, protoValues)
		}
	}
}
