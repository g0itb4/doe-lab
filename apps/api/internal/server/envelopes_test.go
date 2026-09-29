package server_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"go.uber.org/goleak"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/repotest"
)

// prepared is an API with a config and a running run that covers the profile
// day.
type prepared struct {
	*api
	configID string
	runID    string
}

func prepare(t *testing.T) prepared {
	t.Helper()
	a := newAPI(t)
	config, err := a.Store.CreateEnvelopeConfig(repotest.Ctx(), repotest.Config(a.fixture.Feeder.ID))
	noErr(t, "config", err)
	p := prepared{api: a, configID: config.ID.String()}
	p.runID = p.startRun(t, "run-0001").GetId()
	return p
}

func (p prepared) newRun(key string) *doelabv1.EnvelopeRun {
	return &doelabv1.EnvelopeRun{
		FeederId: p.fixture.Feeder.ID.String(), EnvelopeConfigId: p.configID, IdempotencyKey: key,
		HorizonFrom: timestamppb.New(repotest.Day), HorizonTo: timestamppb.New(repotest.Day.Add(24 * time.Hour)),
		EngineVersion: "test",
	}
}

func (p prepared) startRun(t *testing.T, key string) *doelabv1.EnvelopeRun {
	t.Helper()
	res, err := p.runs(engineToken).CreateEnvelopeRun(ctx, req(&doelabv1.CreateEnvelopeRunRequest{EnvelopeRun: p.newRun(key)}))
	noErr(t, "create run", err)
	return res.Msg.GetEnvelopeRun()
}

// envelope is the message for a site and the half hour that starts slot half
// hours into the profile day.
func envelope(siteID string, slot int, exportW float64) *doelabv1.Envelope {
	from := repotest.Day.Add(time.Duration(slot) * 30 * time.Minute)
	return &doelabv1.Envelope{
		SiteId: siteID, ValidFrom: timestamppb.New(from), ValidTo: timestamppb.New(from.Add(30 * time.Minute)),
		ExportLimitW: exportW, ImportLimitW: 7000,
		ExportBinding: doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_HIGH, ExportBindingElement: "Ld1_LOAD_A",
		ImportBinding: doelabv1.BindingConstraint_BINDING_CONSTRAINT_LINE, ImportBindingElement: "service-a",
	}
}

