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
	return network(t, "lv10")
}

// network reads one of the engine's network fixtures.
func network(t *testing.T, prefix string) *engine.Network {
	t.Helper()
	data, err := os.ReadFile("../engine/testdata/" + prefix + "_network.json")
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

func (s *failingStore) GetSubstationByCode(ctx context.Context, code string) (domain.Substation, error) {
	if s.failOn == "GetSubstationByCode" {
		return domain.Substation{}, errInjected
	}
	return s.Store.GetSubstationByCode(ctx, code)
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

func (r failingRepos) CreateSubstation(ctx context.Context, sub domain.Substation) (domain.Substation, error) {
	if r.failOn == "CreateSubstation" {
		return domain.Substation{}, errInjected
	}
	return r.Repos.CreateSubstation(ctx, sub)
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
		if err := importer.ImportProfiles(ctx, sites, series, nil); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := store.ListSiteProfiles(ctx, f.SiteB.ID, repotest.Day, repotest.Day.AddDate(0, 0, 1), domain.Page{Size: 100})
	if err != nil || len(rows) != 48 || rows[0].LoadW != 500 || rows[0].SiteID != f.SiteB.ID {
		t.Errorf("site B has %d rows, first %+v, %v", len(rows), rows, err)
	}

	if err := importer.ImportProfiles(ctx, sites, map[int][]domain.SiteProfile{1: repotest.Profiles(1, 1)}, nil); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "no series for customer 2") {
		t.Errorf("a missing series: %v", err)
	}
	noHome := f.SiteA
	noHome.ProfileCustomer = nil
	if err := importer.ImportProfiles(ctx, []domain.Site{noHome}, series, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a site with no home: %v", err)
	}
	ghost := f.SiteA
	ghost.ID = uuid.New()
	if err := importer.ImportProfiles(ctx, []domain.Site{ghost}, series, nil); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("a site that is not stored: %v", err)
	}

	// A factor scales the PV of its site, and of no other; the series itself
	// is left as it was.
	if err := importer.ImportProfiles(ctx, sites, series, map[uuid.UUID]float64{f.SiteA.ID: 2.5}); err != nil {
		t.Fatal(err)
	}
	scaled, _, err := store.ListSiteProfiles(ctx, f.SiteA.ID, repotest.Day, repotest.Day.AddDate(0, 0, 1), domain.Page{Size: 100})
	if err != nil || len(scaled) != 48 || scaled[4].PVW != 10 || scaled[4].LoadW != 140 {
		t.Errorf("site A scaled: %d rows, fifth %+v, %v", len(scaled), scaled, err)
	}
	plain, _, err := store.ListSiteProfiles(ctx, f.SiteB.ID, repotest.Day, repotest.Day.AddDate(0, 0, 1), domain.Page{Size: 100})
	if err != nil || plain[4].PVW != 4 || series[1][4].PVW != 4 {
		t.Errorf("site B's fifth row has PV %v and the series %v, want 4 and 4 (%v)", plain[4].PVW, series[1][4].PVW, err)
	}

	if err := importer.StoreRaw(ctx, "csiro/LV10/Master.dss", "text/plain", []byte("clear")); err != nil {
		t.Fatal(err)
	}
	if obj, ok := objects.Stat("raw/csiro/LV10/Master.dss"); !ok || string(obj.Body) != "clear" || obj.ContentType != "text/plain" {
		t.Errorf("stored raw file = %+v, %v", obj, ok)
	}
}

