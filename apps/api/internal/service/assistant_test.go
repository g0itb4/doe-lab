package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"doelab/api/internal/apitest"
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

// asked is an ops scene with the assistant over it, a model that plays a
// script, and a wall clock the test moves.
type asked struct {
	*ops
	model *apitest.Model
	svc   *service.Assistant
	wall  time.Time
}

func newAsked(t *testing.T) *asked {
	t.Helper()
	a := &asked{ops: newOps(t), model: &apitest.Model{}, wall: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)}
	a.svc = service.NewAssistant(a.store, a.clock, a.model, 1_000_000)
	a.svc.Now = func() time.Time { return a.wall }
	// Out of the way, for every test but the one about it.
	a.svc.PerMinute = 1000
	// Ten minutes into the profile day: 10:10 in Sydney.
	a.clock.set(at(600))
	return a
}

// ask puts a question about LV10 and returns the steps of the answer.
func (a *asked) ask(q service.Question) ([]service.AnswerStep, error) {
	var steps []service.AnswerStep
	if q.FeederCode == "" {
		q.FeederCode = "LV10"
	}
	if q.Client == "" {
		q.Client = "203.0.113.7"
	}
	err := a.svc.Ask(context.Background(), q, func(step service.AnswerStep) error {
		steps = append(steps, step)
		return nil
	})
	return steps, err
}

// answered is a turn that ends the answer.
func answered(text ...string) apitest.Step {
	return apitest.Step{Text: text, Turn: service.ModelTurn{Stop: service.StopAnswered}}
}

// looksUp is a turn that asks for lookups.
func looksUp(calls ...service.ToolCall) apitest.Step {
	return apitest.Step{Turn: service.ModelTurn{Stop: service.StopLookups, Calls: calls}}
}

// lookup has the model make one lookup, and returns what it was given back
// and the step the asker saw.
func (a *asked) lookup(t *testing.T, feederCode, name, input string) (service.ToolResult, service.Lookup) {
	t.Helper()
	a.model.Play(looksUp(apitest.Call("call-1", name, input)), answered("Done."))
	steps, err := a.ask(service.Question{FeederCode: feederCode, Text: "?"})
	if err != nil {
		t.Fatalf("%s %s: %v", name, input, err)
	}
	if len(steps) != 3 || steps[0].Lookup == nil || steps[2].End != service.EndComplete {
		t.Fatalf("%s %s: steps = %+v", name, input, steps)
	}
	turns := a.model.Results()
	last := turns[len(turns)-1]
	if len(last) != 1 || last[0].CallID != "call-1" {
		t.Fatalf("%s %s: the model was given %+v", name, input, last)
	}
	return last[0], *steps[0].Lookup
}

