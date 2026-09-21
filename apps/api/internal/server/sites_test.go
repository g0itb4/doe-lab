package server_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/repo/repotest"
)

func TestSiteGet(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	c := a.sites("")

	byID, err := c.GetSite(ctx, req(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Id{Id: a.fixture.SiteA.ID.String()}}))
	noErr(t, "get by id", err)
	s := byID.Msg.GetSite()
	if s.GetNmi() != a.fixture.SiteA.NMI || s.GetPhase() != 1 || s.GetExportCapW() != 5000 || !s.GetHasBattery() ||
		s.GetBatteryKwh() != 10 || s.GetProfileCustomer() != 1 || s.GetNodeId() != a.fixture.HouseA.ID.String() {
		t.Errorf("site A = %v", s)
	}
	byNMI, err := c.GetSite(ctx, req(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Nmi{Nmi: a.fixture.SiteB.NMI}}))
	noErr(t, "get by NMI", err)
	// A site with no battery has no battery size: absent, not zero.
	if byNMI.Msg.GetSite().GetId() != a.fixture.SiteB.ID.String() || byNMI.Msg.GetSite().BatteryKwh != nil {
		t.Errorf("site B = %v", byNMI.Msg.GetSite())
	}

	_, err = c.GetSite(ctx, req(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Id{Id: unknownID}}))
	wantCode(t, "unknown id", err, connect.CodeNotFound)
	_, err = c.GetSite(ctx, req(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Nmi{Nmi: repotest.NMI(t, 999)}}))
	wantCode(t, "unknown NMI", err, connect.CodeNotFound)
	_, err = c.GetSite(ctx, req(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Nmi{Nmi: "short"}}))
	wantViolation(t, "malformed NMI", err, "nmi")
	_, err = c.GetSite(ctx, req(&doelabv1.GetSiteRequest{}))
	wantViolation(t, "no key", err, "key")
}

func TestSiteList(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	c := a.sites("")
	feederID := a.fixture.Feeder.ID.String()

	page1, err := c.ListSites(ctx, req(&doelabv1.ListSitesRequest{FeederId: feederID, PageSize: 1}))
	noErr(t, "page 1", err)
	page2, err := c.ListSites(ctx, req(&doelabv1.ListSitesRequest{FeederId: feederID, PageSize: 1, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page1.Msg.GetSites()) != 1 || page1.Msg.GetSites()[0].GetNmi() != a.fixture.SiteA.NMI ||
		len(page2.Msg.GetSites()) != 1 || page2.Msg.GetSites()[0].GetNmi() != a.fixture.SiteB.NMI || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("pages = %v then %v", page1.Msg, page2.Msg)
	}
	phase2, err := c.ListSites(ctx, req(&doelabv1.ListSitesRequest{FeederId: feederID, Phase: repotest.Ptr(int32(2))}))
	noErr(t, "phase filter", err)
	if len(phase2.Msg.GetSites()) != 1 || phase2.Msg.GetSites()[0].GetPhase() != 2 {
		t.Errorf("phase 2 = %v", phase2.Msg)
	}
	phase3, err := c.ListSites(ctx, req(&doelabv1.ListSitesRequest{FeederId: feederID, Phase: repotest.Ptr(int32(3))}))
	noErr(t, "empty filter", err)
	if len(phase3.Msg.GetSites()) != 0 || phase3.Msg.GetNextPageToken() != "" {
		t.Errorf("phase 3 = %v", phase3.Msg)
	}

	_, err = c.ListSites(ctx, req(&doelabv1.ListSitesRequest{FeederId: feederID, Phase: repotest.Ptr(int32(4))}))
	wantViolation(t, "phase 4", err, "phase")
	_, err = c.ListSites(ctx, req(&doelabv1.ListSitesRequest{FeederId: unknownID}))
	wantCode(t, "unknown feeder", err, connect.CodeNotFound)
	_, err = c.ListSites(ctx, req(&doelabv1.ListSitesRequest{}))
	wantViolation(t, "no feeder", err, "feeder_id")
}

func TestSiteCreate(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	valid := func() *doelabv1.Site {
		return &doelabv1.Site{
			Nmi: repotest.NMI(t, 50), FeederId: a.fixture.Feeder.ID.String(), NodeId: a.fixture.HouseB.ID.String(),
			Name: "Ld50_LOAD_C", Phase: 3, PvKw: 6.6, InverterKva: 5, ExportCapW: 5000, ImportCapW: 14000,
			HasBattery: true, BatteryKwh: repotest.Ptr(13.5), HasEv: true, ProfileCustomer: repotest.Ptr(int32(42)),
		}
	}
	create := func(token string, s *doelabv1.Site) (*doelabv1.Site, error) {
		res, err := a.sites(token).CreateSite(ctx, req(&doelabv1.CreateSiteRequest{Site: s}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetSite(), nil
	}

	got, err := create(operatorToken, valid())
	noErr(t, "create", err)
	if got.GetId() == "" || got.GetCreatedAt() == nil || got.GetNmi() != repotest.NMI(t, 50) || got.GetPhase() != 3 ||
		got.GetBatteryKwh() != 13.5 || !got.GetHasEv() || got.GetProfileCustomer() != 42 || got.GetImportCapW() != 14000 {
		t.Errorf("created = %v", got)
	}
	// It is there to read.
	read, err := a.sites("").GetSite(ctx, req(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Id{Id: got.GetId()}}))
	noErr(t, "read back", err)
	if read.Msg.GetSite().GetName() != "Ld50_LOAD_C" {
		t.Errorf("read back = %v", read.Msg.GetSite())
	}

	_, err = create(operatorToken, valid())
	wantCode(t, "a duplicate NMI", err, connect.CodeAlreadyExists)

	change := func(f func(*doelabv1.Site)) *doelabv1.Site {
		s := valid()
		s.Nmi, s.Name = repotest.NMI(t, 51), "Ld51"
		f(s)
		return s
	}
	// Shape, refused by the validation interceptor.
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.Nmi = "lowercase01" }))
	wantViolation(t, "malformed NMI", err, "nmi")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.Phase = 4 }))
	wantViolation(t, "phase 4", err, "phase")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.Phase = 0 }))
	wantViolation(t, "no phase", err, "required")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.Name = "" }))
	wantViolation(t, "no name", err, "required")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.ExportCapW = -1 }))
	wantViolation(t, "negative cap", err, "export_cap_w")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.BatteryKwh = nil }))
	wantViolation(t, "a battery with no size", err, "battery_kwh")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.HasBattery = false }))
	wantViolation(t, "a size with no battery", err, "battery_kwh")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.ProfileCustomer = repotest.Ptr(int32(301)) }))
	wantViolation(t, "customer 301", err, "profile_customer")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.Id = unknownID }))
	wantViolation(t, "a client-chosen id", err, "id is set by the server")
	_, err = a.sites(operatorToken).CreateSite(ctx, req(&doelabv1.CreateSiteRequest{}))
	wantCode(t, "no site", err, connect.CodeInvalidArgument)

	// Meaning, refused by the service.
	wrongChecksum := repotest.NMI(t, 51)[:10] + string('0'+(repotest.NMI(t, 51)[10]-'0'+1)%10)
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.Nmi = wrongChecksum }))
	wantViolation(t, "an NMI with the wrong checksum", err, "checksum")
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.NodeId = unknownID }))
	wantCode(t, "an unknown node", err, connect.CodeFailedPrecondition)
	other := repotest.Seed(t, a.store, "LV20", 11)
	_, err = create(operatorToken, change(func(s *doelabv1.Site) { s.NodeId = other.HouseA.ID.String() }))
	wantCode(t, "a node of another feeder", err, connect.CodeFailedPrecondition)

	_, err = create("", change(func(*doelabv1.Site) {}))
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)
	_, err = create(a.tokens.DeviceToken(a.fixture.SiteA.NMI), change(func(*doelabv1.Site) {}))
	wantCode(t, "a device", err, connect.CodePermissionDenied)
}

