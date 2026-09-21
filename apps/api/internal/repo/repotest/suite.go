package repotest

import (
	"context"
	"errors"
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

	// Well-formed tokens that hold the wrong kind of key: made for one list,
	// sent to another.
	foreign := domain.Page{Size: 10, Token: pagetoken.Encode("abc")}
	_, _, err = s.ListEnvelopeConfigs(ctx, f.Feeder.ID, foreign)
	wantErr(t, "a config token that is not a version", err, domain.ErrInvalid)
	_, _, err = s.ListSiteProfiles(ctx, f.SiteA.ID, Day, Day.Add(time.Hour), foreign)
	wantErr(t, "a profile token that is not a time", err, domain.ErrInvalid)
}
