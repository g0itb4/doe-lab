package repotest

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/service"
)

// Run runs the whole suite. newStore returns an empty store for one test.
func Run(t *testing.T, newStore func(t *testing.T) service.Store) {
	tests := map[string]func(*testing.T, service.Store){
		"Feeders":         testFeeders,
		"FeederTree":      testFeederTree,
		"FeederLines":     testFeederLines,
		"Sites":           testSites,
		"SiteSoftDelete":  testSiteSoftDelete,
		"Devices":         testDevices,
		"EnvelopeConfigs": testEnvelopeConfigs,
		"SiteProfiles":    testSiteProfiles,
		"EnvelopeRuns":    testEnvelopeRuns,
		"Envelopes":       testEnvelopes,
		"IdempotencyKeys": testIdempotencyKeys,
		"RunIntervals":    testRunIntervals,
		"Readings":        testReadings,
		"Alerts":          testAlerts,
		"Backstops":       testBackstops,
		"Retention":       testRetention,
		"Transactions":    testTransactions,
		"PageTokens":      testPageTokens,
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			test(t, newStore(t))
		})
	}
}

func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: error = %v, want %v", what, err, want)
	}
}

func noErr(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func testFeeders(t *testing.T, s service.Store) {
	ctx := Ctx()
	var created []domain.Feeder
	for _, code := range []string{"LV30", "LV10", "LV20"} {
		f, err := s.CreateFeeder(ctx, NewFeeder(code))
		noErr(t, "create "+code, err)
		if f.ID == uuid.Nil || f.CreatedAt.IsZero() || !f.UpdatedAt.Equal(f.CreatedAt) {
			t.Errorf("server-set fields of %s: %+v", code, f)
		}
		created = append(created, f)
	}

	got, err := s.GetFeeder(ctx, created[1].ID)
	noErr(t, "get", err)
	if got != created[1] {
		t.Errorf("get = %+v, want %+v", got, created[1])
	}
	byCode, err := s.GetFeederByCode(ctx, "LV20")
	noErr(t, "get by code", err)
	if byCode.ID != created[2].ID {
		t.Errorf("get by code returned %s", byCode.Code)
	}

	_, err = s.GetFeeder(ctx, uuid.New())
	wantErr(t, "get an unknown id", err, domain.ErrNotFound)
	_, err = s.GetFeederByCode(ctx, "NOPE")
	wantErr(t, "get an unknown code", err, domain.ErrNotFound)
	_, err = s.CreateFeeder(ctx, NewFeeder("LV10"))
	wantErr(t, "create a duplicate code", err, domain.ErrAlreadyExists)

	// Two pages, in code order.
	page1, next, err := s.ListFeeders(ctx, domain.Page{Size: 2})
	noErr(t, "list page 1", err)
	if len(page1) != 2 || page1[0].Code != "LV10" || page1[1].Code != "LV20" || next == "" {
		t.Fatalf("page 1 = %v, next %q", codes(page1), next)
	}
	page2, next, err := s.ListFeeders(ctx, domain.Page{Size: 2, Token: next})
	noErr(t, "list page 2", err)
	if len(page2) != 1 || page2[0].Code != "LV30" || next != "" {
		t.Errorf("page 2 = %v, next %q", codes(page2), next)
	}

	// One row a page: the page is cut from more rows than it holds.
	single, next, err := s.ListFeeders(ctx, domain.Page{Size: 1})
	noErr(t, "list one a page", err)
	if len(single) != 1 || single[0].Code != "LV10" || next == "" {
		t.Errorf("a page of one = %v, next %q", codes(single), next)
	}

	// Update changes the name and the tap, and nothing else.
	change := created[1]
	change.Name, change.TapPU, change.Code, change.TransformerKVA = "Renamed", 0.975, "IGNORED", 9999
	updated, err := s.UpdateFeeder(ctx, change)
	noErr(t, "update", err)
	if updated.Name != "Renamed" || updated.TapPU != 0.975 || updated.Code != "LV10" || updated.TransformerKVA != 100 {
		t.Errorf("updated = %+v", updated)
	}
	if !updated.UpdatedAt.After(created[1].UpdatedAt) && !updated.UpdatedAt.Equal(created[1].UpdatedAt) {
		t.Errorf("updated_at went backwards: %v then %v", created[1].UpdatedAt, updated.UpdatedAt)
	}
	_, err = s.UpdateFeeder(ctx, domain.Feeder{ID: uuid.New(), Name: "x", TapPU: 1})
	wantErr(t, "update an unknown feeder", err, domain.ErrNotFound)
}

func codes(fs []domain.Feeder) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Code
	}
	return out
}

func testFeederTree(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	other := Seed(t, s, "LV20", 11)

	got, err := s.GetFeederNode(ctx, f.Mid.ID)
	noErr(t, "get node", err)
	if got.Name != "mid" || got.ParentNodeID == nil || *got.ParentNodeID != f.Root.ID || got.GroundROhm != nil {
		t.Errorf("mid = %+v", got)
	}
	if f.Root.ParentNodeID != nil || f.Root.GroundROhm == nil || *f.Root.GroundROhm != 0.6 {
		t.Errorf("root = %+v", f.Root)
	}
	_, err = s.GetFeederNode(ctx, uuid.New())
	wantErr(t, "get an unknown node", err, domain.ErrNotFound)

	tree, err := s.ListFeederTree(ctx, f.Feeder.ID)
	noErr(t, "tree", err)
	if len(tree) != 4 || tree[0].Name != "house-a" || tree[3].Name != "root" {
		t.Errorf("tree = %d nodes, first %q", len(tree), tree[0].Name)
	}

	page1, next, err := s.ListFeederNodes(ctx, f.Feeder.ID, domain.Page{Size: 3})
	noErr(t, "list nodes", err)
	page2, end, err := s.ListFeederNodes(ctx, f.Feeder.ID, domain.Page{Size: 3, Token: next})
	noErr(t, "list nodes page 2", err)
	if len(page1) != 3 || len(page2) != 1 || page2[0].Name != "root" || end != "" {
		t.Errorf("pages of %d and %d nodes, end %q", len(page1), len(page2), end)
	}

	// The rules that make a feeder a tree.
	_, err = s.CreateFeederNode(ctx, domain.FeederNode{FeederID: f.Feeder.ID, Name: "second-root"})
	wantErr(t, "a second root", err, domain.ErrAlreadyExists)
	_, err = s.CreateFeederNode(ctx, domain.FeederNode{FeederID: f.Feeder.ID, Name: "mid", ParentNodeID: &f.Root.ID})
	wantErr(t, "a duplicate node name", err, domain.ErrAlreadyExists)
	_, err = s.CreateFeederNode(ctx, domain.FeederNode{FeederID: f.Feeder.ID, Name: "stray", ParentNodeID: &other.Root.ID})
	wantErr(t, "a parent in another feeder", err, domain.ErrFailedPrecondition)
	_, err = s.CreateFeederNode(ctx, domain.FeederNode{FeederID: uuid.New(), Name: "orphan"})
	wantErr(t, "a node of an unknown feeder", err, domain.ErrFailedPrecondition)
}

func testFeederLines(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	main := f.Lines[0]

	got, err := s.GetFeederLine(ctx, main.ID)
	noErr(t, "get line", err)
	if got.Name != "main" || len(got.ROhm) != 16 || got.ROhm[0] != 0.04 || got.ROhm[1] != 0 ||
		got.AmpacityA == nil || *got.AmpacityA != 300 || got.AmpacitySource == nil || *got.AmpacitySource != domain.AmpacityAssumed {
		t.Errorf("main = %+v", got)
	}
	_, err = s.GetFeederLine(ctx, uuid.New())
	wantErr(t, "get an unknown line", err, domain.ErrNotFound)

	all, err := s.ListAllFeederLines(ctx, f.Feeder.ID)
	noErr(t, "all lines", err)
	if len(all) != 3 || all[0].Name != "main" || all[2].Name != "service-b" {
		t.Errorf("all lines = %d, first %q", len(all), all[0].Name)
	}
	page1, next, err := s.ListFeederLines(ctx, f.Feeder.ID, domain.Page{Size: 2})
	noErr(t, "list lines", err)
	page2, end, err := s.ListFeederLines(ctx, f.Feeder.ID, domain.Page{Size: 2, Token: next})
	noErr(t, "list lines page 2", err)
	if len(page1) != 2 || len(page2) != 1 || end != "" {
		t.Errorf("pages of %d and %d lines, end %q", len(page1), len(page2), end)
	}

	// A line joins a node to its parent, and there is one line into a node.
	houseC, err := s.CreateFeederNode(ctx, domain.FeederNode{FeederID: f.Feeder.ID, Name: "house-c", ParentNodeID: &f.Mid.ID})
	noErr(t, "create a node with no line yet", err)
	bad := domain.FeederLine{
		FeederID: f.Feeder.ID, Name: "shortcut", FromNodeID: f.Root.ID, ToNodeID: houseC.ID,
		Linecode: "test", LengthM: 10, ROhm: Matrix(1), XOhm: Matrix(1), BS: Matrix(0),
	}
	_, err = s.CreateFeederLine(ctx, bad)
	wantErr(t, "a line that does not join a node to its parent", err, domain.ErrFailedPrecondition)
	bad.FromNodeID, bad.ToNodeID = f.Mid.ID, f.HouseA.ID
	_, err = s.CreateFeederLine(ctx, bad)
	wantErr(t, "a second line into a node", err, domain.ErrAlreadyExists)
	bad.ToNodeID = f.Root.ID
	_, err = s.CreateFeederLine(ctx, bad)
	wantErr(t, "a line into the root", err, domain.ErrFailedPrecondition)
	bad.ToNodeID, bad.Name = houseC.ID, "main"
	_, err = s.CreateFeederLine(ctx, bad)
	wantErr(t, "a duplicate line name", err, domain.ErrAlreadyExists)
	bad.Name, bad.ROhm = "service-c", []float64{1, 2, 3}
	_, err = s.CreateFeederLine(ctx, bad)
	wantErr(t, "a matrix that is not 4x4", err, domain.ErrInvalid)

	// An operator's rating replaces the assumed one; clearing it unrates
	// the line.
	rated, err := s.UpdateFeederLineAmpacity(ctx, main.ID, Ptr(250.0))
	noErr(t, "set ampacity", err)
	if *rated.AmpacityA != 250 || *rated.AmpacitySource != domain.AmpacityOperator {
		t.Errorf("rated = %+v", rated)
	}
	cleared, err := s.UpdateFeederLineAmpacity(ctx, main.ID, nil)
	noErr(t, "clear ampacity", err)
	if cleared.AmpacityA != nil || cleared.AmpacitySource != nil {
		t.Errorf("cleared = %+v", cleared)
	}
	_, err = s.UpdateFeederLineAmpacity(ctx, uuid.New(), Ptr(1.0))
	wantErr(t, "rate an unknown line", err, domain.ErrNotFound)
}