func TestAssistantAnswersFromLookups(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	ctx := repotest.Ctx()
	nmi := a.f.SiteA.NMI

	// The engine limits site A now, and more tightly at 11:30.
	a.limit(t, 0, 3000)
	a.limit(t, 3, 2100)
	if err := a.store.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{repotest.Interval(a.run.ID, a.f.Feeder.ID, 3, -40000)}); err != nil {
		t.Fatal(err)
	}

	a.model.Play(
		apitest.Step{
			Text: []string{"Looking that up."},
			Turn: service.ModelTurn{Stop: service.StopLookups, Calls: []service.ToolCall{
				apitest.Call("call-1", "get_binding_constraint", fmt.Sprintf(`{"nmi":%q,"at":"2012-10-01T11:30:00+10:00"}`, nmi)),
				apitest.Call("call-2", "get_envelope", fmt.Sprintf(`{"nmi":%q}`, nmi)),
			}},
		},
		answered("Voltage ", "at Ld1_LOAD_A."),
	)
	steps, err := a.ask(service.Question{Text: "Why is this site limited at 11:30?", NMI: nmi})
	if err != nil {
		t.Fatal(err)
	}

	// The asker sees the lookups and the text in the order they happened,
	// and one end.
	want := []service.AnswerStep{
		{Text: "Looking that up."},
		{Lookup: &service.Lookup{Tool: service.ToolGetBindingConstraint, Subject: nmi + " at 11:30 on 1 Oct", Found: true}},
		{Lookup: &service.Lookup{Tool: service.ToolGetEnvelope, Subject: nmi + " at 10:10 on 1 Oct", Found: true}},
		{Text: "Voltage "},
		{Text: "at Ld1_LOAD_A."},
		{End: service.EndComplete},
	}
	if len(steps) != len(want) {
		t.Fatalf("steps = %+v", steps)
	}
	for i, w := range want {
		got := steps[i]
		if got.Text != w.Text || got.End != w.End || (got.Lookup == nil) != (w.Lookup == nil) || (w.Lookup != nil && *got.Lookup != *w.Lookup) {
			t.Errorf("step %d = %+v (%+v), want %+v (%+v)", i, got, got.Lookup, w, w.Lookup)
		}
	}

	// The model was told who asks about what, and when it is on the feeder.
	briefs := a.model.Briefs()
	if len(briefs) != 1 {
		t.Fatalf("briefs = %d", len(briefs))
	}
	brief := briefs[0]
	for _, line := range []string{
		"Feeder: LV10 (Feeder LV10). Time zone: Australia/Sydney.",
		"Feeder time now: 2012-10-01T10:10:00+10:00, a Monday.",
		"The asker has site " + nmi + " on screen.",
		"Question: Why is this site limited at 11:30?",
	} {
		if !strings.Contains(brief.Question, line) {
			t.Errorf("the question lacks %q:\n%s", line, brief.Question)
		}
	}
	if !strings.Contains(brief.System, "plain text") || brief.MaxTokens != service.MaxAnswerTokens {
		t.Errorf("brief = %d tokens, system %q", brief.MaxTokens, brief.System)
	}
	var names []string
	for _, tool := range brief.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" || tool.Properties == nil {
			t.Errorf("tool %s has no description or no properties", tool.Name)
		}
	}
	if got := strings.Join(names, " "); got != "get_envelope get_binding_constraint list_breaches get_config" {
		t.Errorf("tools = %s", got)
	}

	// The second turn was given both results, and the binding constraint is
	// the one in the database.
	turns := a.model.Results()
	if len(turns) != 2 || len(turns[0]) != 0 || len(turns[1]) != 2 {
		t.Fatalf("turns = %+v", turns)
	}
	stored, err := a.store.GetCurrentEnvelope(ctx, a.f.SiteA.ID, at(3*1800))
	if err != nil {
		t.Fatal(err)
	}
	var binding struct {
		NMI           string `json:"nmi"`
		IntervalStart string `json:"interval_start"`
		IntervalEnd   string `json:"interval_end"`
		SetBy         string `json:"set_by"`
		Export        struct {
			LimitW           float64 `json:"limit_w"`
			ConnectionLimitW float64 `json:"connection_limit_w"`
			Binding          string  `json:"binding"`
			Element          string  `json:"element"`
		} `json:"export"`
		Import struct {
			Binding string `json:"binding"`
			Element string `json:"element"`
		} `json:"import"`
		Backstop any `json:"backstop"`
		Feeder   struct {
			NominalVoltageV float64 `json:"nominal_voltage_v"`
			NoLimits        float64 `json:"highest_voltage_pu_with_no_limits"`
			AtEnvelopes     float64 `json:"highest_voltage_pu_at_the_envelopes"`
			AtFixed         float64 `json:"highest_voltage_pu_at_a_fixed_limit"`
		} `json:"feeder_forecast"`
	}
	result := turns[1][0]
	if err := json.Unmarshal([]byte(result.Content), &binding); err != nil || result.IsError || result.CallID != "call-1" {
		t.Fatalf("binding result = %+v, %v", result, err)
	}
	if binding.NMI != nmi || binding.SetBy != "engine" || binding.Backstop != nil ||
		binding.IntervalStart != "2012-10-01T11:30:00+10:00" || binding.IntervalEnd != "2012-10-01T12:00:00+10:00" ||
		binding.Export.LimitW != stored.ExportLimitW || binding.Export.ConnectionLimitW != a.f.SiteA.ExportCapW ||
		binding.Export.Binding != string(stored.ExportBinding) || binding.Export.Element != stored.ExportBindingElement ||
		binding.Import.Binding != string(stored.ImportBinding) || binding.Import.Element != "" {
		t.Errorf("binding = %+v, stored %+v", binding, stored)
	}
	if binding.Feeder.NominalVoltageV != 230 || binding.Feeder.NoLimits != 1.06 || binding.Feeder.AtEnvelopes != 1.09 || binding.Feeder.AtFixed != 1.13 {
		t.Errorf("feeder forecast = %+v", binding.Feeder)
	}

	var envelope map[string]any
	result = turns[1][1]
	if err := json.Unmarshal([]byte(result.Content), &envelope); err != nil || result.IsError || result.CallID != "call-2" {
		t.Fatalf("envelope result = %+v, %v", result, err)
	}
	if envelope["export_limit_w"] != 3000.0 || envelope["import_limit_w"] != 7000.0 || envelope["set_by"] != "engine" ||
		envelope["interval_start"] != "2012-10-01T10:00:00+10:00" || envelope["connection_export_limit_w"] != 5000.0 ||
		envelope["connection_import_limit_w"] != 7000.0 {
		t.Errorf("envelope = %v", envelope)
	}
}

