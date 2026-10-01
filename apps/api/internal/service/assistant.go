package service

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Assistant answers a question about a feeder in plain words. A language
// model writes the answer; this service runs the conversation: it frames the
// question, makes the lookups the model asks for, and stops it when a limit
// is reached.
//
// The lookups only read. There is none that writes, so nothing a question
// says can change the feeder.
//
// Every answer costs money, so the service rations them four ways: it is off
// without a model; one client asks a few questions a minute; one turn writes
// a bounded number of tokens, and one question takes a bounded number of
// turns; and a day has a budget. The rations live in this process, like the
// rate limit of the API: a restart forgets them, and the provider's own
// spending limit is the bound that a restart cannot move.
type Assistant struct {
	store Store
	clock Clock
	model Model

	// PerMinute is how many questions one client may ask in a minute.
	PerMinute int
	// DailyBudgetMicroUSD is what a UTC day of answers may cost, in
	// millionths of a US dollar. A question that has begun is finished, so a
	// day may overrun by the questions in flight when the budget runs out.
	DailyBudgetMicroUSD int64
	// Timeout bounds one answer on the wall clock.
	Timeout time.Duration

	// Now is the wall clock: the rations run on it, not on feeder time.
	Now func() time.Time

	mu      sync.Mutex
	day     time.Time
	spent   int64
	clients map[string]*ration
}

// The bounds of one question.
const (
	// MaxQuestionChars is the longest question, in characters. The wire
	// format refuses a longer one.
	MaxQuestionChars = 500
	// MaxAnswerTokens bounds what the model writes in one turn, its
	// reasoning included.
	MaxAnswerTokens = 2048
	// MaxModelTurns bounds the turns of one question: each turn but the last
	// ends in lookups.
	MaxModelTurns = 6
)

// A backstop, not a working set: see the API's throttle.
const assistantClientsMax = 4096

// ration is one client's token bucket: a level, and when it was last filled.
type ration struct {
	tokens float64
	last   time.Time
}

// NewAssistant builds the service. A nil model means the assistant is off.
// dailyBudgetMicroUSD is what a day of answers may cost.
func NewAssistant(store Store, clock Clock, model Model, dailyBudgetMicroUSD int64) *Assistant {
	return &Assistant{
		store: store, clock: clock, model: model,
		PerMinute: 5, DailyBudgetMicroUSD: dailyBudgetMicroUSD, Timeout: 90 * time.Second,
		Now: time.Now, clients: make(map[string]*ration),
	}
}

// AssistantUnavailable is why the assistant cannot be asked.
type AssistantUnavailable string

// The reasons.
const (
	// AssistantOff: the server has no model.
	AssistantOff AssistantUnavailable = "off"
	// AssistantBudgetSpent: the budget of the day is spent.
	AssistantBudgetSpent AssistantUnavailable = "budget_spent"
)

// AssistantStatus says whether the assistant can be asked, and why not.
type AssistantStatus struct {
	Available bool
	// Reason is set exactly when the assistant is not available.
	Reason AssistantUnavailable
}

