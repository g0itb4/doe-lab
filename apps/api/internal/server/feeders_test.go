package server_test

import (
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/repo/repotest"
)

func TestFeederGet(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	c := a.feeders("")

	byID, err := c.GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Id{Id: a.fixture.Feeder.ID.String()}}))
	noErr(t, "get by id", err)
	f := byID.Msg.GetFeeder()
	if f.GetCode() != "LV10" || f.GetTransformerKva() != 100 || f.GetSourceVoltageV() != 240 || f.GetTapPu() != 1 ||
		f.GetTimezone() != "Australia/Sydney" || f.GetCreatedAt() == nil || f.GetUpdatedAt() == nil {
		t.Errorf("feeder = %v", f)
	}
	byCode, err := c.GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Code{Code: "LV10"}}))
	noErr(t, "get by code", err)
	if byCode.Msg.GetFeeder().GetId() != f.GetId() {
		t.Errorf("get by code returned another feeder")
	}

	_, err = c.GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Id{Id: unknownID}}))
	wantCode(t, "unknown id", err, connect.CodeNotFound)
	_, err = c.GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Code{Code: "LV99"}}))
	wantCode(t, "unknown code", err, connect.CodeNotFound)
	_, err = c.GetFeeder(ctx, req(&doelabv1.GetFeederRequest{}))
	wantViolation(t, "no key", err, "key")
	_, err = c.GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Id{Id: "not-a-uuid"}}))
	wantViolation(t, "malformed id", err, "id")
	_, err = c.GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Code{Code: "lower case"}}))
	wantViolation(t, "malformed code", err, "code")
}