func TestAssistantEnvelopeLookupsThatFindNothing(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	other := repotest.Seed(t, a.store.Store, "LV20", 11)
	nmi := a.f.SiteA.NMI
	a.limit(t, 0, 3000)

	for _, tc := range []struct {
		name, input string
		subject     string
		mention     string
	}{
		{"no envelope then", fmt.Sprintf(`{"nmi":%q,"at":"2012-10-03T09:00:00+10:00"}`, nmi), nmi + " at 09:00 on 3 Oct", "has no envelope at 2012-10-03T09:00:00+10:00"},
		{"a time that is not one", fmt.Sprintf(`{"nmi":%q,"at":"half past twelve"}`, nmi), nmi, "expected RFC 3339 with an offset, like 2012-10-01T10:10:00+10:00"},
		{"an input that is not an object", `[]`, "", "not a JSON object"},
		{"no such site", `{"nmi":"XDLAB999999"}`, "XDLAB999999", "not found"},
		{"a site of another feeder", fmt.Sprintf(`{"nmi":%q}`, other.SiteA.NMI), other.SiteA.NMI, "is not on feeder LV10"},
	} {
		for _, tool := range []service.AssistantTool{service.ToolGetEnvelope, service.ToolGetBindingConstraint} {
			result, lookup := a.lookup(t, "LV10", string(tool), tc.input)
			if !result.IsError || !strings.Contains(result.Content, tc.mention) {
				t.Errorf("%s, %s: result = %+v, want an error that mentions %q", tc.name, tool, result, tc.mention)
			}
			if lookup != (service.Lookup{Tool: tool, Subject: tc.subject}) {
				t.Errorf("%s, %s: lookup = %+v", tc.name, tool, lookup)
			}
		}
	}

	// A lookup that does not exist is answered, not obeyed.
	result, lookup := a.lookup(t, "LV10", "clear_backstop", `{}`)
	if !result.IsError || !strings.Contains(result.Content, "no lookup named clear_backstop") ||
		lookup != (service.Lookup{Subject: "clear_backstop"}) {
		t.Errorf("an unknown lookup: %+v, %+v", result, lookup)
	}
}

func TestAssistantSeesABackstop(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	a.limit(t, 0, 3000)
	if _, _, err := a.backstops.Trigger(repotest.Ctx(), domain.BackstopEvent{
		FeederID: a.f.Feeder.ID, Reason: "transformer fault", ExportLimitW: 500,
	}, nil); err != nil {
		t.Fatal(err)
	}

	result, lookup := a.lookup(t, "LV10", "get_binding_constraint", fmt.Sprintf(`{"nmi":%q}`, a.f.SiteA.NMI))
	var got struct {
		SetBy  string `json:"set_by"`
		Export struct {
			LimitW float64 `json:"limit_w"`
		} `json:"export"`
		Backstop struct {
			Reason      string `json:"reason"`
			TriggeredBy string `json:"triggered_by"`
			TriggeredAt string `json:"triggered_at"`
		} `json:"backstop"`
		Feeder any `json:"feeder_forecast"`
	}
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil || result.IsError || !lookup.Found {
		t.Fatalf("result = %+v, %+v, %v", result, lookup, err)
	}
	// No run has covered the interval, so there is no forecast to give.
	if got.SetBy != "backstop" || got.Export.LimitW != 500 || got.Feeder != nil ||
		got.Backstop.Reason != "transformer fault" || got.Backstop.TriggeredBy != "operator" ||
		got.Backstop.TriggeredAt != "2012-10-01T10:10:00+10:00" {
		t.Errorf("binding under a backstop = %+v", got)
	}
}