func (p prepared) publish(token, runID, key string, envelopes ...*doelabv1.Envelope) (*doelabv1.PublishEnvelopesResponse, error) {
	res, err := p.envelopes(token).PublishEnvelopes(ctx, req(&doelabv1.PublishEnvelopesRequest{
		EnvelopeRunId: runID, IdempotencyKey: key, Envelopes: envelopes,
	}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func TestEnvelopeRuns(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	feederID := p.fixture.Feeder.ID.String()

	run := p.startRun(t, "run-0002")
	if run.GetId() == "" || run.GetStatus() != doelabv1.RunStatus_RUN_STATUS_RUNNING || run.GetStartedAt() == nil ||
		run.CompletedAt != nil || run.DurationMs != nil || run.Error != nil || run.GetEngineVersion() != "test" {
		t.Errorf("run = %v", run)
	}
	// The same key again returns the same run.
	if again := p.startRun(t, "run-0002"); again.GetId() != run.GetId() {
		t.Errorf("the same key gave run %s, want %s", again.GetId(), run.GetId())
	}
	// The same key for another horizon is a mistake.
	other := p.newRun("run-0002")
	other.HorizonTo = timestamppb.New(repotest.Day.Add(48 * time.Hour))
	_, err := p.runs(engineToken).CreateEnvelopeRun(ctx, req(&doelabv1.CreateEnvelopeRunRequest{EnvelopeRun: other}))
	wantCode(t, "a key reused for another run", err, connect.CodeAlreadyExists)

	create := func(token string, change func(*doelabv1.EnvelopeRun)) error {
		r := p.newRun("run-9999")
		change(r)
		_, err := p.runs(token).CreateEnvelopeRun(ctx, req(&doelabv1.CreateEnvelopeRunRequest{EnvelopeRun: r}))
		return err
	}
	wantViolation(t, "a short key", create(engineToken, func(r *doelabv1.EnvelopeRun) { r.IdempotencyKey = "short" }), "idempotency_key")
	wantViolation(t, "no key", create(engineToken, func(r *doelabv1.EnvelopeRun) { r.IdempotencyKey = "" }), "required")
	wantViolation(t, "no horizon", create(engineToken, func(r *doelabv1.EnvelopeRun) { r.HorizonTo = nil }), "required")
	wantViolation(t, "an inverted horizon", create(engineToken, func(r *doelabv1.EnvelopeRun) { r.HorizonTo = timestamppb.New(repotest.Day.Add(-time.Hour)) }), "horizon_to must be after horizon_from")
	wantViolation(t, "a client-chosen status", create(engineToken, func(r *doelabv1.EnvelopeRun) { r.Status = doelabv1.RunStatus_RUN_STATUS_COMPLETED }), "set by the server")
	wantCode(t, "an unknown config", create(engineToken, func(r *doelabv1.EnvelopeRun) { r.EnvelopeConfigId = unknownID }), connect.CodeFailedPrecondition)
	otherFeeder := repotest.Seed(t, p.Store, "LV20", 11)
	wantCode(t, "another feeder's config", create(engineToken, func(r *doelabv1.EnvelopeRun) { r.FeederId = otherFeeder.Feeder.ID.String() }), connect.CodeFailedPrecondition)
	wantCode(t, "anonymous", create("", func(*doelabv1.EnvelopeRun) {}), connect.CodeUnauthenticated)
	wantCode(t, "the operator", create(operatorToken, func(*doelabv1.EnvelopeRun) {}), connect.CodePermissionDenied)
	_, err = p.runs(engineToken).CreateEnvelopeRun(ctx, req(&doelabv1.CreateEnvelopeRunRequest{}))
	wantCode(t, "no run", err, connect.CodeInvalidArgument)

	// Complete.
	complete := func(token string, r *doelabv1.CompleteEnvelopeRunRequest) (*doelabv1.EnvelopeRun, error) {
		res, err := p.runs(token).CompleteEnvelopeRun(ctx, req(r))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetEnvelopeRun(), nil
	}
	done, err := complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{
		Id: run.GetId(), Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED, DurationMs: 241, SiteCount: 2, IntervalCount: 48,
	})
	noErr(t, "complete", err)
	if done.GetStatus() != doelabv1.RunStatus_RUN_STATUS_COMPLETED || done.GetCompletedAt() == nil || done.GetDurationMs() != 241 ||
		done.GetSiteCount() != 2 || done.GetIntervalCount() != 48 {
		t.Errorf("completed = %v", done)
	}
	// Completing again with the same outcome changes nothing.
	again, err := complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{Id: run.GetId(), Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED, DurationMs: 999})
	noErr(t, "complete again", err)
	if again.GetDurationMs() != 241 {
		t.Errorf("a second completion changed the run: %v", again)
	}
	reason := "no convergence at 12:30"
	_, err = complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{Id: run.GetId(), Status: doelabv1.RunStatus_RUN_STATUS_FAILED, Error: &reason})
	wantCode(t, "failing a completed run", err, connect.CodeFailedPrecondition)

	failing := p.startRun(t, "run-0003")
	failed, err := complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{Id: failing.GetId(), Status: doelabv1.RunStatus_RUN_STATUS_FAILED, DurationMs: 5, Error: &reason})
	noErr(t, "fail", err)
	if failed.GetStatus() != doelabv1.RunStatus_RUN_STATUS_FAILED || failed.GetError() != reason {
		t.Errorf("failed = %v", failed)
	}
	_, err = complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{Id: p.runID, Status: doelabv1.RunStatus_RUN_STATUS_FAILED})
	wantViolation(t, "failed with no error", err, "error must be set exactly when status is FAILED")
	_, err = complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{Id: p.runID, Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED, Error: &reason})
	wantViolation(t, "completed with an error", err, "error must be set exactly when status is FAILED")
	_, err = complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{Id: p.runID, Status: doelabv1.RunStatus_RUN_STATUS_RUNNING})
	wantViolation(t, "completing as running", err, "status")
	_, err = complete(engineToken, &doelabv1.CompleteEnvelopeRunRequest{Id: unknownID, Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED})
	wantCode(t, "an unknown run", err, connect.CodeNotFound)
	_, err = complete("", &doelabv1.CompleteEnvelopeRunRequest{Id: p.runID, Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED})
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)

	// Get and List.
	got, err := p.runs("").GetEnvelopeRun(ctx, req(&doelabv1.GetEnvelopeRunRequest{Id: run.GetId()}))
	noErr(t, "get", err)
	if got.Msg.GetEnvelopeRun().GetStatus() != doelabv1.RunStatus_RUN_STATUS_COMPLETED {
		t.Errorf("get = %v", got.Msg.GetEnvelopeRun())
	}
	_, err = p.runs("").GetEnvelopeRun(ctx, req(&doelabv1.GetEnvelopeRunRequest{Id: unknownID}))
	wantCode(t, "get an unknown run", err, connect.CodeNotFound)

	page1, err := p.runs("").ListEnvelopeRuns(ctx, req(&doelabv1.ListEnvelopeRunsRequest{FeederId: feederID, PageSize: 2}))
	noErr(t, "page 1", err)
	page2, err := p.runs("").ListEnvelopeRuns(ctx, req(&doelabv1.ListEnvelopeRunsRequest{FeederId: feederID, PageSize: 2, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page1.Msg.GetEnvelopeRuns()) != 2 || page1.Msg.GetEnvelopeRuns()[0].GetId() != failing.GetId() ||
		len(page2.Msg.GetEnvelopeRuns()) != 1 || page2.Msg.GetEnvelopeRuns()[0].GetId() != p.runID || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("pages = %d and %d runs", len(page1.Msg.GetEnvelopeRuns()), len(page2.Msg.GetEnvelopeRuns()))
	}
	running, err := p.runs("").ListEnvelopeRuns(ctx, req(&doelabv1.ListEnvelopeRunsRequest{FeederId: feederID, Status: repotest.Ptr(doelabv1.RunStatus_RUN_STATUS_RUNNING)}))
	noErr(t, "list running", err)
	if len(running.Msg.GetEnvelopeRuns()) != 1 || running.Msg.GetEnvelopeRuns()[0].GetId() != p.runID {
		t.Errorf("running = %d runs", len(running.Msg.GetEnvelopeRuns()))
	}
	_, err = p.runs("").ListEnvelopeRuns(ctx, req(&doelabv1.ListEnvelopeRunsRequest{FeederId: unknownID}))
	wantCode(t, "list for an unknown feeder", err, connect.CodeNotFound)
	_, err = p.runs("").ListEnvelopeRuns(ctx, req(&doelabv1.ListEnvelopeRunsRequest{FeederId: feederID, Status: repotest.Ptr(doelabv1.RunStatus_RUN_STATUS_UNSPECIFIED)}))
	wantViolation(t, "filter on the unspecified status", err, "status")
}