func testSites(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	other := Seed(t, s, "LV20", 11)

	got, err := s.GetSite(ctx, f.SiteA.ID)
	noErr(t, "get", err)
	if got.NMI != NMI(t, 1) || got.Phase != 1 || !got.HasBattery || *got.BatteryKWh != 10 || *got.ProfileCustomer != 1 || got.DeletedAt != nil {
		t.Errorf("site A = %+v", got)
	}
	byNMI, err := s.GetSiteByNMI(ctx, NMI(t, 2))
	noErr(t, "get by NMI", err)
	if byNMI.ID != f.SiteB.ID || byNMI.BatteryKWh != nil || byNMI.ExportCapW != 0 {
		t.Errorf("site B = %+v", byNMI)
	}
	_, err = s.GetSite(ctx, uuid.New())
	wantErr(t, "get an unknown id", err, domain.ErrNotFound)
	_, err = s.GetSiteByNMI(ctx, NMI(t, 999))
	wantErr(t, "get an unknown NMI", err, domain.ErrNotFound)

	// A third site, so the feeder has two on phase 1.
	third, err := s.CreateSite(ctx, domain.Site{
		NMI: NMI(t, 3), FeederID: f.Feeder.ID, NodeID: f.HouseB.ID, Name: "Ld3_LOAD_A", Phase: 1,
	})
	noErr(t, "create a third site", err)

	all, err := s.ListAllSites(ctx, f.Feeder.ID)
	noErr(t, "all sites", err)
	if len(all) != 3 || all[0].ID != f.SiteA.ID || all[2].ID != third.ID {
		t.Errorf("all sites = %d", len(all))
	}
	phase1, _, err := s.ListSites(ctx, f.Feeder.ID, Ptr(int16(1)), domain.Page{Size: 10})
	noErr(t, "list phase 1", err)
	if len(phase1) != 2 || phase1[0].ID != f.SiteA.ID || phase1[1].ID != third.ID {
		t.Errorf("phase 1 = %d sites", len(phase1))
	}
	page1, next, err := s.ListSites(ctx, f.Feeder.ID, nil, domain.Page{Size: 2})
	noErr(t, "list page 1", err)
	page2, end, err := s.ListSites(ctx, f.Feeder.ID, nil, domain.Page{Size: 2, Token: next})
	noErr(t, "list page 2", err)
	if len(page1) != 2 || len(page2) != 1 || page2[0].ID != third.ID || end != "" {
		t.Errorf("pages of %d and %d sites, end %q", len(page1), len(page2), end)
	}
	elsewhere, _, err := s.ListSites(ctx, other.Feeder.ID, nil, domain.Page{Size: 10})
	noErr(t, "list the other feeder", err)
	if len(elsewhere) != 2 {
		t.Errorf("the other feeder lists %d sites, want its own 2", len(elsewhere))
	}

	// What the schema refuses.
	site := domain.Site{NMI: NMI(t, 4), FeederID: f.Feeder.ID, NodeID: f.HouseA.ID, Name: "Ld4", Phase: 3}
	dup := site
	dup.NMI = NMI(t, 1)
	_, err = s.CreateSite(ctx, dup)
	wantErr(t, "a duplicate NMI", err, domain.ErrAlreadyExists)
	badChecksum := site
	badChecksum.NMI = NMI(t, 4)[:10] + string('0'+(NMI(t, 4)[10]-'0'+1)%10)
	_, err = s.CreateSite(ctx, badChecksum)
	wantErr(t, "an NMI with the wrong checksum", err, domain.ErrInvalid)
	wrongFeeder := site
	wrongFeeder.NodeID = other.HouseA.ID
	_, err = s.CreateSite(ctx, wrongFeeder)
	wantErr(t, "a node of another feeder", err, domain.ErrFailedPrecondition)
	sameName := site
	sameName.Name = f.SiteA.Name
	_, err = s.CreateSite(ctx, sameName)
	wantErr(t, "a duplicate site name", err, domain.ErrAlreadyExists)

	// Update changes the DER fields and the caps, and nothing else.
	change := f.SiteB
	change.PVKW, change.InverterKVA, change.ExportCapW, change.ImportCapW = 6.6, 5, 5000, 14000
	change.HasBattery, change.BatteryKWh, change.HasEV = true, Ptr(13.5), true
	change.NMI, change.Phase = "IGNORED", 3
	updated, err := s.UpdateSite(ctx, change)
	noErr(t, "update", err)
	if updated.PVKW != 6.6 || updated.ExportCapW != 5000 || !updated.HasBattery || *updated.BatteryKWh != 13.5 ||
		!updated.HasEV || updated.NMI != NMI(t, 2) || updated.Phase != 2 {
		t.Errorf("updated = %+v", updated)
	}
	_, err = s.UpdateSite(ctx, domain.Site{ID: uuid.New()})
	wantErr(t, "update an unknown site", err, domain.ErrNotFound)
}

func testSiteSoftDelete(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	device, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 5000})
	noErr(t, "create device", err)

	noErr(t, "delete", s.SoftDeleteSite(ctx, f.SiteA.ID))

	_, err = s.GetSite(ctx, f.SiteA.ID)
	wantErr(t, "get a deleted site", err, domain.ErrNotFound)
	_, err = s.GetSiteByNMI(ctx, f.SiteA.NMI)
	wantErr(t, "get a deleted site by NMI", err, domain.ErrNotFound)
	wantErr(t, "delete it again", s.SoftDeleteSite(ctx, f.SiteA.ID), domain.ErrNotFound)
	_, err = s.UpdateSite(ctx, f.SiteA)
	wantErr(t, "update a deleted site", err, domain.ErrNotFound)
	left, _, err := s.ListSites(ctx, f.Feeder.ID, nil, domain.Page{Size: 10})
	noErr(t, "list", err)
	if len(left) != 1 || left[0].ID != f.SiteB.ID {
		t.Errorf("list after delete = %d sites", len(left))
	}

	// The delete cascades to the site's devices.
	_, err = s.GetDevice(ctx, device.ID)
	wantErr(t, "get a device of a deleted site", err, domain.ErrNotFound)

	// An NMI is never reused, even after its site is deleted.
	_, err = s.CreateSite(ctx, domain.Site{
		NMI: f.SiteA.NMI, FeederID: f.Feeder.ID, NodeID: f.HouseA.ID, Name: "Ld9", Phase: 1,
	})
	wantErr(t, "reuse the NMI of a deleted site", err, domain.ErrAlreadyExists)
}

func testDevices(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)

	solar, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 5000})
	noErr(t, "create solar", err)
	battery, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERBattery, RatedW: 5000})
	noErr(t, "create battery", err)
	_, err = s.CreateDevice(ctx, domain.Device{SiteID: f.SiteB.ID, DERType: domain.DEREV, RatedW: 7000})
	noErr(t, "create EV", err)
	if solar.ID == uuid.Nil || solar.CreatedAt.IsZero() || solar.DeletedAt != nil {
		t.Errorf("solar = %+v", solar)
	}

	got, err := s.GetDevice(ctx, battery.ID)
	noErr(t, "get", err)
	if got.DERType != domain.DERBattery || got.SiteID != f.SiteA.ID {
		t.Errorf("battery = %+v", got)
	}
	_, err = s.GetDevice(ctx, uuid.New())
	wantErr(t, "get an unknown device", err, domain.ErrNotFound)

	_, err = s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 3000})
	wantErr(t, "a second solar device on a site", err, domain.ErrAlreadyExists)
	_, err = s.CreateDevice(ctx, domain.Device{SiteID: uuid.New(), DERType: domain.DERSolar, RatedW: 3000})
	wantErr(t, "a device on an unknown site", err, domain.ErrFailedPrecondition)

	all, next, err := s.ListDevices(ctx, service.DeviceFilter{}, domain.Page{Size: 2})
	noErr(t, "list page 1", err)
	rest, end, err := s.ListDevices(ctx, service.DeviceFilter{}, domain.Page{Size: 2, Token: next})
	noErr(t, "list page 2", err)
	if len(all) != 2 || len(rest) != 1 || end != "" || all[0].ID != solar.ID {
		t.Errorf("pages of %d and %d devices, end %q", len(all), len(rest), end)
	}
	onA, _, err := s.ListDevices(ctx, service.DeviceFilter{SiteID: &f.SiteA.ID}, domain.Page{Size: 10})
	noErr(t, "list by site", err)
	batteries, _, err := s.ListDevices(ctx, service.DeviceFilter{DERType: Ptr(domain.DERBattery)}, domain.Page{Size: 10})
	noErr(t, "list by type", err)
	both, _, err := s.ListDevices(ctx, service.DeviceFilter{SiteID: &f.SiteB.ID, DERType: Ptr(domain.DERBattery)}, domain.Page{Size: 10})
	noErr(t, "list by site and type", err)
	if len(onA) != 2 || len(batteries) != 1 || batteries[0].ID != battery.ID || len(both) != 0 {
		t.Errorf("filters: %d on site A, %d batteries, %d batteries on site B", len(onA), len(batteries), len(both))
	}

	change := solar
	change.RatedW, change.DERType = 6600, domain.DEREV
	updated, err := s.UpdateDevice(ctx, change)
	noErr(t, "update", err)
	if updated.RatedW != 6600 || updated.DERType != domain.DERSolar {
		t.Errorf("updated = %+v", updated)
	}
	_, err = s.UpdateDevice(ctx, domain.Device{ID: uuid.New(), RatedW: 1})
	wantErr(t, "update an unknown device", err, domain.ErrNotFound)

	// A deleted device frees its place: the site can have a new one of the
	// same type.
	noErr(t, "delete", s.SoftDeleteDevice(ctx, solar.ID))
	wantErr(t, "delete it again", s.SoftDeleteDevice(ctx, solar.ID), domain.ErrNotFound)
	_, err = s.UpdateDevice(ctx, solar)
	wantErr(t, "update a deleted device", err, domain.ErrNotFound)
	replacement, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 8000})
	noErr(t, "replace the deleted device", err)
	if replacement.ID == solar.ID {
		t.Error("the replacement reused the deleted device's id")
	}
}