func TestAssistantListsBreaches(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	ctx := repotest.Ctx()
	feeder, siteA, siteB := a.f.Feeder.ID, a.f.SiteA, a.f.SiteB

	// A breach at site B that is over, an open one at site A, and a device
	// at site A that went silent.
	for _, alert := range []domain.Alert{
		{
			SiteID: siteB.ID, FeederID: feeder, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
			OpenedAt: at(60), LimitW: repotest.Ptr(1000.0), PeakW: repotest.Ptr(1800.0), Detail: "export above the limit",
		},
		{
			SiteID: siteA.ID, FeederID: feeder, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityCritical,
			OpenedAt: at(120), LimitW: repotest.Ptr(3000.0), PeakW: repotest.Ptr(4200.0),
		},
		{
			SiteID: siteA.ID, FeederID: feeder, DeviceID: &a.battery.ID, Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo,
			OpenedAt: at(180),
		},
	} {
		if _, _, err := a.store.OpenAlert(ctx, alert); err != nil {
			t.Fatal(err)
		}
	}
	if resolved, err := a.store.ResolveAlert(ctx, siteB.ID, domain.AlertConstraintBreach, at(300)); err != nil || !resolved {
		t.Fatalf("resolve: %v, %v", resolved, err)
	}

	type view struct {
		Alerts []struct {
			NMI        string   `json:"nmi"`
			Kind       string   `json:"kind"`
			Severity   string   `json:"severity"`
			OpenedAt   string   `json:"opened_at"`
			ResolvedAt string   `json:"resolved_at"`
			Open       bool     `json:"open"`
			LimitW     *float64 `json:"limit_w"`
			PeakW      *float64 `json:"peak_w"`
			Detail     string   `json:"detail"`
		} `json:"alerts"`
		More bool `json:"more"`
		Open int  `json:"open_alerts_on_feeder"`
	}
	list := func(input, subject string) view {
		t.Helper()
		result, lookup := a.lookup(t, "LV10", "list_breaches", input)
		var got view
		if err := json.Unmarshal([]byte(result.Content), &got); err != nil || result.IsError {
			t.Fatalf("list_breaches %s: %+v, %v", input, result, err)
		}
		if lookup != (service.Lookup{Tool: service.ToolListBreaches, Subject: subject, Found: true}) {
			t.Errorf("list_breaches %s: lookup = %+v", input, lookup)
		}
		return got
	}

	// Everything, newest first.
	all := list(`{}`, "alerts")
	if len(all.Alerts) != 3 || all.More || all.Open != 2 {
		t.Fatalf("all = %+v", all)
	}
	offline, open, over := all.Alerts[0], all.Alerts[1], all.Alerts[2]
	if offline.Kind != "device_offline" || offline.NMI != siteA.NMI || !offline.Open || offline.LimitW != nil || offline.Severity != "info" {
		t.Errorf("the offline alert = %+v", offline)
	}
	if open.Kind != "constraint_breach" || open.NMI != siteA.NMI || !open.Open || open.ResolvedAt != "" ||
		*open.LimitW != 3000 || *open.PeakW != 4200 || open.OpenedAt != "2012-10-01T10:02:00+10:00" {
		t.Errorf("the open breach = %+v", open)
	}
	if over.NMI != siteB.NMI || over.Open || over.ResolvedAt != "2012-10-01T10:05:00+10:00" || over.Detail != "export above the limit" {
		t.Errorf("the breach that is over = %+v", over)
	}

	// Narrowed to what is open, and to one site.
	if got := list(`{"open_only":true}`, "open alerts"); len(got.Alerts) != 2 {
		t.Errorf("open only = %+v", got)
	}
	if got := list(fmt.Sprintf(`{"nmi":%q}`, siteB.NMI), "alerts of "+siteB.NMI); len(got.Alerts) != 1 || got.Alerts[0].NMI != siteB.NMI || got.Open != 2 {
		t.Errorf("site B = %+v", got)
	}
	if got := list(fmt.Sprintf(`{"nmi":%q,"open_only":true}`, siteB.NMI), "open alerts of "+siteB.NMI); len(got.Alerts) != 0 {
		t.Errorf("site B, open only = %+v", got)
	}

	// Asked for badly.
	result, lookup := a.lookup(t, "LV10", "list_breaches", `"all of them"`)
	if !result.IsError || !strings.Contains(result.Content, "not a JSON object") || lookup.Found {
		t.Errorf("a string for an input: %+v, %+v", result, lookup)
	}
	result, lookup = a.lookup(t, "LV10", "list_breaches", `{"nmi":"XDLAB999999"}`)
	if !result.IsError || lookup != (service.Lookup{Tool: service.ToolListBreaches, Subject: "alerts of XDLAB999999"}) {
		t.Errorf("no such site: %+v, %+v", result, lookup)
	}
}