// Status says whether a question would be taken now.
func (s *Assistant) Status() AssistantStatus {
	if s.model == nil {
		return AssistantStatus{Reason: AssistantOff}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.budgetSpent(s.Now()) {
		return AssistantStatus{Reason: AssistantBudgetSpent}
	}
	return AssistantStatus{Available: true}
}

// budgetSpent says whether the budget of today is spent, and starts a new
// day's count when the day has changed. Called with the lock held.
func (s *Assistant) budgetSpent(now time.Time) bool {
	if day := now.UTC().Truncate(24 * time.Hour); !day.Equal(s.day) {
		s.day, s.spent = day, 0
	}
	return s.spent >= s.DailyBudgetMicroUSD
}

// admit takes one question from a client, or says which ration refuses it.
func (s *Assistant) admit(client string) error {
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.budgetSpent(now) {
		return fmt.Errorf("the assistant has answered all it can today; it is back at midnight UTC: %w", domain.ErrExhausted)
	}
	// Full of clients that are all asking: drop the lot rather than grow.
	if len(s.clients) >= assistantClientsMax {
		clear(s.clients)
	}
	perMinute := float64(s.PerMinute)
	r, ok := s.clients[client]
	if !ok {
		r = &ration{tokens: perMinute, last: now}
		s.clients[client] = r
	}
	r.tokens = min(perMinute, r.tokens+now.Sub(r.last).Minutes()*perMinute)
	r.last = now
	if r.tokens < 1 {
		return fmt.Errorf("too many questions; wait a minute and ask again: %w", domain.ErrExhausted)
	}
	r.tokens--
	return nil
}

// spend adds the cost of a turn to the day.
func (s *Assistant) spend(microUSD int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spent += microUSD
}

// Question is what a client asks.
type Question struct {
	// FeederCode names the feeder the question is about.
	FeederCode string
	Text       string
	// NMI is the site on the asker's screen, or empty.
	NMI string
	// Client is the address the question came from. The ration is per client.
	Client string
}

// AssistantTool names a lookup.
type AssistantTool string

// The lookups.
const (
	ToolGetEnvelope          AssistantTool = "get_envelope"
	ToolGetBindingConstraint AssistantTool = "get_binding_constraint"
	ToolListBreaches         AssistantTool = "list_breaches"
	ToolGetConfig            AssistantTool = "get_config"
)

// AnswerEnd is how an answer ended.
type AnswerEnd string

// The ways an answer ends.
const (
	EndComplete       AnswerEnd = "complete"
	EndCutShort       AnswerEnd = "cut_short"
	EndDeclined       AnswerEnd = "declined"
	EndTooManyLookups AnswerEnd = "too_many_lookups"
)

// Lookup is one lookup the assistant made.
type Lookup struct {
	// Tool is empty when the model asked for a lookup that does not exist.
	Tool AssistantTool
	// Subject is what was looked up, in a few words.
	Subject string
	// Found is false when the lookup found nothing or was asked for badly.
	Found bool
}

// AnswerStep is one step of an answer: a piece of text, a lookup, or the end.
// Exactly one field is set.
type AnswerStep struct {
	Text   string
	Lookup *Lookup
	End    AnswerEnd
}

// Ask answers a question, and sends the answer as it forms: lookups and
// pieces of text in the order they happen, then one end.
//
// It is domain.ErrFailedPrecondition when the assistant is off,
// domain.ErrExhausted when a ration refuses the question, and
// domain.ErrNotFound for a feeder or a site that does not exist.
func (s *Assistant) Ask(ctx context.Context, q Question, send func(AnswerStep) error) error {
	if s.model == nil {
		return fmt.Errorf("the assistant is off on this server: %w", domain.ErrFailedPrecondition)
	}
	feeder, err := s.store.GetFeederByCode(ctx, q.FeederCode)
	if err != nil {
		return fmt.Errorf("feeder %s: %w", q.FeederCode, err)
	}
	zone, err := time.LoadLocation(feeder.Timezone)
	if err != nil {
		return fmt.Errorf("feeder %s: time zone %q: %w", feeder.Code, feeder.Timezone, err)
	}
	d := &desk{store: s.store, clock: s.clock, feeder: feeder, zone: zone}
	if q.NMI != "" {
		if _, err := d.site(ctx, q.NMI); err != nil {
			return err
		}
	}
	// Last, so that a question which could never be answered costs the
	// client nothing.
	if err := s.admit(q.Client); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	conversation := s.model.Start(ModelBrief{
		System: assistantBrief, Tools: toolSpecs(), Question: d.frame(q), MaxTokens: MaxAnswerTokens,
	})
	// The model's adapter calls onText from inside Next, so a send that fails
	// is kept until Next returns.
	var sendErr error
	onText := func(text string) {
		if sendErr == nil {
			sendErr = send(AnswerStep{Text: text})
		}
	}
	var results []ToolResult
	for range MaxModelTurns {
		turn, err := conversation.Next(ctx, results, onText)
		s.spend(turn.CostMicroUSD)
		if err := cmp.Or(err, sendErr); err != nil {
			return err
		}
		if turn.Stop != StopLookups {
			return send(AnswerStep{End: answerEnds[turn.Stop]})
		}
		results = make([]ToolResult, 0, len(turn.Calls))
		for _, call := range turn.Calls {
			result, lookup, err := d.run(ctx, call)
			if err != nil {
				return err
			}
			if err := send(AnswerStep{Lookup: &lookup}); err != nil {
				return err
			}
			results = append(results, result)
		}
	}
	return send(AnswerStep{End: EndTooManyLookups})
}

var answerEnds = map[ModelStop]AnswerEnd{
	StopAnswered: EndComplete,
	StopLength:   EndCutShort,
	StopDeclined: EndDeclined,
}

// assistantBrief is the standing instruction. It is the same for every
// question, and carries nothing of the feeder's state: that comes from the
// lookups, so that an answer cannot be older than the data.
const assistantBrief = `You answer questions about one low-voltage electricity feeder, in a public demonstration of dynamic operating envelopes.

Background. An envelope is the export limit and the import limit of one site for one interval of time, half an hour by default. An engine computes the envelopes from a forecast, so that every customer voltage, the transformer and every cable stay inside their limits when every site uses its whole limit at once. A site also has a connection limit, which no envelope exceeds. A backstop is a fixed export limit that an operator puts over the envelopes in an emergency. An alert is opened when a site exports above its limit for longer than a grace period (a breach), or when a device stops reporting.

Who asks. Network operators, planners, and visitors who are new to the subject. The question arrives after some context lines: the feeder, its time zone, the time now, and the site the asker has on screen, if any. "This site" and "here" mean that site.

How to answer.
- Look up what the answer needs before you write it. Every figure, site and time in an answer comes from a lookup made for this question. Never estimate one.
- Give the answer first, in a sentence or two, then the figures that support it. Stay under 120 words unless the question asks for more.
- Write plain text. No Markdown, no headings, no lists, no tables.
- Give power in kW to one decimal place (the lookups return watts), and voltage in volts (per unit times the nominal voltage). Give a time in the feeder's zone as HH:MM, with the date when it is not today.
- Use an operator's words: export limit, import limit, connection limit. Name a site by its NMI.
- A binding constraint says what stopped a limit from being larger. voltage_high: a customer voltage would pass the upper limit, and the element names where. voltage_low: the same at the lower limit. transformer: the transformer would pass its loading limit. line: a cable would pass its rating, and the element names the cable. site_cap: the limit equals the site's own connection limit, so the network is not what limits it. none: nothing binds.
- When a lookup finds nothing, say so plainly. Do not fill the gap.
- When the question is not about this feeder, its sites, envelopes, alerts or config, say in one sentence what you can answer instead.
- You can read and nothing else. Asked to trigger or clear a backstop, or to change a config or a site, say that an operator does that on the Operations or the Config page.`

// frame puts the context of a question in front of it.
func (d *desk) frame(q Question) string {
	now := d.clock.Now().In(d.zone)
	out := fmt.Sprintf("Feeder: %s (%s). Time zone: %s.\nFeeder time now: %s, a %s.\n",
		d.feeder.Code, d.feeder.Name, d.feeder.Timezone, now.Format(time.RFC3339), now.Weekday())
	if q.NMI != "" {
		out += fmt.Sprintf("The asker has site %s on screen.\n", q.NMI)
	}
	return out + "\nQuestion: " + q.Text
}

// desk makes the lookups of one question, on one feeder.
type desk struct {
	store  Store
	clock  Clock
	feeder domain.Feeder
	zone   *time.Location
}

// lookup is one tool: how the model is told of it, and what it does. run
// returns the subject of the lookup whether or not it finds anything.
type lookup struct {
	tool AssistantTool
	spec ModelTool
	run  func(d *desk, ctx context.Context, input []byte) (subject string, result any, err error)
}

const (
	nmiProperty = "The NMI of the site: eleven characters, like XDLAB000014."
	atProperty  = "The instant, as RFC 3339 with the offset of the feeder's zone, like 2026-11-10T12:30:00+11:00. Leave it out for now."
)

var lookups = []lookup{
	{
		tool: ToolGetEnvelope,
		spec: ModelTool{
			Name:        string(ToolGetEnvelope),
			Description: "The envelope in force for one site at an instant: its interval, the export and import limits in watts, whether the engine or a backstop set it, and the site's connection limits. Use it for what a site may export or import, now or at a time of the day.",
			Properties: map[string]any{
				"nmi": map[string]any{"type": "string", "description": nmiProperty},
				"at":  map[string]any{"type": "string", "description": atProperty},
			},
			Required: []string{"nmi"},
		},
		run: (*desk).getEnvelope,
	},
	{
		tool: ToolGetBindingConstraint,
		spec: ModelTool{
			Name:        string(ToolGetBindingConstraint),
			Description: "Why a site's limits are what they are at an instant: the binding constraint of the export limit and of the import limit, the network element each binds at, the backstop if one set the envelope, and the feeder's forecast for that interval (the highest customer voltage with no limits, at the envelopes, and at a fixed limit). Use it for any question of why a site is limited.",
			Properties: map[string]any{
				"nmi": map[string]any{"type": "string", "description": nmiProperty},
				"at":  map[string]any{"type": "string", "description": atProperty},
			},
			Required: []string{"nmi"},
		},
		run: (*desk).getBindingConstraint,
	},
	{
		tool: ToolListBreaches,
		spec: ModelTool{
			Name:        string(ToolListBreaches),
			Description: "The alerts of the feeder, newest first, twenty at most: breaches (a site exported above its limit; with the limit and the peak in watts) and devices that stopped reporting. Also the count of open alerts on the feeder. Use it for which sites are over their limit, or what went wrong.",
			Properties: map[string]any{
				"nmi":       map[string]any{"type": "string", "description": "Only the alerts of this site. Leave it out for the whole feeder."},
				"open_only": map[string]any{"type": "boolean", "description": "Only alerts that are still open."},
			},
		},
		run: (*desk).listBreaches,
	},
	{
		tool: ToolGetConfig,
		spec: ModelTool{
			Name:        string(ToolGetConfig),
			Description: "The config the engine runs with: how capacity is shared between sites, the voltage band in per unit, the loading limits of the transformer and the cables, the fixed limit the envelopes are compared with, the interval and the horizon, and the grace periods of the alerts.",
			Properties:  map[string]any{},
		},
		run: (*desk).getConfig,
	},
}

func toolSpecs() []ModelTool {
	out := make([]ModelTool, len(lookups))
	for i, l := range lookups {
		out[i] = l.spec
	}
	return out
}

// run makes the lookup a call asks for. A lookup that finds nothing, or that
// the model asked for badly, is a result the model reads and can recover
// from. Anything else is the database failing, and ends the answer.
func (d *desk) run(ctx context.Context, call ToolCall) (ToolResult, Lookup, error) {
	for _, l := range lookups {
		if l.spec.Name != call.Name {
			continue
		}
		subject, found, err := l.run(d, ctx, call.Input)
		switch {
		case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrInvalid):
			return ToolResult{CallID: call.ID, Content: err.Error(), IsError: true},
				Lookup{Tool: l.tool, Subject: subject}, nil
		case err != nil:
			return ToolResult{}, Lookup{}, err
		}
		// The views are structs of strings, numbers and booleans: they
		// always marshal.
		content, _ := json.Marshal(found)
		return ToolResult{CallID: call.ID, Content: string(content)},
			Lookup{Tool: l.tool, Subject: subject, Found: true}, nil
	}
	return ToolResult{CallID: call.ID, Content: "there is no lookup named " + call.Name, IsError: true},
		Lookup{Subject: call.Name}, nil
}