func TestFeederList(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	repotest.Seed(t, a.Store, "LV20", 11)
	repotest.Seed(t, a.Store, "LV30", 21)
	c := a.feeders("")

	page1, err := c.ListFeeders(ctx, req(&doelabv1.ListFeedersRequest{PageSize: 2}))
	noErr(t, "page 1", err)
	if len(page1.Msg.GetFeeders()) != 2 || page1.Msg.GetFeeders()[0].GetCode() != "LV10" || page1.Msg.GetNextPageToken() == "" {
		t.Fatalf("page 1 = %v", page1.Msg)
	}
	page2, err := c.ListFeeders(ctx, req(&doelabv1.ListFeedersRequest{PageSize: 2, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page2.Msg.GetFeeders()) != 1 || page2.Msg.GetFeeders()[0].GetCode() != "LV30" || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("page 2 = %v", page2.Msg)
	}
	// No page size means the default, which holds all three.
	all, err := c.ListFeeders(ctx, req(&doelabv1.ListFeedersRequest{}))
	noErr(t, "default page", err)
	if len(all.Msg.GetFeeders()) != 3 {
		t.Errorf("default page = %d feeders", len(all.Msg.GetFeeders()))
	}

	_, err = c.ListFeeders(ctx, req(&doelabv1.ListFeedersRequest{PageSize: 501}))
	wantViolation(t, "page size over the cap", err, "page_size")
	_, err = c.ListFeeders(ctx, req(&doelabv1.ListFeedersRequest{PageSize: -1}))
	wantViolation(t, "negative page size", err, "page_size")
	_, err = c.ListFeeders(ctx, req(&doelabv1.ListFeedersRequest{PageToken: "not-a-token"}))
	wantCode(t, "malformed page token", err, connect.CodeInvalidArgument)
}

func TestFeederUpdate(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	id := a.fixture.Feeder.ID.String()
	update := func(token string, feeder *doelabv1.Feeder, paths ...string) (*doelabv1.Feeder, error) {
		res, err := a.feeders(token).UpdateFeeder(ctx, req(&doelabv1.UpdateFeederRequest{
			Feeder: feeder, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetFeeder(), nil
	}

	// The masked field changes; the unmasked one does not, whatever the
	// message holds.
	got, err := update(operatorToken, &doelabv1.Feeder{Id: id, TapPu: 0.975, Name: "ignored"}, "tap_pu")
	noErr(t, "update the tap", err)
	if got.GetTapPu() != 0.975 || got.GetName() != "Feeder LV10" {
		t.Errorf("after the tap update: %v", got)
	}
	got, err = update(operatorToken, &doelabv1.Feeder{Id: id, Name: "Elm Street", TapPu: 1.1}, "name")
	noErr(t, "update the name", err)
	if got.GetName() != "Elm Street" || got.GetTapPu() != 0.975 {
		t.Errorf("after the name update: %v", got)
	}
	got, err = update(operatorToken, &doelabv1.Feeder{Id: id, Name: "Oak Street", TapPu: 1}, "name", "tap_pu")
	noErr(t, "update both", err)
	if got.GetName() != "Oak Street" || got.GetTapPu() != 1 {
		t.Errorf("after both: %v", got)
	}

	// An immutable field is rejected in the mask, not silently ignored.
	_, err = update(operatorToken, &doelabv1.Feeder{Id: id, TransformerKva: 1000}, "transformer_kva")
	wantViolation(t, "an immutable field in the mask", err, "update_mask")
	_, err = update(operatorToken, &doelabv1.Feeder{Id: id, Name: "x"})
	wantViolation(t, "an empty mask", err, "update_mask")
	_, err = update(operatorToken, &doelabv1.Feeder{Id: id, TapPu: 1.5}, "tap_pu")
	wantViolation(t, "a tap out of range", err, "tap_pu")
	_, err = update(operatorToken, &doelabv1.Feeder{Id: id, TapPu: 0}, "tap_pu")
	wantViolation(t, "a zero tap", err, "tap_pu")
	_, err = update(operatorToken, &doelabv1.Feeder{Id: id}, "name")
	wantViolation(t, "an empty name", err, "name")
	_, err = update(operatorToken, &doelabv1.Feeder{Name: "x"}, "name")
	wantViolation(t, "no id", err, "feeder.id")
	_, err = a.feeders(operatorToken).UpdateFeeder(ctx, req(&doelabv1.UpdateFeederRequest{}))
	wantCode(t, "no feeder", err, connect.CodeInvalidArgument)
	_, err = update(operatorToken, &doelabv1.Feeder{Id: unknownID, Name: "x"}, "name")
	wantCode(t, "an unknown feeder", err, connect.CodeNotFound)

	_, err = update("", &doelabv1.Feeder{Id: id, Name: "x"}, "name")
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)
	_, err = update(engineToken, &doelabv1.Feeder{Id: id, Name: "x"}, "name")
	wantCode(t, "the engine", err, connect.CodePermissionDenied)
}

func TestFeederNodes(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	c := a.feeders("")
	feederID := a.fixture.Feeder.ID.String()

	got, err := c.GetFeederNode(ctx, req(&doelabv1.GetFeederNodeRequest{Id: a.fixture.Mid.ID.String()}))
	noErr(t, "get", err)
	n := got.Msg.GetFeederNode()
	if n.GetName() != "mid" || n.GetParentNodeId() != a.fixture.Root.ID.String() || n.GroundROhm != nil || n.GetFeederId() != feederID {
		t.Errorf("mid = %v", n)
	}
	root, err := c.GetFeederNode(ctx, req(&doelabv1.GetFeederNodeRequest{Id: a.fixture.Root.ID.String()}))
	noErr(t, "get root", err)
	// The root has no parent: the field is absent, not empty.
	if root.Msg.GetFeederNode().ParentNodeId != nil || root.Msg.GetFeederNode().GetGroundROhm() != 0.6 {
		t.Errorf("root = %v", root.Msg.GetFeederNode())
	}
	_, err = c.GetFeederNode(ctx, req(&doelabv1.GetFeederNodeRequest{Id: unknownID}))
	wantCode(t, "unknown node", err, connect.CodeNotFound)
	_, err = c.GetFeederNode(ctx, req(&doelabv1.GetFeederNodeRequest{Id: "x"}))
	wantViolation(t, "malformed id", err, "id")

	page1, err := c.ListFeederNodes(ctx, req(&doelabv1.ListFeederNodesRequest{FeederId: feederID, PageSize: 3}))
	noErr(t, "page 1", err)
	page2, err := c.ListFeederNodes(ctx, req(&doelabv1.ListFeederNodesRequest{FeederId: feederID, PageSize: 3, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page1.Msg.GetFeederNodes()) != 3 || len(page2.Msg.GetFeederNodes()) != 1 || page2.Msg.GetFeederNodes()[0].GetName() != "root" || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("pages = %d and %d nodes", len(page1.Msg.GetFeederNodes()), len(page2.Msg.GetFeederNodes()))
	}
	_, err = c.ListFeederNodes(ctx, req(&doelabv1.ListFeederNodesRequest{FeederId: unknownID}))
	wantCode(t, "nodes of an unknown feeder", err, connect.CodeNotFound)
	_, err = c.ListFeederNodes(ctx, req(&doelabv1.ListFeederNodesRequest{}))
	wantViolation(t, "no feeder id", err, "feeder_id")
}

func TestFeederLines(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	c := a.feeders("")
	feederID := a.fixture.Feeder.ID.String()
	main, serviceB := a.fixture.Lines[0], a.fixture.Lines[2]

	got, err := c.GetFeederLine(ctx, req(&doelabv1.GetFeederLineRequest{Id: main.ID.String()}))
	noErr(t, "get", err)
	l := got.Msg.GetFeederLine()
	if l.GetName() != "main" || len(l.GetROhm()) != 16 || l.GetROhm()[0] != 0.04 || l.GetAmpacityA() != 300 ||
		l.GetAmpacitySource() != doelabv1.AmpacitySource_AMPACITY_SOURCE_ASSUMED || l.GetFromNodeId() != a.fixture.Root.ID.String() {
		t.Errorf("main = %v", l)
	}
	unrated, err := c.GetFeederLine(ctx, req(&doelabv1.GetFeederLineRequest{Id: serviceB.ID.String()}))
	noErr(t, "get an unrated line", err)
	if unrated.Msg.GetFeederLine().AmpacityA != nil || unrated.Msg.GetFeederLine().AmpacitySource != nil {
		t.Errorf("unrated = %v", unrated.Msg.GetFeederLine())
	}
	_, err = c.GetFeederLine(ctx, req(&doelabv1.GetFeederLineRequest{Id: unknownID}))
	wantCode(t, "unknown line", err, connect.CodeNotFound)

	page1, err := c.ListFeederLines(ctx, req(&doelabv1.ListFeederLinesRequest{FeederId: feederID, PageSize: 2}))
	noErr(t, "page 1", err)
	page2, err := c.ListFeederLines(ctx, req(&doelabv1.ListFeederLinesRequest{FeederId: feederID, PageSize: 2, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page1.Msg.GetFeederLines()) != 2 || len(page2.Msg.GetFeederLines()) != 1 || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("pages = %d and %d lines", len(page1.Msg.GetFeederLines()), len(page2.Msg.GetFeederLines()))
	}
	_, err = c.ListFeederLines(ctx, req(&doelabv1.ListFeederLinesRequest{FeederId: unknownID}))
	wantCode(t, "lines of an unknown feeder", err, connect.CodeNotFound)

	rate := func(token, id string, ampacity *float64, paths ...string) (*doelabv1.FeederLine, error) {
		res, err := a.feeders(token).UpdateFeederLine(ctx, req(&doelabv1.UpdateFeederLineRequest{
			FeederLine: &doelabv1.FeederLine{Id: id, AmpacityA: ampacity}, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetFeederLine(), nil
	}
	rated, err := rate(operatorToken, serviceB.ID.String(), repotest.Ptr(85.0), "ampacity_a")
	noErr(t, "rate", err)
	if rated.GetAmpacityA() != 85 || rated.GetAmpacitySource() != doelabv1.AmpacitySource_AMPACITY_SOURCE_OPERATOR {
		t.Errorf("rated = %v", rated)
	}
	cleared, err := rate(operatorToken, main.ID.String(), nil, "ampacity_a")
	noErr(t, "clear", err)
	if cleared.AmpacityA != nil || cleared.AmpacitySource != nil {
		t.Errorf("cleared = %v", cleared)
	}

	_, err = rate(operatorToken, main.ID.String(), repotest.Ptr(0.0), "ampacity_a")
	wantViolation(t, "a zero rating", err, "ampacity_a")
	_, err = rate(operatorToken, main.ID.String(), repotest.Ptr(100.0), "length_m")
	wantViolation(t, "an immutable field in the mask", err, "update_mask")
	_, err = rate(operatorToken, main.ID.String(), repotest.Ptr(100.0))
	wantViolation(t, "an empty mask", err, "update_mask")
	_, err = rate(operatorToken, "", repotest.Ptr(100.0), "ampacity_a")
	wantViolation(t, "no id", err, "feeder_line.id")
	_, err = rate(operatorToken, unknownID, repotest.Ptr(100.0), "ampacity_a")
	wantCode(t, "an unknown line", err, connect.CodeNotFound)
	_, err = rate("", main.ID.String(), repotest.Ptr(100.0), "ampacity_a")
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)
}