func TestPublishEnvelopes(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	siteA, siteB := p.fixture.SiteA.ID.String(), p.fixture.SiteB.ID.String()

	first, err := p.publish(engineToken, p.runID, "batch-0001",
		envelope(siteA, 0, 1000), envelope(siteA, 1, 1100), envelope(siteB, 0, 2000))
	noErr(t, "publish", err)
	if first.GetPublished() != 3 || first.GetSuperseded() != 0 || first.GetReplayed() {
		t.Errorf("first publish = %v", first)
	}

	// The same batch under the same key writes nothing.
	replay, err := p.publish(engineToken, p.runID, "batch-0001",
		envelope(siteA, 0, 1000), envelope(siteA, 1, 1100), envelope(siteB, 0, 2000))
	noErr(t, "replay", err)
	if !replay.GetReplayed() || replay.GetPublished() != 0 || replay.GetSuperseded() != 0 {
		t.Errorf("replay = %v", replay)
	}
	all, err := p.envelopes("").ListEnvelopes(ctx, req(&doelabv1.ListEnvelopesRequest{
		SiteId: siteA, From: timestamppb.New(repotest.Day), To: timestamppb.New(repotest.Day.Add(24 * time.Hour)), IncludeSuperseded: true,
	}))
	noErr(t, "list", err)
	if len(all.Msg.GetEnvelopes()) != 2 {
		t.Errorf("after a replay site A has %d envelopes, want 2", len(all.Msg.GetEnvelopes()))
	}
	// The same key with another batch is refused.
	_, err = p.publish(engineToken, p.runID, "batch-0001", envelope(siteA, 0, 9999))
	wantCode(t, "a key reused for another batch", err, connect.CodeAlreadyExists)

	// A second batch supersedes the interval it overlaps, and only that one.
	second, err := p.publish(engineToken, p.runID, "batch-0002", envelope(siteA, 1, 1150), envelope(siteA, 2, 1250))
	noErr(t, "second publish", err)
	if second.GetPublished() != 2 || second.GetSuperseded() != 1 {
		t.Errorf("second publish = %v", second)
	}
	active, err := p.envelopes("").ListEnvelopes(ctx, req(&doelabv1.ListEnvelopesRequest{
		SiteId: siteA, From: timestamppb.New(repotest.Day), To: timestamppb.New(repotest.Day.Add(24 * time.Hour)),
	}))
	noErr(t, "list active", err)
	withHistory, err := p.envelopes("").ListEnvelopes(ctx, req(&doelabv1.ListEnvelopesRequest{
		SiteId: siteA, From: timestamppb.New(repotest.Day), To: timestamppb.New(repotest.Day.Add(24 * time.Hour)), IncludeSuperseded: true, PageSize: 3,
	}))
	noErr(t, "list with history", err)
	if len(active.Msg.GetEnvelopes()) != 3 || active.Msg.GetEnvelopes()[1].GetExportLimitW() != 1150 || len(withHistory.Msg.GetEnvelopes()) != 3 || withHistory.Msg.GetNextPageToken() == "" {
		t.Errorf("%d active, %d of the history on page 1", len(active.Msg.GetEnvelopes()), len(withHistory.Msg.GetEnvelopes()))
	}
	// The superseded row is kept, and says when it was replaced.
	old := withHistory.Msg.GetEnvelopes()[1]
	if old.GetExportLimitW() != 1100 || old.GetSupersededAt() == nil {
		t.Errorf("the superseded envelope = %v", old)
	}
	e := active.Msg.GetEnvelopes()[0]
	if e.GetSource() != doelabv1.EnvelopeSource_ENVELOPE_SOURCE_ENGINE || e.GetEnvelopeRunId() != p.runID || e.BackstopEventId != nil ||
		e.GetExportBinding() != doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_HIGH || e.GetExportBindingElement() != "Ld1_LOAD_A" ||
		e.GetImportBinding() != doelabv1.BindingConstraint_BINDING_CONSTRAINT_LINE || e.GetImportLimitW() != 7000 || e.SupersededAt != nil || e.GetId() == "" {
		t.Errorf("published envelope = %v", e)
	}
	// The run counts what it published.
	run, err := p.runs("").GetEnvelopeRun(ctx, req(&doelabv1.GetEnvelopeRunRequest{Id: p.runID}))
	noErr(t, "get run", err)
	if run.Msg.GetEnvelopeRun().GetEnvelopeCount() != 5 {
		t.Errorf("the run counts %d envelopes, want 5", run.Msg.GetEnvelopeRun().GetEnvelopeCount())
	}

	// What a batch may not be. Each attempt has a key of its own.
	key := 100
	refuse := func(what string, want connect.Code, runID string, envelopes ...*doelabv1.Envelope) {
		t.Helper()
		key++
		_, err := p.publish(engineToken, runID, fmt.Sprintf("batch-refused-%03d", key), envelopes...)
		wantCode(t, what, err, want)
		// Refused by the service, not by a malformed request.
		var ce *connect.Error
		if errors.As(err, &ce) && strings.Contains(ce.Message(), "validation error") {
			t.Errorf("%s: refused by validation, not by the service: %v", what, err)
		}
	}
	shifted := envelope(siteA, 5, 1)
	shifted.ValidFrom, shifted.ValidTo = timestamppb.New(repotest.Day.Add(155*time.Minute)), timestamppb.New(repotest.Day.Add(185*time.Minute))
	refuse("off the 30-minute grid", connect.CodeInvalidArgument, p.runID, shifted)
	long := envelope(siteA, 5, 1)
	long.ValidTo = timestamppb.New(repotest.Day.Add(210 * time.Minute))
	refuse("an hour-long interval under a 30-minute config", connect.CodeInvalidArgument, p.runID, long)
	refuse("outside the run's horizon", connect.CodeInvalidArgument, p.runID, envelope(siteA, 48, 1))
	refuse("twice in one batch", connect.CodeInvalidArgument, p.runID, envelope(siteA, 6, 1), envelope(siteA, 6, 2))
	other := repotest.Seed(t, p.Store, "LV20", 11)
	refuse("site of another feeder", connect.CodeFailedPrecondition, p.runID, envelope(other.SiteA.ID.String(), 6, 1))
	refuse("unknown site", connect.CodeFailedPrecondition, p.runID, envelope(unknownID, 6, 1))
	refuse("unknown run", connect.CodeFailedPrecondition, unknownID, envelope(siteA, 6, 1))
	// Nothing of a refused batch was written.
	at := timestamppb.New(repotest.Day.Add(3 * time.Hour))
	none, err := p.envelopes("").GetCurrentEnvelope(ctx, req(&doelabv1.GetCurrentEnvelopeRequest{Site: &doelabv1.GetCurrentEnvelopeRequest_SiteId{SiteId: siteA}, At: at}))
	noErr(t, "current", err)
	if none.Msg.Envelope != nil {
		t.Errorf("a refused batch left an envelope behind: %v", none.Msg.GetEnvelope())
	}

	// Shape, refused before the service.
	negative := envelope(siteA, 7, -1)
	_, err = p.publish(engineToken, p.runID, "batch-negative", negative)
	wantViolation(t, "a negative limit", err, "export_limit_w")
	inverted := envelope(siteA, 7, 1)
	inverted.ValidTo = timestamppb.New(repotest.Day)
	_, err = p.publish(engineToken, p.runID, "batch-inverted", inverted)
	wantViolation(t, "an inverted interval", err, "valid_to must be after valid_from")
	withID := envelope(siteA, 7, 1)
	withID.Id = unknownID
	_, err = p.publish(engineToken, p.runID, "batch-with-id", withID)
	wantViolation(t, "a client-chosen id", err, "must not set id")
	withSource := envelope(siteA, 7, 1)
	withSource.Source = doelabv1.EnvelopeSource_ENVELOPE_SOURCE_BACKSTOP
	_, err = p.publish(engineToken, p.runID, "batch-with-source", withSource)
	wantViolation(t, "a client-chosen source", err, "must not set id, source")
	_, err = p.publish(engineToken, p.runID, "batch-empty")
	wantViolation(t, "an empty batch", err, "envelopes")
	_, err = p.publish(engineToken, p.runID, "short", envelope(siteA, 7, 1))
	wantViolation(t, "a short key", err, "idempotency_key")
	_, err = p.publish("", p.runID, "batch-anonymous", envelope(siteA, 7, 1))
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)
	_, err = p.publish(operatorToken, p.runID, "batch-operator", envelope(siteA, 7, 1))
	wantCode(t, "the operator", err, connect.CodePermissionDenied)

	// A run that has finished takes no more envelopes.
	_, err = p.runs(engineToken).CompleteEnvelopeRun(ctx, req(&doelabv1.CompleteEnvelopeRunRequest{Id: p.runID, Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED}))
	noErr(t, "complete", err)
	_, err = p.publish(engineToken, p.runID, "batch-too-late", envelope(siteA, 8, 1))
	wantCode(t, "publishing to a finished run", err, connect.CodeFailedPrecondition)
}