func testEnvelopeConfigs(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	other := Seed(t, s, "LV20", 11)

	_, err := s.GetActiveEnvelopeConfig(ctx, f.Feeder.ID)
	wantErr(t, "the active config of a feeder with none", err, domain.ErrNotFound)

	var versions []domain.EnvelopeConfig
	for i := range 3 {
		c := Config(f.Feeder.ID)
		c.VMaxPU = 1.08 + 0.01*float64(i)
		// The repository assigns the version, whatever is asked for.
		c.Version = 99
		created, err := s.CreateEnvelopeConfig(ctx, c)
		noErr(t, "create", err)
		if created.Version != int32(i+1) || created.ID == uuid.Nil || created.CreatedAt.IsZero() || created.CreatedBy != "operator" {
			t.Errorf("version %d = %+v", i+1, created)
		}
		versions = append(versions, created)
	}
	// Versions count per feeder.
	first, err := s.CreateEnvelopeConfig(ctx, Config(other.Feeder.ID))
	noErr(t, "create for the other feeder", err)
	if first.Version != 1 {
		t.Errorf("the other feeder's first version = %d", first.Version)
	}

	got, err := s.GetEnvelopeConfig(ctx, versions[1].ID)
	noErr(t, "get", err)
	if got != versions[1] {
		t.Errorf("get = %+v, want %+v", got, versions[1])
	}
	_, err = s.GetEnvelopeConfig(ctx, uuid.New())
	wantErr(t, "get an unknown config", err, domain.ErrNotFound)

	active, err := s.GetActiveEnvelopeConfig(ctx, f.Feeder.ID)
	noErr(t, "active", err)
	if active.ID != versions[2].ID {
		t.Errorf("active version = %d, want 3", active.Version)
	}

	page1, next, err := s.ListEnvelopeConfigs(ctx, f.Feeder.ID, domain.Page{Size: 2})
	noErr(t, "list page 1", err)
	page2, end, err := s.ListEnvelopeConfigs(ctx, f.Feeder.ID, domain.Page{Size: 2, Token: next})
	noErr(t, "list page 2", err)
	if len(page1) != 2 || page1[0].Version != 3 || page1[1].Version != 2 || len(page2) != 1 || page2[0].Version != 1 || end != "" {
		t.Errorf("pages of %d and %d versions, end %q", len(page1), len(page2), end)
	}

	_, err = s.CreateEnvelopeConfig(ctx, Config(uuid.New()))
	wantErr(t, "a config of an unknown feeder", err, domain.ErrFailedPrecondition)
}

func testSiteProfiles(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)

	noErr(t, "store A", s.ReplaceSiteProfiles(ctx, f.SiteA.ID, Profiles(48, 100)))
	noErr(t, "store B", s.ReplaceSiteProfiles(ctx, f.SiteB.ID, Profiles(48, 500)))

	// A range is from <= ts < to.
	from, to := Day.Add(time.Hour), Day.Add(3*time.Hour)
	rows, next, err := s.ListSiteProfiles(ctx, f.SiteA.ID, from, to, domain.Page{Size: 3})
	noErr(t, "list page 1", err)
	if len(rows) != 3 || !rows[0].TS.Equal(from) || rows[0].LoadW != 120 || rows[0].PVW != 2 || rows[0].SiteID != f.SiteA.ID || next == "" {
		t.Fatalf("page 1 = %+v, next %q", rows, next)
	}
	rest, end, err := s.ListSiteProfiles(ctx, f.SiteA.ID, from, to, domain.Page{Size: 3, Token: next})
	noErr(t, "list page 2", err)
	if len(rest) != 1 || !rest[0].TS.Equal(Day.Add(150*time.Minute)) || end != "" {
		t.Errorf("page 2 = %+v, end %q", rest, end)
	}

	// Every site of the feeder, in time order and then NMI order.
	feeder, err := s.ListFeederProfiles(ctx, f.Feeder.ID, Day, Day.Add(time.Hour))
	noErr(t, "feeder profiles", err)
	if len(feeder) != 4 || feeder[0].SiteID != f.SiteA.ID || feeder[1].SiteID != f.SiteB.ID ||
		!feeder[2].TS.Equal(Day.Add(30*time.Minute)) || feeder[1].LoadW != 500 {
		t.Errorf("feeder profiles = %+v", feeder)
	}

	// Replacing is what makes an import repeatable.
	noErr(t, "replace A", s.ReplaceSiteProfiles(ctx, f.SiteA.ID, Profiles(2, 900)))
	rows, _, err = s.ListSiteProfiles(ctx, f.SiteA.ID, Day, Day.Add(24*time.Hour), domain.Page{Size: 100})
	noErr(t, "list after replace", err)
	if len(rows) != 2 || rows[0].LoadW != 900 {
		t.Errorf("after replace = %d rows", len(rows))
	}

	wantErr(t, "profiles of an unknown site", s.ReplaceSiteProfiles(ctx, uuid.New(), Profiles(1, 1)), domain.ErrFailedPrecondition)
	twice := append(Profiles(1, 1), Profiles(1, 2)...)
	wantErr(t, "two rows for one interval", s.ReplaceSiteProfiles(ctx, f.SiteB.ID, twice), domain.ErrAlreadyExists)
}

func testTransactions(t *testing.T, s service.Store) {
	ctx := Ctx()
	boom := errors.New("boom")

	// A failed transaction leaves nothing behind.
	err := s.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		if _, err := r.CreateFeeder(ctx, NewFeeder("LV10")); err != nil {
			return err
		}
		return boom
	})
	wantErr(t, "the transaction's error", err, boom)
	_, err = s.GetFeederByCode(ctx, "LV10")
	wantErr(t, "a row of a rolled-back transaction", err, domain.ErrNotFound)

	// A transaction sees its own writes, and they last once it commits.
	err = s.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		created, err := r.CreateFeeder(ctx, NewFeeder("LV10"))
		if err != nil {
			return err
		}
		_, err = r.GetFeeder(ctx, created.ID)
		return err
	})
	noErr(t, "commit", err)
	_, err = s.GetFeederByCode(ctx, "LV10")
	noErr(t, "a row of a committed transaction", err)

	// A constraint error inside a transaction is the same domain error.
	err = s.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		_, err := r.CreateFeeder(ctx, NewFeeder("LV10"))
		return err
	})
	wantErr(t, "a duplicate inside a transaction", err, domain.ErrAlreadyExists)
}

func testPageTokens(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	bad := domain.Page{Size: 10, Token: "not-a-token"}

	_, _, err := s.ListFeeders(ctx, bad)
	wantErr(t, "feeders", err, domain.ErrInvalid)
	_, _, err = s.ListFeederNodes(ctx, f.Feeder.ID, bad)
	wantErr(t, "nodes", err, domain.ErrInvalid)
	_, _, err = s.ListFeederLines(ctx, f.Feeder.ID, bad)
	wantErr(t, "lines", err, domain.ErrInvalid)
	_, _, err = s.ListSites(ctx, f.Feeder.ID, nil, bad)
	wantErr(t, "sites", err, domain.ErrInvalid)
	_, _, err = s.ListDevices(ctx, service.DeviceFilter{}, bad)
	wantErr(t, "devices", err, domain.ErrInvalid)
	_, _, err = s.ListEnvelopeConfigs(ctx, f.Feeder.ID, bad)
	wantErr(t, "configs", err, domain.ErrInvalid)
	_, _, err = s.ListSiteProfiles(ctx, f.SiteA.ID, Day, Day.Add(time.Hour), bad)
	wantErr(t, "profiles", err, domain.ErrInvalid)

	_, _, err = s.ListEnvelopeRuns(ctx, f.Feeder.ID, nil, bad)
	wantErr(t, "runs", err, domain.ErrInvalid)
	_, _, err = s.ListEnvelopes(ctx, f.SiteA.ID, Day, Day.Add(time.Hour), false, bad)
	wantErr(t, "envelopes", err, domain.ErrInvalid)
	_, _, err = s.ListRunEnvelopes(ctx, uuid.New(), bad)
	wantErr(t, "run envelopes", err, domain.ErrInvalid)

	_, _, err = s.ListReadings(ctx, uuid.New(), Day, Day.Add(time.Hour), bad)
	wantErr(t, "readings", err, domain.ErrInvalid)
	_, _, err = s.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{}, bad)
	wantErr(t, "alerts", err, domain.ErrInvalid)
	_, _, err = s.ListBackstopEvents(ctx, f.Feeder.ID, bad)
	wantErr(t, "backstops", err, domain.ErrInvalid)

	// Well-formed tokens that hold the wrong kind of key: made for one list,
	// sent to another.
	foreign := domain.Page{Size: 10, Token: pagetoken.Encode("abc")}
	_, _, err = s.ListEnvelopeConfigs(ctx, f.Feeder.ID, foreign)
	wantErr(t, "a config token that is not a version", err, domain.ErrInvalid)
	_, _, err = s.ListSiteProfiles(ctx, f.SiteA.ID, Day, Day.Add(time.Hour), foreign)
	wantErr(t, "a profile token that is not a time", err, domain.ErrInvalid)
	pair := domain.Page{Size: 10, Token: pagetoken.Encode("abc", "def")}
	_, _, err = s.ListEnvelopeRuns(ctx, f.Feeder.ID, nil, pair)
	wantErr(t, "a run token that is not a time", err, domain.ErrInvalid)
	_, _, err = s.ListEnvelopes(ctx, f.SiteA.ID, Day, Day.Add(time.Hour), false, pair)
	wantErr(t, "an envelope token that is not a time", err, domain.ErrInvalid)
	_, _, err = s.ListRunEnvelopes(ctx, uuid.New(), pair)
	wantErr(t, "a run-envelope token that is not a time", err, domain.ErrInvalid)
	_, _, err = s.ListReadings(ctx, uuid.New(), Day, Day.Add(time.Hour), foreign)
	wantErr(t, "a reading token that is not a time", err, domain.ErrInvalid)
	_, _, err = s.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{}, pair)
	wantErr(t, "an alert token that is not a time", err, domain.ErrInvalid)
	_, _, err = s.ListBackstopEvents(ctx, f.Feeder.ID, pair)
	wantErr(t, "a backstop token that is not a time", err, domain.ErrInvalid)
}