func TestAssistantReadsTheConfig(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	repotest.Seed(t, a.store.Store, "LV20", 11)

	result, lookup := a.lookup(t, "LV10", "get_config", `{}`)
	var got map[string]any
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil || result.IsError {
		t.Fatalf("get_config: %+v, %v", result, err)
	}
	if lookup != (service.Lookup{Tool: service.ToolGetConfig, Subject: "config version 1", Found: true}) {
		t.Errorf("lookup = %+v", lookup)
	}
	for key, want := range map[string]any{
		"version": 1.0, "policy": "equal", "v_min_pu": 0.94, "v_max_pu": 1.10, "nominal_voltage_v": 230.0,
		"transformer_kva": 100.0, "transformer_limit_pct": 100.0, "line_limit_pct": 100.0, "pv_scale": 1.0,
		"fixed_limit_w": 5000.0, "interval_minutes": 30.0, "horizon_intervals": 48.0,
		"breach_grace_seconds": 60.0, "offline_after_seconds": 300.0, "note": "test", "saved_by": "operator",
	} {
		if got[key] != want {
			t.Errorf("config %s = %v, want %v", key, got[key], want)
		}
	}

	// A feeder that has never been given one.
	result, lookup = a.lookup(t, "LV20", "get_config", `{}`)
	if !result.IsError || !strings.Contains(result.Content, "feeder LV20 has no config") ||
		lookup != (service.Lookup{Tool: service.ToolGetConfig, Subject: "the config"}) {
		t.Errorf("no config: %+v, %+v", result, lookup)
	}
}

func TestAssistantStopsWhenTheDatabaseFails(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	a.limit(t, 0, 3000)
	if _, _, err := a.backstops.Trigger(repotest.Ctx(), domain.BackstopEvent{FeederID: a.f.Feeder.ID, Reason: "fault", ExportLimitW: 500}, nil); err != nil {
		t.Fatal(err)
	}
	site := fmt.Sprintf(`{"nmi":%q}`, a.f.SiteA.NMI)

	for _, tc := range []struct{ fails, tool, input string }{
		{"GetCurrentEnvelope", "get_envelope", site},
		{"GetCurrentEnvelope", "get_binding_constraint", site},
		{"GetBackstopEvent", "get_binding_constraint", site},
		{"ListFeederIntervals", "get_binding_constraint", site},
		{"ListAllSites", "list_breaches", `{}`},
		{"ListAlerts", "list_breaches", `{}`},
		{"CountOpenAlerts", "list_breaches", `{}`},
		{"GetActiveEnvelopeConfig", "get_config", `{}`},
	} {
		a.model.Play(looksUp(apitest.Call("call-1", tc.tool, tc.input)), answered("It should not come to this."))
		a.store.failAt(tc.fails)
		steps, err := a.ask(service.Question{Text: "?"})
		a.store.failAt("")
		// Not a result for the model to talk around: the answer ends.
		if !errors.Is(err, errDown) || len(steps) != 0 {
			t.Errorf("%s fails in %s: %v, steps %+v", tc.fails, tc.tool, err, steps)
		}
	}
}

