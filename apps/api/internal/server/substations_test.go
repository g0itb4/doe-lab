package server_test

import (
	"testing"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/repotest"
)

func TestSubstations(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	var created []domain.Substation
	for _, code := range []string{"SUB-002", "SUB-001", "SUB-003"} {
		s, err := a.Store.CreateSubstation(repotest.Ctx(), repotest.NewSubstation(code))
		noErr(t, "create "+code, err)
		created = append(created, s)
	}
	c := a.substations("")

	byID, err := c.GetSubstation(ctx, req(&doelabv1.GetSubstationRequest{Key: &doelabv1.GetSubstationRequest_Id{Id: created[1].ID.String()}}))
	noErr(t, "get by id", err)
	s := byID.Msg.GetSubstation()
	if s.GetCode() != "SUB-001" || s.GetDnsp() != "Ausgrid" || s.GetState() != "NSW" ||
		s.GetLatitudeDeg() != -33.8524 || s.GetLongitudeDeg() != 151.0621 || s.GetCreatedAt() == nil {
		t.Errorf("substation = %v", s)
	}
	byCode, err := c.GetSubstation(ctx, req(&doelabv1.GetSubstationRequest{Key: &doelabv1.GetSubstationRequest_Code{Code: "SUB-001"}}))
	noErr(t, "get by code", err)
	if byCode.Msg.GetSubstation().GetId() != s.GetId() {
		t.Errorf("get by code returned another substation")
	}

	_, err = c.GetSubstation(ctx, req(&doelabv1.GetSubstationRequest{Key: &doelabv1.GetSubstationRequest_Id{Id: unknownID}}))
	wantCode(t, "unknown id", err, connect.CodeNotFound)
	_, err = c.GetSubstation(ctx, req(&doelabv1.GetSubstationRequest{Key: &doelabv1.GetSubstationRequest_Code{Code: "SUB-999"}}))
	wantCode(t, "unknown code", err, connect.CodeNotFound)
	_, err = c.GetSubstation(ctx, req(&doelabv1.GetSubstationRequest{}))
	wantViolation(t, "no key", err, "key")
	_, err = c.GetSubstation(ctx, req(&doelabv1.GetSubstationRequest{Key: &doelabv1.GetSubstationRequest_Id{Id: "not-a-uuid"}}))
	wantViolation(t, "malformed id", err, "id")

	page1, err := c.ListSubstations(ctx, req(&doelabv1.ListSubstationsRequest{PageSize: 2}))
	noErr(t, "page 1", err)
	if len(page1.Msg.GetSubstations()) != 2 || page1.Msg.GetSubstations()[0].GetCode() != "SUB-001" || page1.Msg.GetNextPageToken() == "" {
		t.Fatalf("page 1 = %v", page1.Msg)
	}
	page2, err := c.ListSubstations(ctx, req(&doelabv1.ListSubstationsRequest{PageSize: 2, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page2.Msg.GetSubstations()) != 1 || page2.Msg.GetSubstations()[0].GetCode() != "SUB-003" || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("page 2 = %v", page2.Msg)
	}
	_, err = c.ListSubstations(ctx, req(&doelabv1.ListSubstationsRequest{PageSize: 501}))
	wantViolation(t, "page size over the cap", err, "page_size")
	_, err = c.ListSubstations(ctx, req(&doelabv1.ListSubstationsRequest{PageToken: "not-a-token"}))
	wantCode(t, "malformed page token", err, connect.CodeInvalidArgument)

	// A feeder says which substation it hangs from; one that was never placed
	// says nothing.
	placed := repotest.NewFeeder("LV20")
	placed.SubstationID = &created[1].ID
	feeder, err := a.Store.CreateFeeder(repotest.Ctx(), placed)
	noErr(t, "create a feeder below the substation", err)
	got, err := a.feeders("").GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Id{Id: feeder.ID.String()}}))
	noErr(t, "get the placed feeder", err)
	if got.Msg.GetFeeder().GetSubstationId() != created[1].ID.String() {
		t.Errorf("substation_id = %q, want %s", got.Msg.GetFeeder().GetSubstationId(), created[1].ID)
	}
	unplaced, err := a.feeders("").GetFeeder(ctx, req(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Code{Code: "LV10"}}))
	noErr(t, "get the fixture feeder", err)
	if unplaced.Msg.GetFeeder().SubstationId != nil {
		t.Errorf("the fixture feeder has substation %q", unplaced.Msg.GetFeeder().GetSubstationId())
	}
}