// NewRun returns a run ready to create, covering the profile day.
func NewRun(feederID, configID uuid.UUID, key string) domain.EnvelopeRun {
	return domain.EnvelopeRun{
		FeederID: feederID, EnvelopeConfigID: configID, IdempotencyKey: key,
		HorizonFrom: Day, HorizonTo: Day.Add(24 * time.Hour), EngineVersion: "test",
	}
}

// Envelope returns an engine envelope ready to store: the half hour that
// starts slot half hours into Day.
func Envelope(siteID, runID uuid.UUID, slot int, exportW float64) domain.Envelope {
	from := Day.Add(time.Duration(slot) * 30 * time.Minute)
	return domain.Envelope{
		SiteID: siteID, ValidFrom: from, ValidTo: from.Add(30 * time.Minute),
		ExportLimitW: exportW, ImportLimitW: 7000,
		Source: domain.SourceEngine, EnvelopeRunID: &runID,
		ExportBinding: domain.BindingVoltageHigh, ExportBindingElement: "Ld1_LOAD_A",
		ImportBinding: domain.BindingSiteCap,
	}
}

func testEnvelopeRuns(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	other := Seed(t, s, "LV20", 11)
	config, err := s.CreateEnvelopeConfig(ctx, Config(f.Feeder.ID))
	noErr(t, "config", err)

	first, created, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0001"))
	noErr(t, "create", err)
	if !created || first.ID == uuid.Nil || first.Status != domain.RunRunning || first.CompletedAt != nil ||
		first.DurationMS != nil || first.Error != nil || first.StartedAt.IsZero() || first.EnvelopeCount != 0 {
		t.Errorf("first = %+v, created %v", first, created)
	}

	// The same key again creates nothing and returns the first run.
	again, created, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0001"))
	noErr(t, "create again", err)
	if created || again.ID != first.ID {
		t.Errorf("the same key created run %s (created %v), want %s", again.ID, created, first.ID)
	}

	// A config of another feeder.
	_, _, err = s.CreateEnvelopeRun(ctx, NewRun(other.Feeder.ID, config.ID, "run-other"))
	wantErr(t, "a run with another feeder's config", err, domain.ErrFailedPrecondition)
	inverted := NewRun(f.Feeder.ID, config.ID, "run-inverted")
	inverted.HorizonFrom, inverted.HorizonTo = inverted.HorizonTo, inverted.HorizonFrom
	_, _, err = s.CreateEnvelopeRun(ctx, inverted)
	wantErr(t, "an inverted horizon", err, domain.ErrInvalid)

	noErr(t, "count", s.AddEnvelopeRunCount(ctx, first.ID, 94))
	noErr(t, "count", s.AddEnvelopeRunCount(ctx, first.ID, 6))

	done, err := s.CompleteEnvelopeRun(ctx, first.ID, service.RunResult{
		Status: domain.RunCompleted, DurationMS: 241, SiteCount: 94, IntervalCount: 48,
	})
	noErr(t, "complete", err)
	if done.Status != domain.RunCompleted || done.CompletedAt == nil || *done.DurationMS != 241 ||
		done.SiteCount != 94 || done.IntervalCount != 48 || done.EnvelopeCount != 100 || done.Error != nil {
		t.Errorf("completed = %+v", done)
	}
	_, err = s.CompleteEnvelopeRun(ctx, first.ID, service.RunResult{Status: domain.RunCompleted})
	wantErr(t, "complete a finished run", err, domain.ErrFailedPrecondition)
	_, err = s.CompleteEnvelopeRun(ctx, uuid.New(), service.RunResult{Status: domain.RunCompleted})
	wantErr(t, "complete an unknown run", err, domain.ErrNotFound)

	failed, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0002"))
	noErr(t, "create a second run", err)
	_, err = s.CompleteEnvelopeRun(ctx, failed.ID, service.RunResult{Status: domain.RunFailed})
	wantErr(t, "fail a run with no error text", err, domain.ErrInvalid)
	reason := "no convergence at 12:30"
	failed, err = s.CompleteEnvelopeRun(ctx, failed.ID, service.RunResult{Status: domain.RunFailed, DurationMS: 5, Error: &reason})
	noErr(t, "fail", err)
	if failed.Status != domain.RunFailed || *failed.Error != reason {
		t.Errorf("failed = %+v", failed)
	}
	running, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0003"))
	noErr(t, "create a third run", err)

	got, err := s.GetEnvelopeRun(ctx, first.ID)
	noErr(t, "get", err)
	if got.ID != first.ID || got.Status != domain.RunCompleted {
		t.Errorf("get = %+v", got)
	}
	_, err = s.GetEnvelopeRun(ctx, uuid.New())
	wantErr(t, "get an unknown run", err, domain.ErrNotFound)

	// Newest first, in two pages, then by status.
	page1, next, err := s.ListEnvelopeRuns(ctx, f.Feeder.ID, nil, domain.Page{Size: 2})
	noErr(t, "list page 1", err)
	page2, end, err := s.ListEnvelopeRuns(ctx, f.Feeder.ID, nil, domain.Page{Size: 2, Token: next})
	noErr(t, "list page 2", err)
	if len(page1) != 2 || page1[0].ID != running.ID || page1[1].ID != failed.ID || len(page2) != 1 || page2[0].ID != first.ID || end != "" {
		t.Errorf("pages of %d and %d runs, end %q", len(page1), len(page2), end)
	}
	onlyFailed, _, err := s.ListEnvelopeRuns(ctx, f.Feeder.ID, Ptr(domain.RunFailed), domain.Page{Size: 10})
	noErr(t, "list failed", err)
	if len(onlyFailed) != 1 || onlyFailed[0].ID != failed.ID {
		t.Errorf("failed runs = %d", len(onlyFailed))
	}
	elsewhere, _, err := s.ListEnvelopeRuns(ctx, other.Feeder.ID, nil, domain.Page{Size: 10})
	noErr(t, "list the other feeder", err)
	if len(elsewhere) != 0 {
		t.Errorf("the other feeder lists %d runs", len(elsewhere))
	}
	wantErr(t, "count on an unknown run", ignoreMissing(s.AddEnvelopeRunCount(ctx, uuid.New(), 1)), nil)
}

