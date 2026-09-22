package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/engine"
	"doelab/api/internal/repo/mem"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

func lv10(t *testing.T) *engine.Network {
	t.Helper()
	data, err := os.ReadFile("../engine/testdata/lv10_network.json")
	if err != nil {
		t.Fatal(err)
	}
	var net engine.Network
	if err := json.Unmarshal(data, &net); err != nil {
		t.Fatal(err)
	}
	return &net
}

// homes is a dataset of 300 customers with capacities from 1.0 to 3.99 kWp.
func homes() []service.Customer {
	out := make([]service.Customer, 300)
	for i := range out {
		out[i] = service.Customer{Number: i + 1, CapacityKWp: 1 + float64(i)/100}
	}
	return out
}

var importOptions = service.ImportOptions{
	Code: "LV10", Attribution: "CSIRO, CC BY-NC-SA 4.0", TapPU: 0.975, PVScale: 3, Seed: 20261001, EnrolledFraction: 0.6,
}

func TestImportFeeder(t *testing.T) {
	t.Parallel()
	store := mem.New()
	importer := service.NewImporter(store, mem.NewObjects())
	ctx := context.Background()

	got, err := importer.ImportFeeder(ctx, lv10(t), homes(), importOptions)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Created || got.Feeder.Code != "LV10" || got.Feeder.TapPU != 0.975 || got.Feeder.TransformerKVA != 500 {
		t.Errorf("feeder = %+v, created %v", got.Feeder, got.Created)
	}

	nodes, err := store.ListFeederTree(ctx, got.Feeder.ID)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := store.ListAllFeederLines(ctx, got.Feeder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 223 || len(lines) != 222 || len(got.Sites) != 94 {
		t.Fatalf("%d nodes, %d lines, %d sites; want 223, 222, 94", len(nodes), len(lines), len(got.Sites))
	}

	// Every site: a valid synthetic NMI, a home of its own, and DER that is
	// consistent with whether it is enrolled.
	customers := map[int32]bool{}
	phases := map[int16]int{}
	enrolled, batteries, evs := 0, 0, 0
	for i, site := range got.Sites {
		wantNMI, _ := domain.SyntheticNMI(i + 1)
		if site.NMI != wantNMI || !domain.ValidNMI(site.NMI) {
			t.Errorf("site %d has NMI %s, want %s", i, site.NMI, wantNMI)
		}
		if site.ProfileCustomer == nil || customers[*site.ProfileCustomer] || *site.ProfileCustomer < 1 || *site.ProfileCustomer > 300 {
			t.Fatalf("site %s replays customer %v: missing, out of range or shared", site.NMI, site.ProfileCustomer)
		}
		customers[*site.ProfileCustomer] = true
		phases[site.Phase]++

		// 1.0 to 3.99 kWp, times three: 3 to 12 kW of PV, 3 to 10 kVA of inverter.
		if site.PVKW < 3 || site.PVKW > 12 || site.InverterKVA < 3 || site.InverterKVA > 10 {
			t.Errorf("site %s: %.1f kW of PV on a %.1f kVA inverter", site.NMI, site.PVKW, site.InverterKVA)
		}
		devices, _, err := store.ListDevices(ctx, service.DeviceFilter{SiteID: &site.ID}, domain.Page{Size: 10})
		if err != nil {
			t.Fatal(err)
		}
		if site.ExportCapW == 0 {
			// Passive: forecast, not controlled. No caps, no devices.
			if site.ImportCapW != 0 || site.HasBattery || site.HasEV || len(devices) != 0 {
				t.Errorf("passive site %s = %+v with %d devices", site.NMI, site, len(devices))
			}
			continue
		}
		enrolled++
		wantDevices := 1
		if site.HasBattery {
			batteries++
			wantDevices++
			if site.BatteryKWh == nil || (*site.BatteryKWh != 10 && *site.BatteryKWh != 13.5) {
				t.Errorf("site %s has a battery of %v kWh", site.NMI, site.BatteryKWh)
			}
		}
		if site.HasEV {
			evs++
			wantDevices++
		}
		if site.ExportCapW != site.InverterKVA*1000 || site.ImportCapW != 14000 || len(devices) != wantDevices {
			t.Errorf("enrolled site %s = %+v with %d devices, want %d", site.NMI, site, len(devices), wantDevices)
		}
	}
	if phases[1] != 32 || phases[2] != 31 || phases[3] != 31 {
		t.Errorf("sites per phase = %v", phases)
	}
	// About six in ten enrolled; some batteries and some EVs among them.
	if enrolled < 45 || enrolled > 68 || batteries < 5 || evs < 4 {
		t.Errorf("%d enrolled, %d batteries, %d EVs of 94 sites", enrolled, batteries, evs)
	}
	t.Logf("%d enrolled, %d with a battery, %d with an EV", enrolled, batteries, evs)

	config, err := store.GetActiveEnvelopeConfig(ctx, got.Feeder.ID)
	if err != nil || config.Version != 1 || config.VMaxPU != 1.10 || config.PVScale != 3 || config.CreatedBy != "import" {
		t.Errorf("config = %+v, %v", config, err)
	}

	// The same seed gives the same feeder, in a fresh store.
	again, err := service.NewImporter(mem.New(), mem.NewObjects()).ImportFeeder(ctx, lv10(t), homes(), importOptions)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got.Sites {
		a, b := got.Sites[i], again.Sites[i]
		if *a.ProfileCustomer != *b.ProfileCustomer || a.ExportCapW != b.ExportCapW || a.HasBattery != b.HasBattery || a.HasEV != b.HasEV {
			t.Fatalf("site %d differs between two imports with one seed: %+v and %+v", i, a, b)
		}
	}
	// Another seed gives another pairing.
	other := importOptions
	other.Seed = 7
	different, err := service.NewImporter(mem.New(), mem.NewObjects()).ImportFeeder(ctx, lv10(t), homes(), other)
	if err != nil {
		t.Fatal(err)
	}
	same := 0
	for i := range got.Sites {
		if *got.Sites[i].ProfileCustomer == *different.Sites[i].ProfileCustomer {
			same++
		}
	}
	if same > 10 {
		t.Errorf("%d of 94 sites replay the same home under another seed", same)
	}
}

func TestImportFeederIsIdempotent(t *testing.T) {
	t.Parallel()
	store := mem.New()
	importer := service.NewImporter(store, mem.NewObjects())
	ctx := context.Background()

	first, err := importer.ImportFeeder(ctx, lv10(t), homes(), importOptions)
	if err != nil {
		t.Fatal(err)
	}
	second, err := importer.ImportFeeder(ctx, lv10(t), homes(), importOptions)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.Feeder.ID != first.Feeder.ID || len(second.Sites) != 94 {
		t.Errorf("second import: created %v, %d sites", second.Created, len(second.Sites))
	}
	nodes, _ := store.ListFeederTree(ctx, first.Feeder.ID)
	configs, _, _ := store.ListEnvelopeConfigs(ctx, first.Feeder.ID, domain.Page{Size: 10})
	if len(nodes) != 223 || len(configs) != 1 {
		t.Errorf("after two imports: %d nodes, %d config versions", len(nodes), len(configs))
	}
}

func TestImportFeederErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, err := service.NewImporter(mem.New(), mem.NewObjects()).ImportFeeder(ctx, lv10(t), homes()[:10], importOptions)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "94 sites need 94 homes") {
		t.Errorf("too few homes: %v", err)
	}

	bad := lv10(t)
	bad.Buses = nil
	_, err = service.NewImporter(mem.New(), mem.NewObjects()).ImportFeeder(ctx, bad, homes(), importOptions)
	if !errors.Is(err, engine.ErrInvalidNetwork) {
		t.Errorf("an invalid network: %v", err)
	}

	// A tap outside what the schema allows: the store refuses it, and the
	// transaction leaves nothing behind.
	refusing := &failingStore{Store: mem.New(), failOn: "CreateFeederLine"}
	_, err = service.NewImporter(refusing, mem.NewObjects()).ImportFeeder(ctx, lv10(t), homes(), importOptions)
	if err == nil || !strings.Contains(err.Error(), "line ") {
		t.Errorf("a failing line: %v", err)
	}
	if _, err := refusing.GetFeederByCode(ctx, "LV10"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a failed import left the feeder behind: %v", err)
	}
	for _, step := range []string{"CreateFeeder", "CreateFeederNode", "CreateSite", "CreateDevice", "CreateEnvelopeConfig", "GetFeederByCode"} {
		s := &failingStore{Store: mem.New(), failOn: step}
		if _, err := service.NewImporter(s, mem.NewObjects()).ImportFeeder(ctx, lv10(t), homes(), importOptions); !errors.Is(err, errInjected) {
			t.Errorf("a failing %s: %v", step, err)
		}
	}
}