// demoFleet is two substations and three small feeders, with a seeded site of
// each kind.
func demoFleet(t *testing.T) service.Fleet {
	t.Helper()
	at := func(nmi10 string, kind [3]bool, kW float64) service.SeededSite {
		return service.SeededSite{
			NMI:   nmi10 + string(rune('0'+domain.NMIChecksum(nmi10))),
			Solar: kind[0], Battery: kind[1], EV: kind[2], CapacityKW: kW,
			LatitudeDeg: -33.85, LongitudeDeg: 151.06,
		}
	}
	solar, battery, ev, hybrid := [3]bool{true}, [3]bool{false, true}, [3]bool{false, false, true}, [3]bool{true, true}
	lidcombe, footscray := repotest.NewSubstation("SUB-001"), repotest.NewSubstation("SUB-007")
	footscray.State, footscray.LatitudeDeg, footscray.LongitudeDeg = "VIC", -37.8048, 144.9011
	return service.Fleet{
		Substations: []domain.Substation{lidcombe, footscray},
		Feeders: []service.FleetFeeder{
			{Code: "SUB-001-LV1", Substation: "SUB-001", Timezone: "Australia/Sydney", Network: network(t, "lv2"), Sites: []service.SeededSite{
				at("NMI0000001", battery, 5.4), at("NMI0000002", ev, 17.2), at("NMI0000003", solar, 3.3), at("NMI0000007", hybrid, 11.1),
			}},
			{Code: "SUB-007-LV1", Substation: "SUB-007", Timezone: "Australia/Melbourne", PVScale: 1, Network: network(t, "lv13"), Sites: []service.SeededSite{
				at("NMI0000070", solar, 8.7),
			}},
			{Code: "SUB-007-LV2", Substation: "SUB-007", Timezone: "Australia/Melbourne", Network: network(t, "lv2")},
		},
	}
}

var fleetOptions = service.ImportOptions{Attribution: "CSIRO, CC BY-NC-SA 4.0", TapPU: 0.975, PVScale: 3, Seed: 20261001}