// ignoreMissing accepts either answer to a write on a row that is not there:
// Postgres updates nothing and says nothing, the in-memory store says "not
// found". No caller depends on which.
func ignoreMissing(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

func testEnvelopes(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	config, err := s.CreateEnvelopeConfig(ctx, Config(f.Feeder.ID))
	noErr(t, "config", err)
	run, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0001"))
	noErr(t, "run", err)
	a, b := f.SiteA.ID, f.SiteB.ID

	// Four half hours for site A and two for site B.
	stored, superseded, err := s.ReplaceEnvelopes(ctx, []domain.Envelope{
		Envelope(a, run.ID, 0, 1000), Envelope(a, run.ID, 1, 1100), Envelope(a, run.ID, 2, 1200), Envelope(a, run.ID, 3, 1300),
		Envelope(b, run.ID, 0, 2000), Envelope(b, run.ID, 1, 2100),
	})
	noErr(t, "store", err)
	if len(stored) != 6 || superseded != 0 || stored[0].ID == uuid.Nil || stored[0].ID == stored[1].ID || stored[0].CreatedAt.IsZero() {
		t.Fatalf("stored %d, superseded %d, first %+v", len(stored), superseded, stored[0])
	}

	// The envelope in force: from <= at < to.
	at := func(minutes int) time.Time { return Day.Add(time.Duration(minutes) * time.Minute) }
	for minutes, want := range map[int]float64{0: 1000, 29: 1000, 30: 1100, 119: 1300} {
		got, err := s.GetCurrentEnvelope(ctx, a, at(minutes))
		noErr(t, "current", err)
		if got.ExportLimitW != want {
			t.Errorf("at %d minutes: %v W, want %v", minutes, got.ExportLimitW, want)
		}
	}
	got, err := s.GetCurrentEnvelope(ctx, a, at(45))
	noErr(t, "current", err)
	if got.Source != domain.SourceEngine || *got.EnvelopeRunID != run.ID || got.BackstopEventID != nil || got.SupersededAt != nil ||
		got.ExportBinding != domain.BindingVoltageHigh || got.ExportBindingElement != "Ld1_LOAD_A" || got.ImportBinding != domain.BindingSiteCap ||
		got.ImportLimitW != 7000 || !got.ValidTo.Equal(at(60)) {
		t.Errorf("current = %+v", got)
	}
	_, err = s.GetCurrentEnvelope(ctx, a, at(120))
	wantErr(t, "after the last interval", err, domain.ErrNotFound)
	_, err = s.GetCurrentEnvelope(ctx, a, at(-1))
	wantErr(t, "before the first interval", err, domain.ErrNotFound)

	// A newer run replaces two of site A's intervals. The old rows are kept.
	second, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0002"))
	noErr(t, "second run", err)
	_, superseded, err = s.ReplaceEnvelopes(ctx, []domain.Envelope{
		Envelope(a, second.ID, 1, 1150), Envelope(a, second.ID, 2, 1250), Envelope(a, second.ID, 4, 1450),
	})
	noErr(t, "replace", err)
	if superseded != 2 {
		t.Errorf("superseded %d, want 2: only the intervals that overlap", superseded)
	}
	got, err = s.GetCurrentEnvelope(ctx, a, at(45))
	noErr(t, "current after replace", err)
	if got.ExportLimitW != 1150 || *got.EnvelopeRunID != second.ID {
		t.Errorf("current after replace = %+v", got)
	}

	active, _, err := s.ListEnvelopes(ctx, a, Day, at(24*60), false, domain.Page{Size: 100})
	noErr(t, "list active", err)
	if len(active) != 5 || active[1].ExportLimitW != 1150 || active[4].ExportLimitW != 1450 {
		t.Errorf("%d active envelopes", len(active))
	}
	all, next, err := s.ListEnvelopes(ctx, a, Day, at(24*60), true, domain.Page{Size: 4})
	noErr(t, "list all, page 1", err)
	rest, end, err := s.ListEnvelopes(ctx, a, Day, at(24*60), true, domain.Page{Size: 4, Token: next})
	noErr(t, "list all, page 2", err)
	if len(all) != 4 || len(rest) != 3 || end != "" {
		t.Fatalf("pages of %d and %d, end %q; want 7 rows in all", len(all), len(rest), end)
	}
	// In time order; the superseded row of an interval sorts before its
	// successor, which was stored later.
	whole := slices.Concat(all, rest)
	if whole[1].ExportLimitW != 1100 || whole[1].SupersededAt == nil || whole[2].ExportLimitW != 1150 || whole[2].SupersededAt != nil {
		t.Errorf("the 00:30 interval lists as %v then %v", whole[1].ExportLimitW, whole[2].ExportLimitW)
	}
	ranged, _, err := s.ListEnvelopes(ctx, a, at(30), at(90), false, domain.Page{Size: 100})
	noErr(t, "list a range", err)
	if len(ranged) != 2 || !ranged[0].ValidFrom.Equal(at(30)) || !ranged[1].ValidFrom.Equal(at(60)) {
		t.Errorf("range [00:30, 01:30) = %d envelopes", len(ranged))
	}

	// What a run published, superseded or not, in time then site order.
	published, next, err := s.ListRunEnvelopes(ctx, run.ID, domain.Page{Size: 4})
	noErr(t, "run envelopes page 1", err)
	more, end, err := s.ListRunEnvelopes(ctx, run.ID, domain.Page{Size: 4, Token: next})
	noErr(t, "run envelopes page 2", err)
	if len(published) != 4 || len(more) != 2 || end != "" || !published[0].ValidFrom.Equal(at(0)) || !published[1].ValidFrom.Equal(at(0)) || published[0].SiteID == published[1].SiteID {
		t.Errorf("run envelopes: pages of %d and %d", len(published), len(more))
	}

	// The feeder's active envelopes over a range: two sites at 00:00, in NMI
	// order.
	feeder, err := s.ListFeederEnvelopes(ctx, f.Feeder.ID, Day, at(60))
	noErr(t, "feeder envelopes", err)
	if len(feeder) != 4 || feeder[0].SiteID != a || feeder[1].SiteID != b || feeder[2].ExportLimitW != 1150 {
		t.Errorf("feeder envelopes = %d", len(feeder))
	}

	// What the schema refuses.
	bad := func(change func(*domain.Envelope)) error {
		e := Envelope(b, run.ID, 10, 500)
		change(&e)
		_, _, err := s.ReplaceEnvelopes(ctx, []domain.Envelope{e})
		return err
	}
	wantErr(t, "a negative limit", bad(func(e *domain.Envelope) { e.ExportLimitW = -1 }), domain.ErrInvalid)
	wantErr(t, "a 20-minute interval", bad(func(e *domain.Envelope) { e.ValidTo = e.ValidFrom.Add(20 * time.Minute) }), domain.ErrInvalid)
	wantErr(t, "a start off the grid", bad(func(e *domain.Envelope) {
		e.ValidFrom, e.ValidTo = e.ValidFrom.Add(7*time.Minute), e.ValidTo.Add(7*time.Minute)
	}), domain.ErrInvalid)
	wantErr(t, "an engine envelope with no run", bad(func(e *domain.Envelope) { e.EnvelopeRunID = nil }), domain.ErrInvalid)
	wantErr(t, "a backstop envelope with a run", bad(func(e *domain.Envelope) { e.Source = domain.SourceBackstop }), domain.ErrInvalid)
	wantErr(t, "an unknown site", bad(func(e *domain.Envelope) { e.SiteID = uuid.New() }), domain.ErrFailedPrecondition)
	wantErr(t, "an unknown run", bad(func(e *domain.Envelope) { e.EnvelopeRunID = Ptr(uuid.New()) }), domain.ErrFailedPrecondition)
	_, _, err = s.ReplaceEnvelopes(ctx, []domain.Envelope{Envelope(b, run.ID, 12, 1), Envelope(b, run.ID, 12, 2)})
	wantErr(t, "one interval twice in a batch", err, domain.ErrAlreadyExists)
}

func testIdempotencyKeys(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	config, err := s.CreateEnvelopeConfig(ctx, Config(f.Feeder.ID))
	noErr(t, "config", err)
	run, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0001"))
	noErr(t, "run", err)

	hash := make([]byte, 32)
	hash[0] = 7
	key := domain.IdempotencyKey{Scope: "publish_envelopes", Key: "batch-0001", RequestHash: hash, EnvelopeRunID: &run.ID}
	held, claimed, err := s.ClaimIdempotencyKey(ctx, key)
	noErr(t, "claim", err)
	if !claimed || held.Key != "batch-0001" || held.CreatedAt.IsZero() || !held.ExpiresAt.After(held.CreatedAt) || *held.EnvelopeRunID != run.ID {
		t.Errorf("claimed = %+v, %v", held, claimed)
	}

	// Taken: the holder comes back, with the hash of the first request.
	other := key
	other.RequestHash = make([]byte, 32)
	held, claimed, err = s.ClaimIdempotencyKey(ctx, other)
	noErr(t, "claim again", err)
	if claimed || held.RequestHash[0] != 7 {
		t.Errorf("a taken key: claimed %v, hash %v", claimed, held.RequestHash[:2])
	}

	// The same key in another scope is another key.
	elsewhere := key
	elsewhere.Scope, elsewhere.EnvelopeRunID = "create_backstop", nil
	_, claimed, err = s.ClaimIdempotencyKey(ctx, elsewhere)
	noErr(t, "claim in another scope", err)
	if !claimed {
		t.Error("the same key in another scope was taken")
	}

	short := key
	short.Key, short.RequestHash = "batch-0002", []byte{1, 2, 3}
	_, _, err = s.ClaimIdempotencyKey(ctx, short)
	wantErr(t, "a hash that is not 32 bytes", err, domain.ErrInvalid)
	orphan := key
	orphan.Key, orphan.EnvelopeRunID = "batch-0003", Ptr(uuid.New())
	_, _, err = s.ClaimIdempotencyKey(ctx, orphan)
	wantErr(t, "a key for an unknown run", err, domain.ErrFailedPrecondition)
}

// Interval returns a run interval ready to store: the half hour that starts
// slot half hours into Day.
func Interval(runID, feederID uuid.UUID, slot int, netLoadW float64) domain.EnvelopeRunInterval {
	from := Day.Add(time.Duration(slot) * 30 * time.Minute)
	return domain.EnvelopeRunInterval{
		EnvelopeRunID: runID, FeederID: feederID, ValidFrom: from, ValidTo: from.Add(30 * time.Minute),
		ForecastNetLoadW: netLoadW, ForecastLoadingPct: 12.5, ForecastVMinPU: 1.02, ForecastVMaxPU: 1.06,
		ExportLimitTotalW: 150000, ImportLimitTotalW: 300000, StaticLimitTotalW: 280000,
		StaticVMaxPU: 1.13, StaticBinding: domain.BindingVoltageHigh, StaticBindingElement: "XDLAB000014",
		EnvelopeVMaxPU: Ptr(1.09),
	}
}

func testRunIntervals(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	config, err := s.CreateEnvelopeConfig(ctx, Config(f.Feeder.ID))
	noErr(t, "config", err)
	first, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0001"))
	noErr(t, "run", err)
	second, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0002"))
	noErr(t, "second run", err)

	noErr(t, "store", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{
		Interval(first.ID, f.Feeder.ID, 0, 10000), Interval(first.ID, f.Feeder.ID, 1, 11000), Interval(first.ID, f.Feeder.ID, 2, 12000),
	}))
	rows, err := s.ListEnvelopeRunIntervals(ctx, first.ID)
	noErr(t, "list", err)
	if len(rows) != 3 || rows[1].ForecastNetLoadW != 11000 || rows[0].StaticBinding != domain.BindingVoltageHigh ||
		rows[0].StaticBindingElement != "XDLAB000014" || rows[0].ForecastVMaxPU != 1.06 || rows[0].CreatedAt.IsZero() ||
		rows[0].FeederID != f.Feeder.ID || !rows[2].ValidFrom.Equal(Day.Add(time.Hour)) {
		t.Fatalf("intervals = %+v", rows)
	}

	// A later run covers two of the intervals again: the feeder's series
	// takes its row for those, and the first run's for the rest.
	noErr(t, "store the second run", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{
		Interval(second.ID, f.Feeder.ID, 1, 21000), Interval(second.ID, f.Feeder.ID, 2, 22000), Interval(second.ID, f.Feeder.ID, 3, 23000),
	}))
	series, err := s.ListFeederIntervals(ctx, f.Feeder.ID, Day, Day.Add(24*time.Hour))
	noErr(t, "series", err)
	var loads []float64
	for _, row := range series {
		loads = append(loads, row.ForecastNetLoadW)
	}
	if len(loads) != 4 || loads[0] != 10000 || loads[1] != 21000 || loads[2] != 22000 || loads[3] != 23000 {
		t.Errorf("the feeder's series = %v, want 10000 21000 22000 23000", loads)
	}
	ranged, err := s.ListFeederIntervals(ctx, f.Feeder.ID, Day.Add(30*time.Minute), Day.Add(90*time.Minute))
	noErr(t, "series over a range", err)
	if len(ranged) != 2 || !ranged[0].ValidFrom.Equal(Day.Add(30*time.Minute)) {
		t.Errorf("range [00:30, 01:30) = %d intervals", len(ranged))
	}
	none, err := s.ListEnvelopeRunIntervals(ctx, uuid.New())
	noErr(t, "list an unknown run", err)
	if len(none) != 0 {
		t.Errorf("an unknown run has %d intervals", len(none))
	}

	wantErr(t, "an interval twice", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{Interval(first.ID, f.Feeder.ID, 0, 1)}), domain.ErrAlreadyExists)
	wantErr(t, "an unknown run", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{Interval(uuid.New(), f.Feeder.ID, 9, 1)}), domain.ErrFailedPrecondition)
	inverted := Interval(first.ID, f.Feeder.ID, 9, 1)
	inverted.ForecastVMinPU, inverted.ForecastVMaxPU = 1.1, 0.9
	wantErr(t, "a minimum voltage above the maximum", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{inverted}), domain.ErrInvalid)
	flat := Interval(first.ID, f.Feeder.ID, 9, 1)
	flat.EnvelopeVMaxPU = Ptr(0.0)
	wantErr(t, "an envelope voltage of zero", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{flat}), domain.ErrInvalid)
	// A row from before the engine recorded the envelope voltage has none.
	old := Interval(first.ID, f.Feeder.ID, 9, 1)
	old.EnvelopeVMaxPU = nil
	noErr(t, "an interval with no envelope voltage", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{old}))
	stored, err := s.ListEnvelopeRunIntervals(ctx, first.ID)
	noErr(t, "list", err)
	if last := stored[len(stored)-1]; last.EnvelopeVMaxPU != nil || *stored[0].EnvelopeVMaxPU != 1.09 {
		t.Errorf("envelope voltages = %v and %v, want 1.09 and none", stored[0].EnvelopeVMaxPU, last.EnvelopeVMaxPU)
	}
}