var errInjected = errors.New("injected failure")

// failingStore fails one named repository call.
type failingStore struct {
	*mem.Store
	failOn string
}

func (s *failingStore) GetFeederByCode(ctx context.Context, code string) (domain.Feeder, error) {
	if s.failOn == "GetFeederByCode" {
		return domain.Feeder{}, errInjected
	}
	return s.Store.GetFeederByCode(ctx, code)
}

func (s *failingStore) Tx(ctx context.Context, fn func(context.Context, service.Repos) error) error {
	return s.Store.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		return fn(ctx, failingRepos{Repos: r, failOn: s.failOn})
	})
}

type failingRepos struct {
	service.Repos
	failOn string
}

func (r failingRepos) CreateFeeder(ctx context.Context, f domain.Feeder) (domain.Feeder, error) {
	if r.failOn == "CreateFeeder" {
		return domain.Feeder{}, errInjected
	}
	return r.Repos.CreateFeeder(ctx, f)
}

func (r failingRepos) CreateFeederNode(ctx context.Context, n domain.FeederNode) (domain.FeederNode, error) {
	if r.failOn == "CreateFeederNode" {
		return domain.FeederNode{}, errInjected
	}
	return r.Repos.CreateFeederNode(ctx, n)
}

func (r failingRepos) CreateFeederLine(ctx context.Context, l domain.FeederLine) (domain.FeederLine, error) {
	if r.failOn == "CreateFeederLine" {
		return domain.FeederLine{}, errInjected
	}
	return r.Repos.CreateFeederLine(ctx, l)
}