func TestAssistantRefusesBeforeItSpends(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	a.svc.PerMinute = 1
	other := repotest.Seed(t, a.store.Store, "LV20", 11)
	odd := oddZone(t, a.ops)

	for _, tc := range []struct {
		name    string
		q       service.Question
		want    error
		mention string
	}{
		{"no such feeder", service.Question{FeederCode: "LV99", Text: "?"}, domain.ErrNotFound, "feeder LV99"},
		{"no such site", service.Question{Text: "?", NMI: "XDLAB999999"}, domain.ErrNotFound, "XDLAB999999"},
		{"a site of another feeder", service.Question{Text: "?", NMI: other.SiteA.NMI}, domain.ErrNotFound, "is not on feeder LV10"},
	} {
		steps, err := a.ask(tc.q)
		if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.mention) || len(steps) != 0 {
			t.Errorf("%s: %v, steps %+v", tc.name, err, steps)
		}
	}
	if _, err := a.ask(service.Question{FeederCode: "ODD", Text: "?", NMI: odd.NMI}); err == nil || !strings.Contains(err.Error(), "Mars/Olympus_Mons") {
		t.Errorf("an unknown zone: %v", err)
	}
	// None of them reached the model, or took the client's one question.
	if len(a.model.Briefs()) != 0 {
		t.Errorf("the model was started %d times", len(a.model.Briefs()))
	}
	a.model.Play(answered("Yes."))
	if steps, err := a.ask(service.Question{Text: "?"}); err != nil || len(steps) != 2 {
		t.Errorf("the first real question: %+v, %v", steps, err)
	}

	// With no model there is nothing to ask.
	off := service.NewAssistant(a.store, a.clock, nil, 1_000_000)
	if status := off.Status(); status != (service.AssistantStatus{Reason: service.AssistantOff}) {
		t.Errorf("status with no model = %+v", status)
	}
	err := off.Ask(context.Background(), service.Question{FeederCode: "LV10", Text: "?"}, func(service.AnswerStep) error { return nil })
	if !errors.Is(err, domain.ErrFailedPrecondition) || !strings.Contains(err.Error(), "is off") {
		t.Errorf("asking with no model: %v", err)
	}
}

func TestAssistantRationsQuestionsByClient(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	a.svc.PerMinute = 2
	ask := func(client string) error {
		_, err := a.ask(service.Question{Text: "?", Client: client})
		return err
	}

	// Two in a row, and the third is refused.
	for i := range 2 {
		if err := ask("203.0.113.7"); err != nil {
			t.Fatalf("question %d: %v", i+1, err)
		}
	}
	if err := ask("203.0.113.7"); !errors.Is(err, domain.ErrExhausted) || !strings.Contains(err.Error(), "wait a minute") {
		t.Errorf("the third question: %v", err)
	}
	// Another client has its own ration.
	if err := ask("198.51.100.20"); err != nil {
		t.Errorf("another client: %v", err)
	}
	// Half a minute refills one question, and a long wait no more than two.
	a.wall = a.wall.Add(30 * time.Second)
	if err := ask("203.0.113.7"); err != nil {
		t.Errorf("after half a minute: %v", err)
	}
	if err := ask("203.0.113.7"); !errors.Is(err, domain.ErrExhausted) {
		t.Errorf("straight after: %v", err)
	}
	a.wall = a.wall.Add(time.Hour)
	for i := range 2 {
		if err := ask("203.0.113.7"); err != nil {
			t.Errorf("after an hour, question %d: %v", i+1, err)
		}
	}
	if err := ask("203.0.113.7"); !errors.Is(err, domain.ErrExhausted) {
		t.Errorf("after an hour, the third: %v", err)
	}

	// The memory of clients is bounded: when it is full it is dropped whole,
	// and with it what each client had used.
	for i := range 4096 {
		if err := ask(fmt.Sprintf("client-%d", i)); err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
	}
	if err := ask("203.0.113.7"); err != nil {
		t.Errorf("after the memory was dropped: %v", err)
	}
}