func TestImportFleet(t *testing.T) {
	t.Parallel()
	store := mem.New()
	importer := service.NewImporter(store, mem.NewObjects())
	ctx := context.Background()
	fleet := demoFleet(t)

	got, err := importer.ImportFleet(ctx, fleet, homes(), fleetOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d feeders, want 3", len(got))
	}
	lidcombe, err := store.GetSubstationByCode(ctx, "SUB-001")
	if err != nil {
		t.Fatal(err)
	}
	footscray, err := store.GetSubstationByCode(ctx, "SUB-007")
	if err != nil || footscray.State != "VIC" || footscray.LatitudeDeg != -37.8048 {
		t.Fatalf("Footscray = %+v, %v", footscray, err)
	}

	// Each feeder hangs from its substation, in its own zone, with a config.
	first, second, third := got[0], got[1], got[2]
	if !first.Created || first.Feeder.Code != "SUB-001-LV1" || first.Feeder.SubstationID == nil ||
		*first.Feeder.SubstationID != lidcombe.ID || first.Feeder.Timezone != "Australia/Sydney" || first.Feeder.TapPU != 0.975 {
		t.Errorf("first feeder = %+v", first.Feeder)
	}
	if *second.Feeder.SubstationID != footscray.ID || second.Feeder.Timezone != "Australia/Melbourne" || *third.Feeder.SubstationID != footscray.ID {
		t.Errorf("second and third feeders = %+v, %+v", second.Feeder, third.Feeder)
	}
	// A feeder's own PV scale is its config's; with none, the import's.
	for i, want := range []float64{3, 1, 3} {
		config, err := store.GetActiveEnvelopeConfig(ctx, got[i].Feeder.ID)
		if err != nil || config.PVScale != want {
			t.Errorf("feeder %d has a config with PV scale %v, %v; want %v", i, config.PVScale, err, want)
		}
	}
	if len(first.Sites) != 7 || len(second.Sites) != 11 || len(third.Sites) != 7 {
		t.Fatalf("%d, %d and %d sites; want 7, 11 and 7", len(first.Sites), len(second.Sites), len(third.Sites))
	}

	// No NMI is made up twice, and no home is shared within a feeder.
	nmis := map[string]bool{}
	for _, feeder := range got {
		homes := map[int32]bool{}
		for _, site := range feeder.Sites {
			if nmis[site.NMI] || !domain.ValidNMI(site.NMI) || homes[*site.ProfileCustomer] {
				t.Errorf("site %s of %s: a repeated or invalid NMI, or a shared home", site.NMI, feeder.Feeder.Code)
			}
			nmis[site.NMI], homes[*site.ProfileCustomer] = true, true
		}
	}

	// The seeded sites, by the first ten characters of their NMI.
	seeded := map[string]domain.Site{}
	devices := map[string]map[domain.DERType]float64{}
	passive := 0
	for _, feeder := range got {
		for _, site := range feeder.Sites {
			found, _, err := store.ListDevices(ctx, service.DeviceFilter{SiteID: &site.ID}, domain.Page{Size: 10})
			if err != nil {
				t.Fatal(err)
			}
			if site.LatitudeDeg == nil {
				// Passive: a home of the dataset with its own PV, no caps, no
				// devices and no place.
				passive++
				if site.ExportCapW != 0 || site.ImportCapW != 0 || site.HasBattery || site.HasEV || len(found) != 0 ||
					site.LongitudeDeg != nil || site.PVKW < 1 || site.NMI[:5] != domain.SyntheticNMIPrefix {
					t.Errorf("passive site %s = %+v with %d devices", site.NMI, site, len(found))
				}
				if _, scaled := feeder.PVFactor[site.ID]; scaled {
					t.Errorf("passive site %s has a PV factor", site.NMI)
				}
				continue
			}
			seeded[site.NMI[:10]] = site
			devices[site.NMI[:10]] = map[domain.DERType]float64{}
			for _, d := range found {
				devices[site.NMI[:10]][d.DERType] = d.RatedW
			}
		}
	}
	if len(seeded) != 5 || passive != 20 {
		t.Fatalf("%d seeded and %d passive sites, want 5 and 20", len(seeded), passive)
	}

	battery := seeded["NMI0000001"]
	if battery.PVKW != 0 || battery.InverterKVA != 5.4 || battery.ExportCapW != 5400 || battery.ImportCapW != 14000 ||
		!battery.HasBattery || *battery.BatteryKWh != 10.8 || battery.HasEV ||
		len(devices["NMI0000001"]) != 1 || devices["NMI0000001"][domain.DERBattery] != 5400 {
		t.Errorf("the battery site = %+v with %v", battery, devices["NMI0000001"])
	}
	charger := seeded["NMI0000002"]
	if charger.PVKW != 0 || charger.InverterKVA != 0 || charger.ExportCapW != 0 || charger.ImportCapW != 17200 ||
		charger.HasBattery || !charger.HasEV || len(devices["NMI0000002"]) != 1 || devices["NMI0000002"][domain.DEREV] != 17200 {
		t.Errorf("the charger site = %+v with %v", charger, devices["NMI0000002"])
	}
	solar := seeded["NMI0000003"]
	if solar.PVKW != 3.3 || solar.InverterKVA != 3.3 || solar.ExportCapW != 3300 || solar.ImportCapW != 14000 ||
		solar.HasBattery || solar.HasEV || len(devices["NMI0000003"]) != 1 || devices["NMI0000003"][domain.DERSolar] != 3300 ||
		*solar.LatitudeDeg != -33.85 || *solar.LongitudeDeg != 151.06 {
		t.Errorf("the solar site = %+v with %v", solar, devices["NMI0000003"])
	}
	hybrid := seeded["NMI0000007"]
	if hybrid.PVKW != 11.1 || hybrid.ExportCapW != 11100 || !hybrid.HasBattery || *hybrid.BatteryKWh != 22.2 ||
		len(devices["NMI0000007"]) != 2 || devices["NMI0000007"][domain.DERSolar] != 11100 || devices["NMI0000007"][domain.DERBattery] != 11100 {
		t.Errorf("the hybrid site = %+v with %v", hybrid, devices["NMI0000007"])
	}

	// A seeded site's PV is the fleet's, so the home it replays is scaled to
	// it: its capacity over the home's, after the config's scale. A site with
	// no PV replays none.
	capacity := map[int32]float64{}
	for _, home := range homes() {
		capacity[int32(home.Number)] = home.CapacityKWp
	}
	if want := 3.3 / (capacity[*solar.ProfileCustomer] * 3); first.PVFactor[solar.ID] != want {
		t.Errorf("the solar site's PV factor = %v, want %v", first.PVFactor[solar.ID], want)
	}
	if factor, ok := first.PVFactor[charger.ID]; !ok || factor != 0 {
		t.Errorf("the charger site's PV factor = %v, %v; want 0", factor, ok)
	}
	if len(first.PVFactor) != 4 || len(second.PVFactor) != 1 || len(third.PVFactor) != 0 {
		t.Errorf("%d, %d and %d PV factors; want 4, 1 and 0", len(first.PVFactor), len(second.PVFactor), len(third.PVFactor))
	}

	// The fleet reaches the far end: the last seeded site of a feeder is on
	// the customer with the most cable between it and the transformer.
	net := fleet.Feeders[0].Network
	distance := make([]float64, len(net.Buses))
	farthest := 0.0
	for i, bus := range net.Buses {
		if bus.Line != nil {
			distance[i] = distance[bus.Parent] + bus.Line.LengthKm
		}
	}
	byName := map[string]float64{}
	for _, site := range net.Sites {
		byName[site.Name] = distance[site.Bus]
		farthest = max(farthest, distance[site.Bus])
	}
	if byName[hybrid.Name] != farthest || byName[battery.Name] >= byName[hybrid.Name] {
		t.Errorf("the seeded sites are %.3f and %.3f km out, and the farthest customer %.3f km", byName[battery.Name], byName[hybrid.Name], farthest)
	}

	// Again: nothing is written, and the factors are the same.
	again, err := importer.ImportFleet(ctx, fleet, homes(), fleetOptions)
	if err != nil {
		t.Fatal(err)
	}
	substations, _, err := store.ListSubstations(ctx, domain.Page{Size: 10})
	if err != nil || len(substations) != 2 {
		t.Errorf("%d substations after two imports, %v", len(substations), err)
	}
	if again[0].Created || again[0].Feeder.ID != first.Feeder.ID || len(again[0].Sites) != 7 ||
		again[0].PVFactor[solar.ID] != first.PVFactor[solar.ID] || len(again[0].PVFactor) != 4 {
		t.Errorf("the second import of the first feeder: created %v, %d sites, factors %v", again[0].Created, len(again[0].Sites), again[0].PVFactor)
	}
}

func TestImportFleetErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	importFleet := func(store service.Store, fleet service.Fleet, customers []service.Customer) error {
		_, err := service.NewImporter(store, mem.NewObjects()).ImportFleet(ctx, fleet, customers, fleetOptions)
		return err
	}

	lost := demoFleet(t)
	lost.Feeders[1].Substation = "SUB-404"
	if err := importFleet(mem.New(), lost, homes()); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "SUB-404, which is not a substation of the fleet") {
		t.Errorf("a feeder below an unknown substation: %v", err)
	}

	crowded := demoFleet(t)
	crowded.Feeders[0].Sites = append(crowded.Feeders[0].Sites, crowded.Feeders[0].Sites...)
	if err := importFleet(mem.New(), crowded, homes()); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "given 8 seeded sites and has 7 customers") {
		t.Errorf("more seeded sites than customers: %v", err)
	}

	// A seeded site with a home the dataset gives no capacity: its PV cannot
	// be scaled from nothing, so it replays none.
	dark := homes()
	for i := range dark {
		dark[i].CapacityKWp = 0
	}
	got, err := service.NewImporter(mem.New(), mem.NewObjects()).ImportFleet(ctx, demoFleet(t), dark, fleetOptions)
	if err != nil {
		t.Fatal(err)
	}
	for id, factor := range got[1].PVFactor {
		if factor != 0 {
			t.Errorf("site %s of a home with no PV has factor %v", id, factor)
		}
	}

	for _, step := range []string{"GetSubstationByCode", "CreateSubstation", "CreateSite"} {
		if err := importFleet(&failingStore{Store: mem.New(), failOn: step}, demoFleet(t), homes()); !errors.Is(err, errInjected) {
			t.Errorf("a failing %s: %v", step, err)
		}
	}
}