func (r failingRepos) CreateSite(ctx context.Context, s domain.Site) (domain.Site, error) {
	if r.failOn == "CreateSite" {
		return domain.Site{}, errInjected
	}
	return r.Repos.CreateSite(ctx, s)
}

func (r failingRepos) CreateDevice(ctx context.Context, d domain.Device) (domain.Device, error) {
	if r.failOn == "CreateDevice" {
		return domain.Device{}, errInjected
	}
	return r.Repos.CreateDevice(ctx, d)
}

func (r failingRepos) CreateEnvelopeConfig(ctx context.Context, c domain.EnvelopeConfig) (domain.EnvelopeConfig, error) {
	if r.failOn == "CreateEnvelopeConfig" {
		return domain.EnvelopeConfig{}, errInjected
	}
	return r.Repos.CreateEnvelopeConfig(ctx, c)
}

func TestImportProfiles(t *testing.T) {
	t.Parallel()
	store := mem.New()
	objects := mem.NewObjects()
	importer := service.NewImporter(store, objects)
	ctx := context.Background()
	f := repotest.Seed(t, store, "LV10", 1)
	sites := []domain.Site{f.SiteA, f.SiteB} // replaying customers 1 and 2

	series := map[int][]domain.SiteProfile{1: repotest.Profiles(48, 100), 2: repotest.Profiles(48, 500)}
	// Twice: the second run replaces the first.
	for range 2 {
		if err := importer.ImportProfiles(ctx, sites, series); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := store.ListSiteProfiles(ctx, f.SiteB.ID, repotest.Day, repotest.Day.AddDate(0, 0, 1), domain.Page{Size: 100})
	if err != nil || len(rows) != 48 || rows[0].LoadW != 500 || rows[0].SiteID != f.SiteB.ID {
		t.Errorf("site B has %d rows, first %+v, %v", len(rows), rows, err)
	}

	if err := importer.ImportProfiles(ctx, sites, map[int][]domain.SiteProfile{1: repotest.Profiles(1, 1)}); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "no series for customer 2") {
		t.Errorf("a missing series: %v", err)
	}
	noHome := f.SiteA
	noHome.ProfileCustomer = nil
	if err := importer.ImportProfiles(ctx, []domain.Site{noHome}, series); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a site with no home: %v", err)
	}
	ghost := f.SiteA
	ghost.ID = uuid.New()
	if err := importer.ImportProfiles(ctx, []domain.Site{ghost}, series); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("a site that is not stored: %v", err)
	}

	if err := importer.StoreRaw(ctx, "csiro/LV10/Master.dss", "text/plain", []byte("clear")); err != nil {
		t.Fatal(err)
	}
	if obj, ok := objects.Stat("raw/csiro/LV10/Master.dss"); !ok || string(obj.Body) != "clear" || obj.ContentType != "text/plain" {
		t.Errorf("stored raw file = %+v, %v", obj, ok)
	}
}