// Reading returns a reading of a device, minutes into Day.
func Reading(deviceID uuid.UUID, seconds int, netExportW float64) domain.Reading {
	return domain.Reading{
		DeviceID: deviceID, TS: Day.Add(time.Duration(seconds) * time.Second),
		PowerW: netExportW + 500, NetExportW: netExportW, VoltageV: Ptr(241.5),
	}
}

func testReadings(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	solar, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 5000})
	noErr(t, "solar", err)
	battery, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERBattery, RatedW: 5000})
	noErr(t, "battery", err)
	ev, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteB.ID, DERType: domain.DEREV, RatedW: 7000})
	noErr(t, "ev", err)

	// Before any reading: every device is known, none has been seen.
	states, err := s.ListDeviceStates(ctx, f.Feeder.ID)
	noErr(t, "states", err)
	if len(states) != 3 || states[0].NMI != f.SiteA.NMI || states[2].DeviceID != ev.ID || states[0].LastSeenAt != nil {
		t.Fatalf("states before any reading = %+v", states)
	}

	withSOC := Reading(battery.ID, 30, 1200)
	withSOC.SOCPct, withSOC.VoltageV = Ptr(55.0), nil
	// The site named in a reading is ignored: it comes from the device.
	wrongSite := Reading(solar.ID, 90, 1400)
	wrongSite.SiteID = f.SiteB.ID
	stored, err := s.InsertReadings(ctx, []domain.Reading{
		Reading(solar.ID, 0, 1000), Reading(solar.ID, 30, 1200), withSOC, wrongSite, Reading(ev.ID, 30, -3000),
	})
	noErr(t, "insert", err)
	if stored != 5 {
		t.Errorf("stored %d readings, want 5", stored)
	}

	// The same readings again are skipped, and so is one of an unknown device.
	stored, err = s.InsertReadings(ctx, []domain.Reading{Reading(solar.ID, 0, 9999), Reading(solar.ID, 120, 1500), Reading(uuid.New(), 0, 1)})
	noErr(t, "insert again", err)
	if stored != 1 {
		t.Errorf("stored %d readings the second time, want 1: the new one", stored)
	}

	rows, next, err := s.ListReadings(ctx, solar.ID, Day, Day.Add(time.Hour), domain.Page{Size: 3})
	noErr(t, "list page 1", err)
	rest, end, err := s.ListReadings(ctx, solar.ID, Day, Day.Add(time.Hour), domain.Page{Size: 3, Token: next})
	noErr(t, "list page 2", err)
	if len(rows) != 3 || len(rest) != 1 || end != "" || rows[0].NetExportW != 1000 || rows[0].SiteID != f.SiteA.ID ||
		rows[2].SiteID != f.SiteA.ID || *rows[0].VoltageV != 241.5 || rows[0].SOCPct != nil || rows[0].ReceivedAt.IsZero() {
		t.Fatalf("readings = %+v then %+v", rows, rest)
	}
	// The first value of a reading wins: a duplicate does not overwrite it.
	if rows[0].NetExportW != 1000 || rows[0].PowerW != 1500 {
		t.Errorf("the duplicate overwrote the reading: %+v", rows[0])
	}
	batteryRows, _, err := s.ListReadings(ctx, battery.ID, Day, Day.Add(time.Hour), domain.Page{Size: 10})
	noErr(t, "list the battery", err)
	if len(batteryRows) != 1 || *batteryRows[0].SOCPct != 55 || batteryRows[0].VoltageV != nil {
		t.Errorf("battery readings = %+v", batteryRows)
	}
	ranged, _, err := s.ListReadings(ctx, solar.ID, Day.Add(30*time.Second), Day.Add(120*time.Second), domain.Page{Size: 10})
	noErr(t, "list a range", err)
	if len(ranged) != 2 {
		t.Errorf("range [00:00:30, 00:02:00) = %d readings, want 2", len(ranged))
	}

	// The status of a device is its latest reading; a late one does not move
	// it back.
	_, err = s.InsertReadings(ctx, []domain.Reading{Reading(solar.ID, 60, 777)})
	noErr(t, "a late reading", err)
	states, err = s.ListDeviceStates(ctx, f.Feeder.ID)
	noErr(t, "states", err)
	for _, state := range states {
		switch state.DeviceID {
		case solar.ID:
			if state.LastSeenAt == nil || !state.LastSeenAt.Equal(Day.Add(120*time.Second)) || state.NetExportW != 1500 || state.DERType != domain.DERSolar {
				t.Errorf("solar state = %+v", state)
			}
		case ev.ID:
			if state.NetExportW != -3000 || state.SiteID != f.SiteB.ID {
				t.Errorf("ev state = %+v", state)
			}
		}
	}

	// By the minute: site A's first minute has solar at 0 s and 30 s and the
	// battery at 30 s: 1000, 1200, 1200.
	power, err := s.ListSitePower(ctx, f.SiteA.ID, Day, Day.Add(time.Hour))
	noErr(t, "site power", err)
	if len(power) != 3 || !power[0].Bucket.Equal(Day) || power[0].ReadingCount != 3 || power[0].MaxNetExportW != 1200 ||
		math.Abs(power[0].AvgNetExportW-3400.0/3) > 1e-9 || *power[0].AvgSOCPct != 55 || *power[0].AvgVoltageV != 241.5 {
		t.Errorf("site power = %+v", power)
	}
	// The second minute has the late reading and the one at 90 s.
	if power[1].ReadingCount != 2 || power[1].AvgSOCPct != nil {
		t.Errorf("second minute = %+v", power[1])
	}

	// The fleet's first minute: site A exports 1133 W, site B imports 3000 W.
	fleet, err := s.ListFleetSeries(ctx, f.Feeder.ID, Day, Day.Add(time.Hour))
	noErr(t, "fleet", err)
	if len(fleet) != 3 || fleet[0].ReportingSites != 2 || fleet[0].ReadingCount != 4 || fleet[0].ImportW != 3000 ||
		math.Abs(fleet[0].ExportW-3400.0/3) > 1e-9 || fleet[0].FeederID != f.Feeder.ID {
		t.Errorf("fleet = %+v", fleet)
	}

	soc := Reading(solar.ID, 300, 1)
	soc.SOCPct = Ptr(101.0)
	_, err = s.InsertReadings(ctx, []domain.Reading{soc})
	wantErr(t, "a state of charge of 101 %", err, domain.ErrInvalid)
	volts := Reading(solar.ID, 300, 1)
	volts.VoltageV = Ptr(0.0)
	_, err = s.InsertReadings(ctx, []domain.Reading{volts})
	wantErr(t, "a voltage of zero", err, domain.ErrInvalid)
}