// site returns a site of the desk's feeder by NMI.
func (d *desk) site(ctx context.Context, nmi string) (domain.Site, error) {
	site, err := d.store.GetSiteByNMI(ctx, nmi)
	if err != nil {
		return site, fmt.Errorf("site %q: %w", nmi, err)
	}
	if site.FeederID != d.feeder.ID {
		return site, fmt.Errorf("site %s is not on feeder %s: %w", nmi, d.feeder.Code, domain.ErrNotFound)
	}
	return site, nil
}

// local writes an instant in the feeder's zone.
func (d *desk) local(t time.Time) string {
	return t.In(d.zone).Format(time.RFC3339)
}

// siteAt reads the two inputs that both envelope lookups take, and returns
// the site, its envelope at the instant, and the subject of the lookup.
func (d *desk) siteAt(ctx context.Context, input []byte) (domain.Site, domain.Envelope, string, error) {
	var in struct {
		NMI string `json:"nmi"`
		At  string `json:"at"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return domain.Site{}, domain.Envelope{}, "", fmt.Errorf("the input is not a JSON object with nmi and at: %w", domain.ErrInvalid)
	}
	site, err := d.site(ctx, in.NMI)
	if err != nil {
		return site, domain.Envelope{}, in.NMI, err
	}
	at := d.clock.Now()
	if in.At != "" {
		if at, err = time.Parse(time.RFC3339, in.At); err != nil {
			return site, domain.Envelope{}, in.NMI, fmt.Errorf("at %q: expected RFC 3339 with an offset, like %s: %w",
				in.At, d.local(d.clock.Now()), domain.ErrInvalid)
		}
	}
	subject := fmt.Sprintf("%s at %s", site.NMI, at.In(d.zone).Format("15:04 on 2 Jan"))
	envelope, err := d.store.GetCurrentEnvelope(ctx, site.ID, at)
	if err != nil {
		return site, envelope, subject, fmt.Errorf("site %s has no envelope at %s: %w", site.NMI, d.local(at), err)
	}
	return site, envelope, subject, nil
}

// envelopeView is what get_envelope returns.
type envelopeView struct {
	NMI                    string  `json:"nmi"`
	IntervalStart          string  `json:"interval_start"`
	IntervalEnd            string  `json:"interval_end"`
	ExportLimitW           float64 `json:"export_limit_w"`
	ImportLimitW           float64 `json:"import_limit_w"`
	SetBy                  string  `json:"set_by"`
	ConnectionExportLimitW float64 `json:"connection_export_limit_w"`
	ConnectionImportLimitW float64 `json:"connection_import_limit_w"`
}

func (d *desk) getEnvelope(ctx context.Context, input []byte) (string, any, error) {
	site, e, subject, err := d.siteAt(ctx, input)
	if err != nil {
		return subject, nil, err
	}
	return subject, envelopeView{
		NMI: site.NMI, IntervalStart: d.local(e.ValidFrom), IntervalEnd: d.local(e.ValidTo),
		ExportLimitW: e.ExportLimitW, ImportLimitW: e.ImportLimitW, SetBy: string(e.Source),
		ConnectionExportLimitW: site.ExportCapW, ConnectionImportLimitW: site.ImportCapW,
	}, nil
}

// bindingView is what get_binding_constraint returns.
type bindingView struct {
	NMI           string        `json:"nmi"`
	IntervalStart string        `json:"interval_start"`
	IntervalEnd   string        `json:"interval_end"`
	SetBy         string        `json:"set_by"`
	Export        limitView     `json:"export"`
	Import        limitView     `json:"import"`
	Backstop      *backstopView `json:"backstop,omitempty"`
	Feeder        *forecastView `json:"feeder_forecast,omitempty"`
}

type limitView struct {
	LimitW           float64 `json:"limit_w"`
	ConnectionLimitW float64 `json:"connection_limit_w"`
	Binding          string  `json:"binding"`
	Element          string  `json:"element,omitempty"`
}

type backstopView struct {
	Reason      string `json:"reason"`
	TriggeredBy string `json:"triggered_by"`
	TriggeredAt string `json:"triggered_at"`
}

type forecastView struct {
	NominalVoltageV          float64  `json:"nominal_voltage_v"`
	HighestVoltageNoLimitsPU float64  `json:"highest_voltage_pu_with_no_limits"`
	HighestVoltageEnvelopePU *float64 `json:"highest_voltage_pu_at_the_envelopes,omitempty"`
	HighestVoltageFixedPU    float64  `json:"highest_voltage_pu_at_a_fixed_limit"`
	LowestVoltagePU          float64  `json:"lowest_voltage_pu"`
	TransformerLoadingPct    float64  `json:"transformer_loading_pct"`
	ExportLimitTotalW        float64  `json:"export_limit_total_w"`
}

func (d *desk) getBindingConstraint(ctx context.Context, input []byte) (string, any, error) {
	site, e, subject, err := d.siteAt(ctx, input)
	if err != nil {
		return subject, nil, err
	}
	out := bindingView{
		NMI: site.NMI, IntervalStart: d.local(e.ValidFrom), IntervalEnd: d.local(e.ValidTo), SetBy: string(e.Source),
		Export: limitView{
			LimitW: e.ExportLimitW, ConnectionLimitW: site.ExportCapW,
			Binding: string(e.ExportBinding), Element: e.ExportBindingElement,
		},
		Import: limitView{
			LimitW: e.ImportLimitW, ConnectionLimitW: site.ImportCapW,
			Binding: string(e.ImportBinding), Element: e.ImportBindingElement,
		},
	}
	if e.BackstopEventID != nil {
		event, err := d.store.GetBackstopEvent(ctx, *e.BackstopEventID)
		if err != nil {
			return subject, nil, fmt.Errorf("the backstop of the envelope: %w", err)
		}
		out.Backstop = &backstopView{Reason: event.Reason, TriggeredBy: event.TriggeredBy, TriggeredAt: d.local(event.TriggeredAt)}
	}
	intervals, err := d.store.ListFeederIntervals(ctx, d.feeder.ID, e.ValidFrom, e.ValidTo)
	if err != nil {
		return subject, nil, err
	}
	if len(intervals) > 0 {
		i := intervals[0]
		out.Feeder = &forecastView{
			NominalVoltageV:          d.feeder.NominalVoltageV,
			HighestVoltageNoLimitsPU: i.ForecastVMaxPU, HighestVoltageEnvelopePU: i.EnvelopeVMaxPU,
			HighestVoltageFixedPU: i.StaticVMaxPU, LowestVoltagePU: i.ForecastVMinPU,
			TransformerLoadingPct: i.ForecastLoadingPct, ExportLimitTotalW: i.ExportLimitTotalW,
		}
	}
	return subject, out, nil
}

// alertsView is what list_breaches returns.
type alertsView struct {
	Alerts []alertView `json:"alerts"`
	// More says that older alerts were left out.
	More       bool `json:"more"`
	OpenOnFeed int  `json:"open_alerts_on_feeder"`
}

type alertView struct {
	NMI        string   `json:"nmi"`
	Kind       string   `json:"kind"`
	Severity   string   `json:"severity"`
	OpenedAt   string   `json:"opened_at"`
	ResolvedAt string   `json:"resolved_at,omitempty"`
	Open       bool     `json:"open"`
	LimitW     *float64 `json:"limit_w,omitempty"`
	PeakW      *float64 `json:"peak_w,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}

// alertsPage is how many alerts one lookup returns.
const alertsPage = 20

func (d *desk) listBreaches(ctx context.Context, input []byte) (string, any, error) {
	var in struct {
		NMI      string `json:"nmi"`
		OpenOnly bool   `json:"open_only"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", nil, fmt.Errorf("the input is not a JSON object with nmi and open_only: %w", domain.ErrInvalid)
	}
	subject := "alerts"
	if in.OpenOnly {
		subject = "open alerts"
	}
	filter := AlertFilter{OpenOnly: in.OpenOnly}
	if in.NMI != "" {
		subject += " of " + in.NMI
		site, err := d.site(ctx, in.NMI)
		if err != nil {
			return subject, nil, err
		}
		filter.SiteID = &site.ID
	}
	sites, err := d.store.ListAllSites(ctx, d.feeder.ID)
	if err != nil {
		return subject, nil, err
	}
	nmis := make(map[uuid.UUID]string, len(sites))
	for _, site := range sites {
		nmis[site.ID] = site.NMI
	}
	alerts, next, err := d.store.ListAlerts(ctx, d.feeder.ID, filter, domain.Page{Size: alertsPage})
	if err != nil {
		return subject, nil, err
	}
	out := alertsView{Alerts: make([]alertView, len(alerts)), More: next != ""}
	for i, a := range alerts {
		view := alertView{
			// A site that was deleted since has no NMI to show.
			NMI: nmis[a.SiteID], Kind: string(a.Kind), Severity: string(a.Severity),
			OpenedAt: d.local(a.OpenedAt), Open: a.ResolvedAt == nil,
			LimitW: a.LimitW, PeakW: a.PeakW, Detail: a.Detail,
		}
		if a.ResolvedAt != nil {
			view.ResolvedAt = d.local(*a.ResolvedAt)
		}
		out.Alerts[i] = view
	}
	if out.OpenOnFeed, err = d.store.CountOpenAlerts(ctx, d.feeder.ID); err != nil {
		return subject, nil, err
	}
	return subject, out, nil
}

// configView is what get_config returns.
type configView struct {
	Version             int32   `json:"version"`
	Policy              string  `json:"policy"`
	VMinPU              float64 `json:"v_min_pu"`
	VMaxPU              float64 `json:"v_max_pu"`
	NominalVoltageV     float64 `json:"nominal_voltage_v"`
	TransformerKVA      float64 `json:"transformer_kva"`
	TransformerLimitPct float64 `json:"transformer_limit_pct"`
	LineLimitPct        float64 `json:"line_limit_pct"`
	PVScale             float64 `json:"pv_scale"`
	FixedLimitW         float64 `json:"fixed_limit_w"`
	IntervalMinutes     int32   `json:"interval_minutes"`
	HorizonIntervals    int32   `json:"horizon_intervals"`
	BreachGraceSeconds  int32   `json:"breach_grace_seconds"`
	OfflineAfterSeconds int32   `json:"offline_after_seconds"`
	Note                string  `json:"note,omitempty"`
	SavedBy             string  `json:"saved_by"`
}

func (d *desk) getConfig(ctx context.Context, _ []byte) (string, any, error) {
	c, err := d.store.GetActiveEnvelopeConfig(ctx, d.feeder.ID)
	if err != nil {
		return "the config", nil, fmt.Errorf("feeder %s has no config: %w", d.feeder.Code, err)
	}
	return fmt.Sprintf("config version %d", c.Version), configView{
		Version: c.Version, Policy: string(c.Policy), VMinPU: c.VMinPU, VMaxPU: c.VMaxPU,
		NominalVoltageV: d.feeder.NominalVoltageV, TransformerKVA: d.feeder.TransformerKVA,
		TransformerLimitPct: c.TransformerLimitPct, LineLimitPct: c.LineLimitPct,
		PVScale: c.PVScale, FixedLimitW: c.StaticLimitW,
		IntervalMinutes: c.IntervalMinutes, HorizonIntervals: c.HorizonIntervals,
		BreachGraceSeconds: c.BreachGraceSeconds, OfflineAfterSeconds: c.OfflineAfterSeconds,
		Note: c.Note, SavedBy: c.CreatedBy,
	}, nil
}
