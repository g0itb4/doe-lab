package server_test

import (
	"testing"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
)

func TestEnvelopeConfigs(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	feederID := a.fixture.Feeder.ID.String()
	valid := func() *doelabv1.EnvelopeConfig {
		return &doelabv1.EnvelopeConfig{
			FeederId: feederID, Policy: doelabv1.EnvelopePolicy_ENVELOPE_POLICY_EQUAL,
			VMinPu: 0.94, VMaxPu: 1.10, TransformerLimitPct: 100, LineLimitPct: 100,
			PvScale: 2, StaticLimitW: 5000, IntervalMinutes: 30, HorizonIntervals: 48,
			BreachGraceSeconds: 60, OfflineAfterSeconds: 300, Note: "first",
		}
	}
	create := func(token string, c *doelabv1.EnvelopeConfig) (*doelabv1.EnvelopeConfig, error) {
		res, err := a.configs(token).CreateEnvelopeConfig(ctx, req(&doelabv1.CreateEnvelopeConfigRequest{EnvelopeConfig: c}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetEnvelopeConfig(), nil
	}

	_, err := a.configs("").GetActiveEnvelopeConfig(ctx, req(&doelabv1.GetActiveEnvelopeConfigRequest{FeederId: feederID}))
	wantCode(t, "the active config of a feeder with none", err, connect.CodeNotFound)

	// Create: the server sets the version and the author.
	v1, err := create(operatorToken, valid())
	noErr(t, "create version 1", err)
	if v1.GetVersion() != 1 || v1.GetCreatedBy() != "operator" || v1.GetId() == "" || v1.GetCreatedAt() == nil ||
		v1.GetPolicy() != doelabv1.EnvelopePolicy_ENVELOPE_POLICY_EQUAL || v1.GetVMaxPu() != 1.10 || v1.GetPvScale() != 2 || v1.GetNote() != "first" {
		t.Errorf("version 1 = %v", v1)
	}
	second := valid()
	second.Policy, second.VMaxPu, second.Note = doelabv1.EnvelopePolicy_ENVELOPE_POLICY_PROPORTIONAL, 1.08, "tighter"
	v2, err := create(operatorToken, second)
	noErr(t, "create version 2", err)
	v3, err := create(operatorToken, valid())
	noErr(t, "create version 3", err)
	if v2.GetVersion() != 2 || v3.GetVersion() != 3 || v2.GetPolicy() != doelabv1.EnvelopePolicy_ENVELOPE_POLICY_PROPORTIONAL {
		t.Errorf("versions = %d, %d", v2.GetVersion(), v3.GetVersion())
	}

	// Get, and the active version.
	got, err := a.configs("").GetEnvelopeConfig(ctx, req(&doelabv1.GetEnvelopeConfigRequest{Id: v2.GetId()}))
	noErr(t, "get", err)
	if got.Msg.GetEnvelopeConfig().GetNote() != "tighter" {
		t.Errorf("get = %v", got.Msg.GetEnvelopeConfig())
	}
	_, err = a.configs("").GetEnvelopeConfig(ctx, req(&doelabv1.GetEnvelopeConfigRequest{Id: unknownID}))
	wantCode(t, "get an unknown config", err, connect.CodeNotFound)
	_, err = a.configs("").GetEnvelopeConfig(ctx, req(&doelabv1.GetEnvelopeConfigRequest{}))
	wantViolation(t, "get with no id", err, "id")
	active, err := a.configs("").GetActiveEnvelopeConfig(ctx, req(&doelabv1.GetActiveEnvelopeConfigRequest{FeederId: feederID}))
	noErr(t, "active", err)
	if active.Msg.GetEnvelopeConfig().GetVersion() != 3 {
		t.Errorf("active = version %d, want 3", active.Msg.GetEnvelopeConfig().GetVersion())
	}
	_, err = a.configs("").GetActiveEnvelopeConfig(ctx, req(&doelabv1.GetActiveEnvelopeConfigRequest{}))
	wantViolation(t, "active with no feeder", err, "feeder_id")

	// List: newest first, in two pages.
	page1, err := a.configs("").ListEnvelopeConfigs(ctx, req(&doelabv1.ListEnvelopeConfigsRequest{FeederId: feederID, PageSize: 2}))
	noErr(t, "page 1", err)
	page2, err := a.configs("").ListEnvelopeConfigs(ctx, req(&doelabv1.ListEnvelopeConfigsRequest{FeederId: feederID, PageSize: 2, PageToken: page1.Msg.GetNextPageToken()}))
	noErr(t, "page 2", err)
	if len(page1.Msg.GetEnvelopeConfigs()) != 2 || page1.Msg.GetEnvelopeConfigs()[0].GetVersion() != 3 ||
		len(page2.Msg.GetEnvelopeConfigs()) != 1 || page2.Msg.GetEnvelopeConfigs()[0].GetVersion() != 1 || page2.Msg.GetNextPageToken() != "" {
		t.Errorf("pages = %v then %v", page1.Msg, page2.Msg)
	}
	_, err = a.configs("").ListEnvelopeConfigs(ctx, req(&doelabv1.ListEnvelopeConfigsRequest{FeederId: unknownID}))
	wantCode(t, "list for an unknown feeder", err, connect.CodeNotFound)
	_, err = a.configs("").ListEnvelopeConfigs(ctx, req(&doelabv1.ListEnvelopeConfigsRequest{}))
	wantViolation(t, "list with no feeder", err, "feeder_id")

	// What a config may not be.
	change := func(f func(*doelabv1.EnvelopeConfig)) *doelabv1.EnvelopeConfig {
		c := valid()
		f(c)
		return c
	}
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.VMinPu, c.VMaxPu = 1.10, 0.94 }))
	wantViolation(t, "an inverted band", err, "v_min_pu must be below v_max_pu")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.VMaxPu = 1.3 }))
	wantViolation(t, "a band above 1.2", err, "v_max_pu")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.VMinPu = 0 }))
	wantViolation(t, "no lower limit", err, "required")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.Policy = 0 }))
	wantViolation(t, "no policy", err, "required")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.IntervalMinutes = 20 }))
	wantViolation(t, "a 20-minute interval", err, "interval_minutes")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.HorizonIntervals = 289 }))
	wantViolation(t, "a horizon over a day of 5-minute intervals", err, "horizon_intervals")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.TransformerLimitPct = 250 }))
	wantViolation(t, "a transformer limit over 200 %", err, "transformer_limit_pct")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.PvScale = 21 }))
	wantViolation(t, "a PV scale over 20", err, "pv_scale")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.OfflineAfterSeconds = 5 }))
	wantViolation(t, "an offline period under 10 s", err, "offline_after_seconds")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.BreachGraceSeconds = 3601 }))
	wantViolation(t, "a grace period over an hour", err, "breach_grace_seconds")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.Version = 9 }))
	wantViolation(t, "a client-chosen version", err, "set by the server")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.CreatedBy = "me" }))
	wantViolation(t, "a client-chosen author", err, "set by the server")
	_, err = create(operatorToken, change(func(c *doelabv1.EnvelopeConfig) { c.FeederId = unknownID }))
	wantCode(t, "an unknown feeder", err, connect.CodeFailedPrecondition)
	_, err = a.configs(operatorToken).CreateEnvelopeConfig(ctx, req(&doelabv1.CreateEnvelopeConfigRequest{}))
	wantCode(t, "no config", err, connect.CodeInvalidArgument)
	_, err = create("", valid())
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)
	_, err = create(engineToken, valid())
	wantCode(t, "the engine", err, connect.CodePermissionDenied)
}