func TestSiteUpdate(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	id := a.fixture.SiteB.ID.String()
	update := func(token string, s *doelabv1.Site, paths ...string) (*doelabv1.Site, error) {
		res, err := a.sites(token).UpdateSite(ctx, req(&doelabv1.UpdateSiteRequest{
			Site: s, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetSite(), nil
	}

	// One masked field changes; the other fields of the message are ignored.
	got, err := update(operatorToken, &doelabv1.Site{Id: id, ExportCapW: 5000, PvKw: 99, HasEv: true}, "export_cap_w")
	noErr(t, "update the export cap", err)
	if got.GetExportCapW() != 5000 || got.GetPvKw() != 0 || got.GetHasEv() {
		t.Errorf("after the cap update: %v", got)
	}
	// A battery arrives: the flag and the size change together.
	got, err = update(operatorToken, &doelabv1.Site{Id: id, HasBattery: true, BatteryKwh: repotest.Ptr(13.5)}, "has_battery", "battery_kwh")
	noErr(t, "add a battery", err)
	if !got.GetHasBattery() || got.GetBatteryKwh() != 13.5 || got.GetExportCapW() != 5000 {
		t.Errorf("after adding a battery: %v", got)
	}
	// Every mutable field at once.
	got, err = update(operatorToken, &doelabv1.Site{Id: id, PvKw: 6.6, InverterKva: 5, ImportCapW: 14000, HasEv: true},
		"pv_kw", "inverter_kva", "import_cap_w", "has_ev")
	noErr(t, "update the rest", err)
	if got.GetPvKw() != 6.6 || got.GetInverterKva() != 5 || got.GetImportCapW() != 14000 || !got.GetHasEv() || !got.GetHasBattery() {
		t.Errorf("after the rest: %v", got)
	}
	// The battery goes: clearing the size needs the flag to go with it.
	got, err = update(operatorToken, &doelabv1.Site{Id: id}, "has_battery", "battery_kwh")
	noErr(t, "remove the battery", err)
	if got.GetHasBattery() || got.BatteryKwh != nil {
		t.Errorf("after removing the battery: %v", got)
	}

	// A change that would leave the site inconsistent is refused whole.
	_, err = update(operatorToken, &doelabv1.Site{Id: id, HasBattery: true, BatteryKwh: repotest.Ptr(5.0)}, "has_battery")
	wantViolation(t, "a battery flag with no size", err, "battery_kwh")
	// An immutable field is rejected in the mask.
	_, err = update(operatorToken, &doelabv1.Site{Id: id, Phase: 3}, "phase")
	wantViolation(t, "phase in the mask", err, "update_mask")
	_, err = update(operatorToken, &doelabv1.Site{Id: id, Nmi: repotest.NMI(t, 77)}, "nmi")
	wantViolation(t, "nmi in the mask", err, "update_mask")
	_, err = update(operatorToken, &doelabv1.Site{Id: id})
	wantViolation(t, "an empty mask", err, "update_mask")
	_, err = update(operatorToken, &doelabv1.Site{Id: id, ExportCapW: -5}, "export_cap_w")
	wantViolation(t, "a negative cap", err, "export_cap_w")
	_, err = update(operatorToken, &doelabv1.Site{ExportCapW: 1}, "export_cap_w")
	wantViolation(t, "no id", err, "site.id")
	_, err = update(operatorToken, &doelabv1.Site{Id: unknownID, ExportCapW: 1}, "export_cap_w")
	wantCode(t, "an unknown site", err, connect.CodeNotFound)
	_, err = update("", &doelabv1.Site{Id: id, ExportCapW: 1}, "export_cap_w")
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)
}

func TestSiteDelete(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	id := a.fixture.SiteA.ID.String()

	_, err := a.sites("").DeleteSite(ctx, req(&doelabv1.DeleteSiteRequest{Id: id}))
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)

	_, err = a.sites(operatorToken).DeleteSite(ctx, req(&doelabv1.DeleteSiteRequest{Id: id}))
	noErr(t, "delete", err)
	_, err = a.sites("").GetSite(ctx, req(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Id{Id: id}}))
	wantCode(t, "get a deleted site", err, connect.CodeNotFound)
	_, err = a.sites(operatorToken).DeleteSite(ctx, req(&doelabv1.DeleteSiteRequest{Id: id}))
	wantCode(t, "delete it again", err, connect.CodeNotFound)
	list, err := a.sites("").ListSites(ctx, req(&doelabv1.ListSitesRequest{FeederId: a.fixture.Feeder.ID.String()}))
	noErr(t, "list", err)
	if len(list.Msg.GetSites()) != 1 {
		t.Errorf("list after delete = %d sites", len(list.Msg.GetSites()))
	}
	_, err = a.sites(operatorToken).DeleteSite(ctx, req(&doelabv1.DeleteSiteRequest{Id: "x"}))
	wantViolation(t, "malformed id", err, "id")
}

