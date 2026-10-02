// Package repotest is the conformance suite of the service ports. It runs
// against every implementation of service.Store: the in-memory one in the
// unit tier, and the Postgres one in the container tier. A rule that the
// services rely on is written here once and holds for both.
package repotest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
	"doelab/api/internal/service"
)

// Ctx is a context that carries the operator, as an authenticated write does.
func Ctx() context.Context {
	return auth.NewContext(context.Background(), auth.Actor{Scope: auth.ScopeOperator})
}

// Fixture is a small feeder: a root, a main and two customer nodes, with a
// site on each.
//
//	root ── mid ─┬─ house-a   (site A, phase 1, with DER)
//	             └─ house-b   (site B, phase 2, passive)
type Fixture struct {
	Feeder domain.Feeder
	Root   domain.FeederNode
	Mid    domain.FeederNode
	HouseA domain.FeederNode
	HouseB domain.FeederNode
	Lines  []domain.FeederLine
	SiteA  domain.Site
	SiteB  domain.Site
}

// Matrix returns a 4x4 row-major matrix with v on the diagonal.
func Matrix(v float64) []float64 {
	m := make([]float64, 16)
	for i := range 4 {
		m[i*4+i] = v
	}
	return m
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T {
	return &v
}

// NMI returns the synthetic NMI with the given serial.
func NMI(t testing.TB, serial int) string {
	t.Helper()
	nmi, err := domain.SyntheticNMI(serial)
	if err != nil {
		t.Fatal(err)
	}
	return nmi
}

// NewSubstation returns a substation ready to create, with the given code, at
// Lidcombe.
func NewSubstation(code string) domain.Substation {
	return domain.Substation{
		Code: code, Name: "Substation " + code, DNSP: "Ausgrid", State: "NSW",
		LatitudeDeg: -33.8524, LongitudeDeg: 151.0621,
	}
}

// NewFeeder returns a feeder ready to create, with the given code.
func NewFeeder(code string) domain.Feeder {
	return domain.Feeder{
		Code: code, Name: "Feeder " + code,
		NominalVoltageV: 230, TransformerKVA: 100,
		SourceVoltageV: 240, SourceAngleDeg: 0, SourceROhm: 0.02, SourceXOhm: 0.01,
		TapPU: 1, Timezone: "Australia/Sydney", Attribution: "test fixture",
	}
}

// Seed creates the fixture feeder under the given code. serial is the first
// of the two NMI serials it uses, so two fixtures in one store do not clash.
func Seed(t testing.TB, store service.Store, code string, serial int) Fixture {
	t.Helper()
	ctx := Ctx()
	var f Fixture
	err := store.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		var err error
		if f.Feeder, err = r.CreateFeeder(ctx, NewFeeder(code)); err != nil {
			return fmt.Errorf("feeder: %w", err)
		}
		node := func(name string, parent *uuid.UUID, ground *float64) (domain.FeederNode, error) {
			n := domain.FeederNode{FeederID: f.Feeder.ID, Name: name, ParentNodeID: parent}
			if ground != nil {
				n.GroundROhm, n.GroundXOhm = ground, Ptr(0.0)
			}
			return r.CreateFeederNode(ctx, n)
		}
		if f.Root, err = node("root", nil, Ptr(0.6)); err != nil {
			return fmt.Errorf("root: %w", err)
		}
		if f.Mid, err = node("mid", &f.Root.ID, nil); err != nil {
			return fmt.Errorf("mid: %w", err)
		}
		if f.HouseA, err = node("house-a", &f.Mid.ID, Ptr(10.0)); err != nil {
			return fmt.Errorf("house-a: %w", err)
		}
		if f.HouseB, err = node("house-b", &f.Mid.ID, nil); err != nil {
			return fmt.Errorf("house-b: %w", err)
		}

		line := func(name string, from, to domain.FeederNode, r1 float64, ampacity *float64) error {
			l := domain.FeederLine{
				FeederID: f.Feeder.ID, Name: name, FromNodeID: from.ID, ToNodeID: to.ID,
				Linecode: "test", LengthM: 50, ROhm: Matrix(r1), XOhm: Matrix(r1 / 2), BS: Matrix(0),
				AmpacityA: ampacity,
			}
			if ampacity != nil {
				l.AmpacitySource = Ptr(domain.AmpacityAssumed)
			}
			created, err := r.CreateFeederLine(ctx, l)
			f.Lines = append(f.Lines, created)
			return err
		}
		if err := line("main", f.Root, f.Mid, 0.04, Ptr(300.0)); err != nil {
			return fmt.Errorf("main: %w", err)
		}
		if err := line("service-a", f.Mid, f.HouseA, 0.15, Ptr(90.0)); err != nil {
			return fmt.Errorf("service-a: %w", err)
		}
		if err := line("service-b", f.Mid, f.HouseB, 0.15, nil); err != nil {
			return fmt.Errorf("service-b: %w", err)
		}

		if f.SiteA, err = r.CreateSite(ctx, domain.Site{
			NMI: NMI(t, serial), FeederID: f.Feeder.ID, NodeID: f.HouseA.ID, Name: "Ld1_LOAD_A", Phase: 1,
			PVKW: 5, InverterKVA: 5, ExportCapW: 5000, ImportCapW: 7000,
			HasBattery: true, BatteryKWh: Ptr(10.0), ProfileCustomer: Ptr(int32(1)),
		}); err != nil {
			return fmt.Errorf("site A: %w", err)
		}
		if f.SiteB, err = r.CreateSite(ctx, domain.Site{
			NMI: NMI(t, serial+1), FeederID: f.Feeder.ID, NodeID: f.HouseB.ID, Name: "Ld2_LOAD_B", Phase: 2,
			ProfileCustomer: Ptr(int32(2)),
		}); err != nil {
			return fmt.Errorf("site B: %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed %s: %v", code, err)
	}
	return f
}

// Config returns an envelope config ready to create for a feeder.
func Config(feederID uuid.UUID) domain.EnvelopeConfig {
	return domain.EnvelopeConfig{
		FeederID: feederID, Policy: domain.PolicyEqual,
		VMinPU: 0.94, VMaxPU: 1.10, TransformerLimitPct: 100, LineLimitPct: 100,
		PVScale: 1, StaticLimitW: 5000, IntervalMinutes: 30, HorizonIntervals: 48,
		BreachGraceSeconds: 60, OfflineAfterSeconds: 300, Note: "test", CreatedBy: "operator",
	}
}

// Day is the first instant of the profile day the tests use.
var Day = time.Date(2012, 10, 1, 0, 0, 0, 0, time.UTC)

// Profiles returns n half-hourly rows from Day, with load rising by 10 W a
// row from base.
func Profiles(n int, base float64) []domain.SiteProfile {
	rows := make([]domain.SiteProfile, n)
	for i := range rows {
		rows[i] = domain.SiteProfile{
			TS: Day.Add(time.Duration(i) * 30 * time.Minute), LoadW: base + 10*float64(i), PVW: float64(i),
		}
	}
	return rows
}