func testAlerts(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	other := Seed(t, s, "LV20", 11)
	device, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 5000})
	noErr(t, "device", err)

	breach := domain.Alert{
		SiteID: f.SiteA.ID, FeederID: f.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
		OpenedAt: Day.Add(time.Hour), LimitW: Ptr(1500.0), PeakW: Ptr(2100.0), Detail: "export above the limit",
	}
	first, opened, err := s.OpenAlert(ctx, breach)
	noErr(t, "open", err)
	if !opened || first.ID == uuid.Nil || first.FeederID != f.Feeder.ID || first.ResolvedAt != nil || first.AcknowledgedAt != nil || *first.PeakW != 2100 || first.Detail == "" {
		t.Errorf("first = %+v, opened %v", first, opened)
	}

	// A breach that continues is the same alert, with a higher peak; a lower
	// reading does not bring the peak down.
	breach.PeakW = Ptr(2600.0)
	again, opened, err := s.OpenAlert(ctx, breach)
	noErr(t, "open again", err)
	if opened || again.ID != first.ID || *again.PeakW != 2600 {
		t.Errorf("a continuing breach = %+v, opened %v", again, opened)
	}
	breach.PeakW = Ptr(1800.0)
	again, _, err = s.OpenAlert(ctx, breach)
	noErr(t, "open with a lower peak", err)
	if *again.PeakW != 2600 {
		t.Errorf("peak fell to %v", *again.PeakW)
	}
	// A breach that turns serious is the same alert, with a higher severity;
	// it does not come down again while the alert is open.
	breach.Severity = domain.SeverityCritical
	again, opened, err = s.OpenAlert(ctx, breach)
	noErr(t, "open as critical", err)
	if opened || again.ID != first.ID || again.Severity != domain.SeverityCritical {
		t.Errorf("an escalated breach = %+v, opened %v", again, opened)
	}
	breach.Severity = domain.SeverityWarning
	again, _, err = s.OpenAlert(ctx, breach)
	noErr(t, "open as a warning again", err)
	if again.Severity != domain.SeverityCritical {
		t.Errorf("severity fell to %v", again.Severity)
	}

	// Another kind on the same site is another alert.
	offline, opened, err := s.OpenAlert(ctx, domain.Alert{
		SiteID: f.SiteA.ID, FeederID: f.Feeder.ID, DeviceID: &device.ID, Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo, OpenedAt: Day.Add(2 * time.Hour),
	})
	noErr(t, "open offline", err)
	if !opened || offline.ID == first.ID || *offline.DeviceID != device.ID {
		t.Errorf("offline = %+v", offline)
	}
	_, _, err = s.OpenAlert(ctx, domain.Alert{
		SiteID: other.SiteA.ID, FeederID: other.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
		OpenedAt: Day.Add(3 * time.Hour), LimitW: Ptr(0.0), PeakW: Ptr(50.0),
	})
	noErr(t, "open on the other feeder", err)

	n, err := s.CountOpenAlerts(ctx, f.Feeder.ID)
	noErr(t, "count", err)
	if n != 2 {
		t.Errorf("%d open alerts, want 2", n)
	}

	got, err := s.GetAlert(ctx, first.ID)
	noErr(t, "get", err)
	if got.ID != first.ID || *got.LimitW != 1500 || got.Kind != domain.AlertConstraintBreach || got.Severity != domain.SeverityCritical {
		t.Errorf("get = %+v", got)
	}
	_, err = s.GetAlert(ctx, uuid.New())
	wantErr(t, "get an unknown alert", err, domain.ErrNotFound)

	// Acknowledge: once; a second time changes nothing.
	acked, err := s.AcknowledgeAlert(ctx, first.ID, "operator")
	noErr(t, "acknowledge", err)
	if acked.AcknowledgedAt == nil || *acked.AcknowledgedBy != "operator" {
		t.Errorf("acknowledged = %+v", acked)
	}
	twice, err := s.AcknowledgeAlert(ctx, first.ID, "someone-else")
	noErr(t, "acknowledge again", err)
	if *twice.AcknowledgedBy != "operator" || !twice.AcknowledgedAt.Equal(*acked.AcknowledgedAt) {
		t.Errorf("a second acknowledgement changed the alert: %+v", twice)
	}
	_, err = s.AcknowledgeAlert(ctx, uuid.New(), "operator")
	wantErr(t, "acknowledge an unknown alert", err, domain.ErrNotFound)

	// Resolve: the open alert of the site and kind, once.
	resolved, err := s.ResolveAlert(ctx, f.SiteA.ID, domain.AlertConstraintBreach, Day.Add(90*time.Minute))
	noErr(t, "resolve", err)
	if !resolved {
		t.Error("the open breach was not resolved")
	}
	resolved, err = s.ResolveAlert(ctx, f.SiteA.ID, domain.AlertConstraintBreach, Day.Add(95*time.Minute))
	noErr(t, "resolve again", err)
	if resolved {
		t.Error("a breach was resolved twice")
	}
	got, _ = s.GetAlert(ctx, first.ID)
	if got.ResolvedAt == nil || !got.ResolvedAt.Equal(Day.Add(90*time.Minute)) {
		t.Errorf("resolved at %v", got.ResolvedAt)
	}
	// A resolution dated before the alert opened is clamped to its opening.
	_, err = s.ResolveAlert(ctx, f.SiteA.ID, domain.AlertDeviceOffline, Day)
	noErr(t, "resolve before opening", err)
	got, _ = s.GetAlert(ctx, offline.ID)
	if got.ResolvedAt == nil || !got.ResolvedAt.Equal(offline.OpenedAt) {
		t.Errorf("the offline alert resolved at %v, want its opening %v", got.ResolvedAt, offline.OpenedAt)
	}
	// Once resolved, the site can breach again: a new alert.
	breach.OpenedAt, breach.PeakW = Day.Add(4*time.Hour), Ptr(1900.0)
	second, opened, err := s.OpenAlert(ctx, breach)
	noErr(t, "open after resolve", err)
	if !opened || second.ID == first.ID {
		t.Errorf("after resolve: %+v, opened %v", second, opened)
	}

	// Newest first, in pages; then each filter.
	page1, next, err := s.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{}, domain.Page{Size: 2})
	noErr(t, "list page 1", err)
	page2, end, err := s.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{}, domain.Page{Size: 2, Token: next})
	noErr(t, "list page 2", err)
	if len(page1) != 2 || page1[0].ID != second.ID || page1[1].ID != offline.ID || len(page2) != 1 || page2[0].ID != first.ID || end != "" {
		t.Errorf("pages of %d and %d alerts, end %q", len(page1), len(page2), end)
	}
	breaches, _, err := s.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{Kind: Ptr(domain.AlertConstraintBreach)}, domain.Page{Size: 10})
	noErr(t, "list breaches", err)
	open, _, err := s.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{OpenOnly: true}, domain.Page{Size: 10})
	noErr(t, "list open", err)
	onB, _, err := s.ListAlerts(ctx, f.Feeder.ID, service.AlertFilter{SiteID: &f.SiteB.ID}, domain.Page{Size: 10})
	noErr(t, "list site B", err)
	if len(breaches) != 2 || len(open) != 1 || open[0].ID != second.ID || len(onB) != 0 {
		t.Errorf("filters: %d breaches, %d open, %d on site B", len(breaches), len(open), len(onB))
	}

	// What the schema refuses.
	_, _, err = s.OpenAlert(ctx, domain.Alert{SiteID: f.SiteB.ID, FeederID: f.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning, OpenedAt: Day})
	wantErr(t, "a breach with no measurement", err, domain.ErrInvalid)
	_, _, err = s.OpenAlert(ctx, domain.Alert{SiteID: f.SiteB.ID, FeederID: f.Feeder.ID, Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo, OpenedAt: Day})
	wantErr(t, "an offline alert with no device", err, domain.ErrInvalid)
	_, _, err = s.OpenAlert(ctx, domain.Alert{SiteID: f.SiteB.ID, FeederID: f.Feeder.ID, DeviceID: &device.ID, Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo, OpenedAt: Day})
	wantErr(t, "an alert naming another site's device", err, domain.ErrFailedPrecondition)
	_, _, err = s.OpenAlert(ctx, domain.Alert{SiteID: f.SiteB.ID, FeederID: other.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning, OpenedAt: Day, LimitW: Ptr(1.0), PeakW: Ptr(2.0)})
	wantErr(t, "an alert whose feeder is not its site's", err, domain.ErrFailedPrecondition)
	_, _, err = s.OpenAlert(ctx, domain.Alert{SiteID: uuid.New(), FeederID: f.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning, OpenedAt: Day, LimitW: Ptr(1.0), PeakW: Ptr(2.0)})
	wantErr(t, "an alert on an unknown site", err, domain.ErrFailedPrecondition)
}

func testBackstops(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	config, err := s.CreateEnvelopeConfig(ctx, Config(f.Feeder.ID))
	noErr(t, "config", err)
	run, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-0001"))
	noErr(t, "run", err)
	a, b := f.SiteA.ID, f.SiteB.ID

	_, err = s.GetActiveBackstopEvent(ctx, f.Feeder.ID)
	wantErr(t, "the active backstop of a feeder with none", err, domain.ErrNotFound)

	event := domain.BackstopEvent{FeederID: f.Feeder.ID, Reason: "storm", ExportLimitW: 0, TriggeredBy: "operator", TriggeredAt: Day.Add(time.Hour)}
	first, err := s.CreateBackstopEvent(ctx, event, []uuid.UUID{a, b})
	noErr(t, "create", err)
	if first.ID == uuid.Nil || first.ClearedAt != nil || first.ClearedBy != nil || first.Reason != "storm" || !first.TriggeredAt.Equal(Day.Add(time.Hour)) {
		t.Errorf("first = %+v", first)
	}
	sites, err := s.ListBackstopEventSiteIDs(ctx, first.ID)
	noErr(t, "sites", err)
	if len(sites) != 2 {
		t.Errorf("the backstop covers %d sites, want 2", len(sites))
	}
	active, err := s.GetActiveBackstopEvent(ctx, f.Feeder.ID)
	noErr(t, "active", err)
	if active.ID != first.ID {
		t.Errorf("active = %s", active.ID)
	}

	// One active backstop per feeder.
	_, err = s.CreateBackstopEvent(ctx, event, []uuid.UUID{a})
	wantErr(t, "a second active backstop", err, domain.ErrAlreadyExists)
	_, err = s.CreateBackstopEvent(ctx, domain.BackstopEvent{FeederID: uuid.New(), Reason: "x", TriggeredBy: "operator", TriggeredAt: Day}, nil)
	wantErr(t, "a backstop on an unknown feeder", err, domain.ErrFailedPrecondition)

	cleared, err := s.ClearBackstopEvent(ctx, first.ID, "operator", Day.Add(2*time.Hour))
	noErr(t, "clear", err)
	if cleared.ClearedAt == nil || !cleared.ClearedAt.Equal(Day.Add(2*time.Hour)) || *cleared.ClearedBy != "operator" {
		t.Errorf("cleared = %+v", cleared)
	}
	_, err = s.ClearBackstopEvent(ctx, first.ID, "operator", Day.Add(3*time.Hour))
	wantErr(t, "clear twice", err, domain.ErrFailedPrecondition)
	_, err = s.ClearBackstopEvent(ctx, uuid.New(), "operator", Day)
	wantErr(t, "clear an unknown backstop", err, domain.ErrNotFound)
	_, err = s.GetActiveBackstopEvent(ctx, f.Feeder.ID)
	wantErr(t, "the active backstop after a clear", err, domain.ErrNotFound)

	// Cleared, the feeder can have another. One that is cleared "before" it
	// began ends just after instead.
	second, err := s.CreateBackstopEvent(ctx, domain.BackstopEvent{FeederID: f.Feeder.ID, Reason: "again", ExportLimitW: 1500, TriggeredBy: "operator", TriggeredAt: Day.Add(5 * time.Hour)}, []uuid.UUID{a})
	noErr(t, "create a second", err)
	early, err := s.ClearBackstopEvent(ctx, second.ID, "operator", Day)
	noErr(t, "clear early", err)
	if !early.ClearedAt.After(early.TriggeredAt) {
		t.Errorf("cleared at %v, triggered at %v", early.ClearedAt, early.TriggeredAt)
	}

	got, err := s.GetBackstopEvent(ctx, second.ID)
	noErr(t, "get", err)
	if got.ExportLimitW != 1500 || got.Reason != "again" {
		t.Errorf("get = %+v", got)
	}
	_, err = s.GetBackstopEvent(ctx, uuid.New())
	wantErr(t, "get an unknown backstop", err, domain.ErrNotFound)
	page1, next, err := s.ListBackstopEvents(ctx, f.Feeder.ID, domain.Page{Size: 1})
	noErr(t, "list page 1", err)
	page2, end, err := s.ListBackstopEvents(ctx, f.Feeder.ID, domain.Page{Size: 1, Token: next})
	noErr(t, "list page 2", err)
	if len(page1) != 1 || page1[0].ID != second.ID || len(page2) != 1 || page2[0].ID != first.ID || end != "" {
		t.Errorf("pages = %d and %d, end %q", len(page1), len(page2), end)
	}

	// What a backstop takes over, and what it gives back.
	_, _, err = s.ReplaceEnvelopes(ctx, []domain.Envelope{
		Envelope(a, run.ID, 0, 1000), Envelope(a, run.ID, 1, 1100), Envelope(a, run.ID, 2, 1200), Envelope(b, run.ID, 1, 2100),
	})
	noErr(t, "engine envelopes", err)
	from := Day.Add(40 * time.Minute) // inside interval 1
	activeNow, err := s.ListActiveEnvelopes(ctx, []uuid.UUID{a, b}, from)
	noErr(t, "active envelopes", err)
	if len(activeNow) != 3 {
		t.Fatalf("%d active envelopes from 00:40, want 3: the one in force and what follows", len(activeNow))
	}
	// A backstop envelope supersedes the engine's for interval 1 of site A.
	override := Envelope(a, run.ID, 1, 0)
	override.Source, override.EnvelopeRunID, override.BackstopEventID = domain.SourceBackstop, nil, &second.ID
	_, superseded, err := s.ReplaceEnvelopes(ctx, []domain.Envelope{override})
	noErr(t, "backstop envelope", err)
	if superseded != 1 {
		t.Errorf("superseded %d", superseded)
	}
	// And a later engine run had replaced interval 2 before that.
	_, _, err = s.ReplaceEnvelopes(ctx, []domain.Envelope{Envelope(a, run.ID, 2, 1250)})
	noErr(t, "newer engine envelope", err)

	latest, err := s.ListLatestEngineEnvelopes(ctx, []uuid.UUID{a}, from)
	noErr(t, "latest engine envelopes", err)
	// Interval 1: the engine's 1100, although it is superseded. Interval 2:
	// the newer 1250, not the older 1200. Interval 0 has ended.
	if len(latest) != 2 || latest[0].ExportLimitW != 1100 || latest[0].SupersededAt == nil || latest[1].ExportLimitW != 1250 {
		t.Errorf("latest engine envelopes = %+v", latest)
	}
	none, err := s.ListActiveEnvelopes(ctx, nil, from)
	noErr(t, "active envelopes of no sites", err)
	if len(none) != 0 {
		t.Errorf("no sites have %d envelopes", len(none))
	}
}