func TestSiteProfiles(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	noErr(t, "seed profiles", a.store.ReplaceSiteProfiles(repotest.Ctx(), a.fixture.SiteA.ID, repotest.Profiles(48, 100)))
	c := a.sites("")
	siteID := a.fixture.SiteA.ID.String()
	from, to := timestamppb.New(repotest.Day.Add(time.Hour)), timestamppb.New(repotest.Day.Add(3*time.Hour))

	page1, err := c.ListSiteProfiles(ctx, req(&doelabv1.ListSiteProfilesRequest{SiteId: siteID, From: from, To: to, PageSize: 3}))
	noErr(t, "page 1", err)
	rows := page1.Msg.GetSiteProfiles()
	if len(rows) != 3 || !rows[0].GetTs().AsTime().Equal(repotest.Day.Add(time.Hour)) || rows[0].GetLoadW() != 120 || rows[0].GetPvW() != 2 || rows[0].GetSiteId() != siteID {
		t.Fatalf("page 1 = %v", rows)
	}
	page2, err := c.ListSiteProfiles(ctx, req(&doelabv1.ListSiteProfilesRequest{SiteId: siteID, From: from, To: to, PageSize: 3, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page2.Msg.GetSiteProfiles()) != 1 || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("page 2 = %v", page2.Msg)
	}

	_, err = c.ListSiteProfiles(ctx, req(&doelabv1.ListSiteProfilesRequest{SiteId: siteID, From: to, To: from}))
	wantViolation(t, "an inverted range", err, "to must be after from")
	_, err = c.ListSiteProfiles(ctx, req(&doelabv1.ListSiteProfilesRequest{SiteId: siteID, From: from}))
	wantViolation(t, "no end", err, "to")
	_, err = c.ListSiteProfiles(ctx, req(&doelabv1.ListSiteProfilesRequest{SiteId: unknownID, From: from, To: to}))
	wantCode(t, "an unknown site", err, connect.CodeNotFound)
}