func TestCurrentEnvelope(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	siteA := p.fixture.SiteA.ID.String()
	_, err := p.publish(engineToken, p.runID, "batch-0001", envelope(siteA, 0, 1000), envelope(siteA, 1, 1100))
	noErr(t, "publish", err)

	current := func(r *doelabv1.GetCurrentEnvelopeRequest) (*doelabv1.Envelope, error) {
		res, err := p.envelopes("").GetCurrentEnvelope(ctx, req(r))
		if err != nil {
			return nil, err
		}
		return res.Msg.Envelope, nil
	}
	bySite := func(at *timestamppb.Timestamp) *doelabv1.GetCurrentEnvelopeRequest {
		return &doelabv1.GetCurrentEnvelopeRequest{Site: &doelabv1.GetCurrentEnvelopeRequest_SiteId{SiteId: siteA}, At: at}
	}

	// With no instant: now, by the feeder clock, which stands at 00:00.
	now, err := current(bySite(nil))
	noErr(t, "now", err)
	if now.GetExportLimitW() != 1000 {
		t.Errorf("now = %v", now)
	}
	p.Clock.Advance(40 * time.Minute)
	later, err := current(bySite(nil))
	noErr(t, "40 minutes later", err)
	if later.GetExportLimitW() != 1100 {
		t.Errorf("40 minutes later = %v", later)
	}
	// By NMI, at a given instant.
	byNMI, err := current(&doelabv1.GetCurrentEnvelopeRequest{
		Site: &doelabv1.GetCurrentEnvelopeRequest_Nmi{Nmi: p.fixture.SiteA.NMI}, At: timestamppb.New(repotest.Day.Add(10 * time.Minute)),
	})
	noErr(t, "by NMI", err)
	if byNMI.GetExportLimitW() != 1000 {
		t.Errorf("by NMI = %v", byNMI)
	}
	// No envelope in force is an answer, not an error.
	none, err := current(bySite(timestamppb.New(repotest.Day.Add(5 * time.Hour))))
	noErr(t, "beyond the horizon", err)
	if none != nil {
		t.Errorf("beyond the horizon = %v", none)
	}
	never, err := current(&doelabv1.GetCurrentEnvelopeRequest{Site: &doelabv1.GetCurrentEnvelopeRequest_SiteId{SiteId: p.fixture.SiteB.ID.String()}})
	noErr(t, "a site with no envelopes", err)
	if never != nil {
		t.Errorf("a site with no envelopes = %v", never)
	}

	_, err = current(&doelabv1.GetCurrentEnvelopeRequest{Site: &doelabv1.GetCurrentEnvelopeRequest_SiteId{SiteId: unknownID}})
	wantCode(t, "an unknown site", err, connect.CodeNotFound)
	_, err = current(&doelabv1.GetCurrentEnvelopeRequest{Site: &doelabv1.GetCurrentEnvelopeRequest_Nmi{Nmi: repotest.NMI(t, 999)}})
	wantCode(t, "an unknown NMI", err, connect.CodeNotFound)
	_, err = current(&doelabv1.GetCurrentEnvelopeRequest{})
	wantViolation(t, "no site", err, "site")

	_, err = p.envelopes("").ListEnvelopes(ctx, req(&doelabv1.ListEnvelopesRequest{SiteId: unknownID, From: timestamppb.New(repotest.Day), To: timestamppb.New(repotest.Day.Add(time.Hour))}))
	wantCode(t, "list for an unknown site", err, connect.CodeNotFound)
	_, err = p.envelopes("").ListEnvelopes(ctx, req(&doelabv1.ListEnvelopesRequest{SiteId: siteA, From: timestamppb.New(repotest.Day.Add(time.Hour)), To: timestamppb.New(repotest.Day)}))
	wantViolation(t, "an inverted range", err, "to must be after from")
}