func TestAssistantKeepsToADailyBudget(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	a.svc.DailyBudgetMicroUSD = 1000
	costing := func(microUSD int64) apitest.Step {
		return apitest.Step{Turn: service.ModelTurn{Stop: service.StopAnswered, CostMicroUSD: microUSD}}
	}
	available := service.AssistantStatus{Available: true}
	spent := service.AssistantStatus{Reason: service.AssistantBudgetSpent}

	if status := a.svc.Status(); status != available {
		t.Errorf("status at the start = %+v", status)
	}
	a.model.Play(costing(600))
	if _, err := a.ask(service.Question{Text: "?"}); err != nil {
		t.Fatal(err)
	}
	if status := a.svc.Status(); status != available {
		t.Errorf("status with 600 of 1000 spent = %+v", status)
	}

	// A turn that failed was still paid for.
	modelDown := fmt.Errorf("the model is away: %w", domain.ErrRetryable)
	a.model.Play(apitest.Step{Turn: service.ModelTurn{CostMicroUSD: 400}, Err: modelDown})
	if steps, err := a.ask(service.Question{Text: "?"}); !errors.Is(err, domain.ErrRetryable) || len(steps) != 0 {
		t.Errorf("a failed turn: %+v, %v", steps, err)
	}
	if status := a.svc.Status(); status != spent {
		t.Errorf("status with the budget spent = %+v", status)
	}
	if _, err := a.ask(service.Question{Text: "?"}); !errors.Is(err, domain.ErrExhausted) || !strings.Contains(err.Error(), "midnight UTC") {
		t.Errorf("a question over the budget: %v", err)
	}

	// The next UTC day starts from nothing.
	a.wall = time.Date(2026, 3, 2, 0, 0, 1, 0, time.UTC)
	if status := a.svc.Status(); status != available {
		t.Errorf("status the next day = %+v", status)
	}
	a.model.Play(costing(100))
	if _, err := a.ask(service.Question{Text: "?"}); err != nil {
		t.Errorf("a question the next day: %v", err)
	}
}

func TestAssistantAnswerEnds(t *testing.T) {
	t.Parallel()
	a := newAsked(t)

	for stop, want := range map[service.ModelStop]service.AnswerEnd{
		service.StopAnswered: service.EndComplete,
		service.StopLength:   service.EndCutShort,
		service.StopDeclined: service.EndDeclined,
	} {
		a.model.Play(apitest.Step{Turn: service.ModelTurn{Stop: stop}})
		steps, err := a.ask(service.Question{Text: "?"})
		if err != nil || len(steps) != 1 || steps[0].End != want {
			t.Errorf("stop %d: %+v, %v, want %s", stop, steps, err, want)
		}
	}

	// A model that never stops looking things up is stopped.
	var script []apitest.Step
	for range service.MaxModelTurns + 3 {
		script = append(script, looksUp(apitest.Call("call", "get_config", `{}`)))
	}
	a.model.Play(script...)
	before := len(a.model.Results())
	steps, err := a.ask(service.Question{Text: "?"})
	if err != nil || len(steps) != service.MaxModelTurns+1 || steps[len(steps)-1].End != service.EndTooManyLookups {
		t.Errorf("endless lookups: %+v, %v", steps, err)
	}
	if turns := len(a.model.Results()) - before; turns != service.MaxModelTurns {
		t.Errorf("the model had %d turns, want %d", turns, service.MaxModelTurns)
	}
}

func TestAssistantStopsWhenTheAskerLeaves(t *testing.T) {
	t.Parallel()
	a := newAsked(t)
	gone := errors.New("the asker has gone")

	// The send fails at a piece of text, at a lookup, and at the end.
	for name, script := range map[string][]apitest.Step{
		"at the text":   {{Text: []string{"One moment.", "Looking."}, Turn: service.ModelTurn{Stop: service.StopAnswered}}},
		"at the lookup": {looksUp(apitest.Call("call", "get_config", `{}`), apitest.Call("call", "get_config", `{}`)), answered()},
		"at the end":    {answered()},
	} {
		a.model.Play(script...)
		sent := 0
		err := a.svc.Ask(context.Background(), service.Question{FeederCode: "LV10", Text: "?", Client: "c"}, func(service.AnswerStep) error {
			sent++
			return gone
		})
		// One send is tried, and none after it failed.
		if !errors.Is(err, gone) || sent != 1 {
			t.Errorf("%s: %v after %d sends", name, err, sent)
		}
	}
}