// Old history goes, and what is recent, open or active stays.
func testRetention(t *testing.T, s service.Store) {
	ctx := Ctx()
	f := Seed(t, s, "LV10", 1)
	config, err := s.CreateEnvelopeConfig(ctx, Config(f.Feeder.ID))
	noErr(t, "config", err)
	device, err := s.CreateDevice(ctx, domain.Device{SiteID: f.SiteA.ID, DERType: domain.DERSolar, RatedW: 5000})
	noErr(t, "device", err)
	a := f.SiteA.ID
	// Two months on: well past any cutoff the test uses.
	later := Day.Add(60 * 24 * time.Hour)

	// An old run with an envelope, an interval and an idempotency key; and a
	// recent one.
	old, _, err := s.CreateEnvelopeRun(ctx, NewRun(f.Feeder.ID, config.ID, "run-old-0001"))
	noErr(t, "old run", err)
	recentRun := NewRun(f.Feeder.ID, config.ID, "run-new-0001")
	recentRun.HorizonFrom, recentRun.HorizonTo = later, later.Add(24*time.Hour)
	recent, _, err := s.CreateEnvelopeRun(ctx, recentRun)
	noErr(t, "recent run", err)
	recentEnvelope := Envelope(a, recent.ID, 0, 2000)
	recentEnvelope.ValidFrom, recentEnvelope.ValidTo = later, later.Add(30*time.Minute)
	_, _, err = s.ReplaceEnvelopes(ctx, []domain.Envelope{Envelope(a, old.ID, 0, 1000), recentEnvelope})
	noErr(t, "envelopes", err)
	noErr(t, "interval", s.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{Interval(old.ID, f.Feeder.ID, 0, 1)}))
	_, _, err = s.ClaimIdempotencyKey(ctx, domain.IdempotencyKey{Scope: "publish_envelopes", Key: "batch-old-0001", RequestHash: bytes.Repeat([]byte{7}, 32), EnvelopeRunID: &old.ID})
	noErr(t, "key", err)

	// Readings then and now.
	fresh := Reading(device.ID, 0, 900)
	fresh.TS = later
	_, err = s.InsertReadings(ctx, []domain.Reading{Reading(device.ID, 0, 1000), Reading(device.ID, 60, 1100), fresh})
	noErr(t, "readings", err)

	// An alert that was resolved long ago, one that is still open, and one
	// that was resolved recently.
	breach := domain.Alert{
		SiteID: a, FeederID: f.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
		OpenedAt: Day, LimitW: Ptr(1000.0), PeakW: Ptr(2000.0),
	}
	resolved, _, err := s.OpenAlert(ctx, breach)
	noErr(t, "old alert", err)
	_, err = s.ResolveAlert(ctx, a, domain.AlertConstraintBreach, Day.Add(time.Hour))
	noErr(t, "resolve", err)
	open, _, err := s.OpenAlert(ctx, domain.Alert{
		SiteID: a, FeederID: f.Feeder.ID, DeviceID: &device.ID, Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo, OpenedAt: Day,
	})
	noErr(t, "open alert", err)
	breach.OpenedAt = later
	lately, _, err := s.OpenAlert(ctx, breach)
	noErr(t, "recent alert", err)
	_, err = s.ResolveAlert(ctx, a, domain.AlertConstraintBreach, later.Add(time.Hour))
	noErr(t, "resolve the recent alert", err)

	// A backstop that was cleared long ago, and one that is active.
	cleared, err := s.CreateBackstopEvent(ctx, domain.BackstopEvent{FeederID: f.Feeder.ID, Reason: "storm", TriggeredBy: "operator", TriggeredAt: Day.Add(2 * time.Hour)}, []uuid.UUID{a})
	noErr(t, "old backstop", err)
	_, err = s.ClearBackstopEvent(ctx, cleared.ID, "operator", Day.Add(3*time.Hour))
	noErr(t, "clear", err)
	active, err := s.CreateBackstopEvent(ctx, domain.BackstopEvent{FeederID: f.Feeder.ID, Reason: "fault", TriggeredBy: "operator", TriggeredAt: Day.Add(4 * time.Hour)}, []uuid.UUID{a})
	noErr(t, "active backstop", err)

	readings := func(from time.Time) int {
		rows, _, err := s.ListReadings(ctx, device.ID, from, from.Add(24*time.Hour), domain.Page{Size: 100})
		noErr(t, "list readings", err)
		return len(rows)
	}
	envelopes := func(from time.Time) int {
		rows, _, err := s.ListEnvelopes(ctx, a, from, from.Add(24*time.Hour), true, domain.Page{Size: 100})
		noErr(t, "list envelopes", err)
		return len(rows)
	}

	// A cutoff before everything removes nothing.
	early := Day.Add(-24 * time.Hour)
	n, err := s.PurgeBefore(ctx, early, early, early)
	noErr(t, "purge before everything", err)
	if n != 0 || readings(Day) != 2 || envelopes(Day) != 1 {
		t.Fatalf("a cutoff before everything removed %d rows; %d readings and %d envelopes are left", n, readings(Day), envelopes(Day))
	}

	// A month on: the old run, the old alert and the cleared backstop go,
	// with the readings and the envelopes of their time.
	cutoff := Day.Add(30 * 24 * time.Hour)
	n, err = s.PurgeBefore(ctx, cutoff, cutoff, cutoff)
	noErr(t, "purge", err)
	if n != 3 {
		t.Errorf("the purge removed %d rows, want 3: a run, an alert and a backstop", n)
	}
	if readings(Day) != 0 || envelopes(Day) != 0 || readings(later) != 1 || envelopes(later) != 1 {
		t.Errorf("after the purge: %d old and %d recent readings, %d old and %d recent envelopes; want 0, 1, 0, 1",
			readings(Day), readings(later), envelopes(Day), envelopes(later))
	}
	_, err = s.GetEnvelopeRun(ctx, old.ID)
	wantErr(t, "the old run", err, domain.ErrNotFound)
	intervals, err := s.ListEnvelopeRunIntervals(ctx, old.ID)
	noErr(t, "intervals", err)
	if len(intervals) != 0 {
		t.Errorf("the old run left %d intervals", len(intervals))
	}
	// Its key went with it: the same key can be claimed again.
	_, claimed, err := s.ClaimIdempotencyKey(ctx, domain.IdempotencyKey{Scope: "publish_envelopes", Key: "batch-old-0001", RequestHash: bytes.Repeat([]byte{7}, 32), EnvelopeRunID: &recent.ID})
	noErr(t, "claim the old key again", err)
	if !claimed {
		t.Error("the old run's idempotency key is still held")
	}
	_, err = s.GetEnvelopeRun(ctx, recent.ID)
	noErr(t, "the recent run", err)

	_, err = s.GetAlert(ctx, resolved.ID)
	wantErr(t, "the alert resolved long ago", err, domain.ErrNotFound)
	for name, id := range map[string]uuid.UUID{"the open alert": open.ID, "the alert resolved recently": lately.ID} {
		_, err = s.GetAlert(ctx, id)
		noErr(t, name, err)
	}
	_, err = s.GetBackstopEvent(ctx, cleared.ID)
	wantErr(t, "the backstop cleared long ago", err, domain.ErrNotFound)
	_, err = s.GetBackstopEvent(ctx, active.ID)
	noErr(t, "the active backstop", err)

	// Again: there is nothing more to remove.
	n, err = s.PurgeBefore(ctx, cutoff, cutoff, cutoff)
	noErr(t, "purge again", err)
	if n != 0 {
		t.Errorf("a second purge removed %d rows", n)
	}
}