// The JSON form of an envelope carries the CSIP-AUS names.
func TestEnvelopeJSONUsesCSIPNames(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	_, err := p.publish(engineToken, p.runID, "batch-0001", envelope(p.fixture.SiteA.ID.String(), 0, 1500))
	noErr(t, "publish", err)
	status, body := p.post(t, "/doelab.v1.EnvelopeService/GetCurrentEnvelope", `{"nmi":"`+p.fixture.SiteA.NMI+`"}`)
	if status != 200 || !contains(body, `"opModExpLimW":1500`) || !contains(body, `"opModImpLimW":7000`) {
		t.Errorf("GetCurrentEnvelope = %d %s", status, body)
	}
}

// subscription reads a SubscribeEnvelopes stream on a goroutine, so a test can
// wait for a message with a timeout.
type subscription struct {
	messages chan *doelabv1.SubscribeEnvelopesResponse
	done     chan error
	cancel   context.CancelFunc
}

func (p prepared) subscribe(t *testing.T, token, nmi string) *subscription {
	t.Helper()
	streamCtx, cancel := context.WithCancel(context.Background())
	s := &subscription{messages: make(chan *doelabv1.SubscribeEnvelopesResponse, 64), done: make(chan error, 1), cancel: cancel}
	stream, err := p.envelopes(token).SubscribeEnvelopes(streamCtx, req(&doelabv1.SubscribeEnvelopesRequest{Nmi: nmi}))
	if err != nil {
		cancel()
		s.done <- err
		return s
	}
	go func() {
		for stream.Receive() {
			s.messages <- stream.Msg()
		}
		s.done <- stream.Err()
		_ = stream.Close()
	}()
	t.Cleanup(cancel)
	return s
}

// next returns the next message that is not a keepalive.
func (s *subscription) next(t *testing.T) *doelabv1.SubscribeEnvelopesResponse {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-s.messages:
			if !m.GetKeepalive() {
				return m
			}
		case err := <-s.done:
			t.Fatalf("the stream ended: %v", err)
		case <-deadline:
			t.Fatal("no message within 5 s")
		}
	}
}

// keepalive returns the next keepalive message.
func (s *subscription) keepalive(t *testing.T) *doelabv1.SubscribeEnvelopesResponse {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-s.messages:
			if m.GetKeepalive() {
				return m
			}
		case err := <-s.done:
			t.Fatalf("the stream ended: %v", err)
		case <-deadline:
			t.Fatal("no keepalive within 5 s")
		}
	}
}

func (s *subscription) end(t *testing.T) error {
	t.Helper()
	select {
	case err := <-s.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end within 5 s")
		return nil
	}
}

func TestSubscribeEnvelopes(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	siteA := p.fixture.SiteA.ID.String()
	nmi := p.fixture.SiteA.NMI
	token := p.Tokens.DeviceToken(nmi)

	// Before anything is published: the first message says "no envelope".
	sub := p.subscribe(t, token, nmi)
	first := sub.next(t)
	if first.Envelope != nil || !first.GetServerTime().AsTime().Equal(repotest.Day) {
		t.Fatalf("first message = %v", first)
	}

	// A publish reaches the open stream.
	_, err := p.publish(engineToken, p.runID, "batch-0001", envelope(siteA, 0, 1000), envelope(siteA, 1, 1100))
	noErr(t, "publish", err)
	if got := sub.next(t); got.GetEnvelope().GetExportLimitW() != 1000 {
		t.Fatalf("after the publish: %v", got)
	}

	// With nothing new, the stream repeats itself as a keepalive.
	if got := sub.keepalive(t); got.GetEnvelope().GetExportLimitW() != 1000 {
		t.Errorf("keepalive = %v", got)
	}

	// A newer envelope for the interval in force replaces it at once.
	_, err = p.publish(engineToken, p.runID, "batch-0002", envelope(siteA, 0, 1050))
	noErr(t, "republish", err)
	if got := sub.next(t); got.GetEnvelope().GetExportLimitW() != 1050 {
		t.Fatalf("after the republish: %v", got)
	}

	// The interval rolls over: the next envelope arrives with no publish.
	p.Clock.Advance(31 * time.Minute)
	rolled := sub.next(t)
	if rolled.GetEnvelope().GetExportLimitW() != 1100 || !rolled.GetServerTime().AsTime().Equal(repotest.Day.Add(31*time.Minute)) {
		t.Fatalf("after the interval rolled: %v", rolled)
	}

	// Past the last envelope: "no envelope" again.
	p.Clock.Advance(time.Hour)
	if got := sub.next(t); got.Envelope != nil {
		t.Fatalf("past the horizon: %v", got)
	}

	// The device leaves, and comes back: the envelope in force is its first
	// message again.
	sub.cancel()
	if err := sub.end(t); err != nil && connect.CodeOf(err) != connect.CodeCanceled {
		t.Errorf("the stream ended with %v", err)
	}
	waitFor(t, "the subscription to be released", func() bool { return p.Bus.Subscribers() == 0 })

	_, err = p.publish(engineToken, p.runID, "batch-0003", envelope(siteA, 3, 1300))
	noErr(t, "publish while away", err)
	back := p.subscribe(t, token, nmi)
	if got := back.next(t); got.GetEnvelope().GetExportLimitW() != 1300 {
		t.Fatalf("on resubscribe: %v", got)
	}

	// A publish for another site says nothing to this one.
	_, err = p.publish(engineToken, p.runID, "batch-0004", envelope(p.fixture.SiteB.ID.String(), 3, 2000))
	noErr(t, "publish for site B", err)
	select {
	case m := <-back.messages:
		if !m.GetKeepalive() {
			t.Errorf("site A's stream got %v after a publish for site B", m)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSubscribeNeedsTheSitesOwnToken(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	nmiA, nmiB := p.fixture.SiteA.NMI, p.fixture.SiteB.NMI

	refused := func(what, token, nmi string, want connect.Code) {
		t.Helper()
		sub := p.subscribe(t, token, nmi)
		wantCode(t, what, sub.end(t), want)
	}
	refused("no token", "", nmiA, connect.CodeUnauthenticated)
	refused("a wrong token", "nonsense", nmiA, connect.CodeUnauthenticated)
	refused("the operator", operatorToken, nmiA, connect.CodePermissionDenied)
	refused("the engine", engineToken, nmiA, connect.CodePermissionDenied)
	// A device token is good for its own site only.
	refused("the token of another site", p.Tokens.DeviceToken(nmiB), nmiA, connect.CodePermissionDenied)
	refused("a malformed NMI", p.Tokens.DeviceToken(nmiA), "nope", connect.CodeInvalidArgument)
	// A valid token for an NMI that is not a site.
	ghost := repotest.NMI(t, 4242)
	refused("an NMI with no site", p.Tokens.DeviceToken(ghost), ghost, connect.CodeNotFound)

	if n := p.Bus.Subscribers(); n != 0 {
		t.Errorf("%d subscriptions are open after every refusal", n)
	}
}

// Not parallel: it runs before the parallel tests start, so every goroutine
// alive at the end belongs to it.
func TestSubscribeLeavesNoGoroutineBehind(t *testing.T) {
	before := goleak.IgnoreCurrent()
	p := prepare(t)
	nmi := p.fixture.SiteA.NMI

	for range 5 {
		sub := p.subscribe(t, p.Tokens.DeviceToken(nmi), nmi)
		sub.next(t)
		sub.cancel()
		if err := sub.end(t); err != nil && connect.CodeOf(err) != connect.CodeCanceled && !errors.Is(err, context.Canceled) {
			t.Fatalf("the stream ended with %v", err)
		}
	}
	waitFor(t, "the subscriptions to be released", func() bool { return p.Bus.Subscribers() == 0 })
	p.Close()
	goleak.VerifyNone(t, before)
}

// A subscription never ends by itself, so a server that waited for it would
// never stop. When the API begins to shut down, its streams end cleanly, and
// a client reconnects to whatever replaces it.
func TestShutdownEndsStreams(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	nmi := p.fixture.SiteA.NMI
	sub := p.subscribe(t, p.Tokens.DeviceToken(nmi), nmi)
	sub.next(t)

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watch, err := p.telemetry("").WatchFleet(watchCtx, req(&doelabv1.WatchFleetRequest{FeederId: p.fixture.Feeder.ID.String()}))
	noErr(t, "watch", err)
	if !watch.Receive() {
		t.Fatalf("no summary: %v", watch.Err())
	}

	// A device with a stream of readings open, and silent for now.
	solar := p.device(t, p.fixture.SiteA, domain.DERSolar)
	readings := p.telemetry(p.Tokens.DeviceToken(nmi)).IngestReadings(ctx)
	noErr(t, "send", readings.Send(&doelabv1.IngestReadingsRequest{Nmi: nmi, Readings: []*doelabv1.Reading{reading(solar, 0, 100)}}))
	waitFor(t, "the reading to be stored", func() bool {
		res, err := p.telemetry("").ListReadings(ctx, req(&doelabv1.ListReadingsRequest{DeviceId: solar, From: day(0), To: day(60)}))
		return err == nil && len(res.Msg.GetReadings()) == 1
	})

	p.Stop()
	if err := sub.end(t); err != nil {
		t.Errorf("the subscription ended with %v, want a clean end", err)
	}
	// The stream of readings is answered with what was stored, without
	// waiting for the device to speak again.
	stored, err := readings.CloseAndReceive()
	if err != nil || stored.Msg.GetAccepted() != 1 {
		t.Errorf("the stream of readings ended with %v, %v; want 1 accepted", stored, err)
	}
	for watch.Receive() {
		// Summaries that were on their way.
	}
	if err := watch.Err(); err != nil {
		t.Errorf("the watch ended with %v, want a clean end", err)
	}
	waitFor(t, "the subscription to be released", func() bool { return p.Bus.Subscribers() == 0 })

	// A unary call is still answered: the listener closes later.
	if _, err := p.envelopes("").GetCurrentEnvelope(ctx, req(&doelabv1.GetCurrentEnvelopeRequest{
		Site: &doelabv1.GetCurrentEnvelopeRequest_Nmi{Nmi: nmi},
	})); err != nil {
		t.Errorf("a unary call after the stop: %v", err)
	}
}

func TestClock(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	p.Clock.Advance(90 * time.Minute)
	status, body := p.post(t, "/doelab.v1.ClockService/GetClock", `{}`)
	if status != 200 || !contains(body, `"speed":60`) || !contains(body, `"now":"2012-10-01T01:30:00Z"`) || !contains(body, `"anchor":"2012-10-01T00:00:00Z"`) {
		t.Errorf("GetClock = %d %s", status, body)
	}
}

func TestFeederForecast(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	feederID := p.fixture.Feeder.ID.String()
	forecast := func(from, to time.Time) (*doelabv1.GetFeederForecastResponse, error) {
		res, err := p.feeders("").GetFeederForecast(ctx, req(&doelabv1.GetFeederForecastRequest{
			FeederId: feederID, From: timestamppb.New(from), To: timestamppb.New(to),
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	// No profiles yet: the forecast says what is missing.
	_, err := forecast(day, day.Add(time.Hour))
	wantCode(t, "a feeder with no profiles", err, connect.CodeFailedPrecondition)

	// A profile year in which the load of each half hour is its index in the
	// year, so a point says which half hour it was read from.
	zone, _ := time.LoadLocation("Australia/Sydney")
	start := time.Date(2010, 7, 1, 0, 0, 0, 0, zone)
	for i, site := range []struct {
		id   string
		base float64
	}{{p.fixture.SiteA.ID.String(), 0}, {p.fixture.SiteB.ID.String(), 100000}} {
		rows := repotest.Profiles(17520, 0)
		for k := range rows {
			rows[k].TS = start.Add(time.Duration(k) * 30 * time.Minute)
			rows[k].LoadW, rows[k].ControlledLoadW, rows[k].PVW = site.base+float64(k), 0.5, float64(k)/10
		}
		id := p.fixture.SiteA.ID
		if i == 1 {
			id = p.fixture.SiteB.ID
		}
		noErr(t, "profiles", p.Store.ReplaceSiteProfiles(repotest.Ctx(), id, rows))
	}

	// 1 October 2026 00:00 UTC is 10:00 in Sydney: the same clock time on
	// 1 October 2010 is half hour 92·48 + 20 of the profile year, less the
	// hour that... no hour is lost before 3 October, so it is exactly that.
	got, err := forecast(day, day.Add(time.Hour))
	noErr(t, "forecast", err)
	if len(got.GetPoints()) != 4 {
		t.Fatalf("%d points, want 2 half hours × 2 sites", len(got.GetPoints()))
	}
	index := 92*48 + 20
	first, second, third := got.GetPoints()[0], got.GetPoints()[1], got.GetPoints()[2]
	if first.GetSiteId() != p.fixture.SiteA.ID.String() || !first.GetTs().AsTime().Equal(day) ||
		first.GetLoadW() != float64(index)+0.5 || first.GetPvW() != float64(index)/10 {
		t.Errorf("first point = %v, want half hour %d of the profile year", first, index)
	}
	if second.GetSiteId() != p.fixture.SiteB.ID.String() || second.GetLoadW() != 100000+float64(index)+0.5 {
		t.Errorf("second point = %v", second)
	}
	if !third.GetTs().AsTime().Equal(day.Add(30*time.Minute)) || third.GetLoadW() != float64(index+1)+0.5 {
		t.Errorf("third point = %v", third)
	}

	// A range that crosses 1 July wraps round the profile year.
	wrap := time.Date(2027, 6, 30, 13, 0, 0, 0, time.UTC) // 23:00 on 30 June in Sydney
	got, err = forecast(wrap, wrap.Add(2*time.Hour))
	noErr(t, "forecast across 1 July", err)
	if len(got.GetPoints()) != 8 || got.GetPoints()[2].GetLoadW() != 17519.5 || got.GetPoints()[4].GetLoadW() != 0.5 {
		t.Errorf("across 1 July: %d points, %v then %v", len(got.GetPoints()), got.GetPoints()[2].GetLoadW(), got.GetPoints()[4].GetLoadW())
	}

	_, err = forecast(day.Add(time.Hour), day)
	wantViolation(t, "an inverted range", err, "to must be after from")
	_, err = forecast(day, day.Add(8*24*time.Hour))
	wantViolation(t, "more than 7 days", err, "at most 7 days")
	_, err = p.feeders("").GetFeederForecast(ctx, req(&doelabv1.GetFeederForecastRequest{FeederId: unknownID, From: timestamppb.New(day), To: timestamppb.New(day.Add(time.Hour))}))
	wantCode(t, "an unknown feeder", err, connect.CodeNotFound)
}

func TestExportEnvelopeRun(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	a, b := p.fixture.SiteA.ID.String(), p.fixture.SiteB.ID.String()
	_, err := p.publish(engineToken, p.runID, "batch-0001",
		envelope(a, 0, 1500), envelope(a, 1, 1400), envelope(b, 0, 900))
	noErr(t, "publish", err)

	export := func(token, id string) (*doelabv1.ExportEnvelopeRunResponse, error) {
		res, err := p.runs(token).ExportEnvelopeRun(ctx, req(&doelabv1.ExportEnvelopeRunRequest{Id: id}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	// An export writes to the object store: it is the operator's to ask for.
	_, err = export("", p.runID)
	wantCode(t, "no token", err, connect.CodeUnauthenticated)
	_, err = export(engineToken, p.runID)
	wantCode(t, "the engine's token", err, connect.CodePermissionDenied)
	_, err = export(operatorToken, "not-a-uuid")
	wantViolation(t, "a malformed id", err, "id")
	_, err = export(operatorToken, p.runID)
	wantCode(t, "a run that is still running", err, connect.CodeFailedPrecondition)
	_, err = export(operatorToken, uuid.NewString())
	wantCode(t, "an unknown run", err, connect.CodeNotFound)

	_, err = p.runs(engineToken).CompleteEnvelopeRun(ctx, req(&doelabv1.CompleteEnvelopeRunRequest{
		Id: p.runID, Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED, DurationMs: 120, SiteCount: 2, IntervalCount: 2,
	}))
	noErr(t, "complete", err)

	before := time.Now()
	res, err := export(operatorToken, p.runID)
	noErr(t, "export", err)
	if res.GetRows() != 3 || res.GetObjectKey() != "exports/runs/LV10/"+p.runID+".csv" || !contains(res.GetUrl(), p.runID) {
		t.Errorf("export = %v", res)
	}
	if expires := res.GetExpiresAt().AsTime(); expires.Before(before.Add(14*time.Minute)) || expires.After(before.Add(16*time.Minute)) {
		t.Errorf("the link expires at %v, want about fifteen minutes from now", expires)
	}
	// The file has a line for the header and one for each envelope the run
	// published.
	file, ok := p.Objects.Stat(res.GetObjectKey())
	if !ok {
		t.Fatal("no file in the object store")
	}
	lines := 0
	for _, c := range file.Body {
		if c == '\n' {
			lines++
		}
	}
	if lines != 1+int(res.GetRows()) {
		t.Errorf("the file has %d lines for %d rows:\n%s", lines, res.GetRows(), file.Body)
	}
}
