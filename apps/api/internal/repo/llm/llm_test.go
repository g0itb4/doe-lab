package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/llm"
	"doelab/api/internal/service"
)

// provider stands in for the Messages API: it answers each request with the
// next reply of its script, as a stream of server-sent events, and keeps
// what it was sent.
type provider struct {
	t      *testing.T
	server *httptest.Server
	log    *bytes.Buffer

	mu       sync.Mutex
	replies  []reply
	requests []map[string]any
	keys     []string
}

// reply is one response: a status other than 200 with a body, or events.
type reply struct {
	status int
	body   string
	events []string
}

func newProvider(t *testing.T, replies ...reply) (*provider, *llm.Client) {
	t.Helper()
	p := &provider{t: t, log: &bytes.Buffer{}, replies: replies}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	log := slog.New(slog.NewTextHandler(p.log, nil))
	// No retries: a test that expects a failure should see it once.
	return p, llm.New("key-for-tests", log, option.WithBaseURL(p.server.URL), option.WithMaxRetries(0))
}

func (p *provider) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil || r.URL.Path != "/v1/messages" {
		p.t.Errorf("request to %s: %v", r.URL.Path, err)
	}
	p.mu.Lock()
	p.requests = append(p.requests, request)
	p.keys = append(p.keys, r.Header.Get("X-Api-Key"))
	var next reply
	if len(p.replies) > 0 {
		next, p.replies = p.replies[0], p.replies[1:]
	}
	p.mu.Unlock()

	if next.status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(next.status)
		_, _ = io.WriteString(w, next.body)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range next.events {
		var kind struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(event), &kind)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind.Type, event)
	}
}

func (p *provider) request(i int) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i >= len(p.requests) {
		p.t.Fatalf("request %d was never sent; there were %d", i, len(p.requests))
	}
	return p.requests[i]
}

// The events of a stream.
func start(inputTokens, cacheRead, cacheWrite int) string {
	return fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%d,"output_tokens":1,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		inputTokens, cacheRead, cacheWrite)
}

func thinking(index int, signature string) []string {
	return []string{
		fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"thinking","thinking":"","signature":""}}`, index),
		fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"signature_delta","signature":%q}}`, index, signature),
		fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index),
	}
}

func text(index int, pieces ...string) []string {
	out := []string{fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, index)}
	for _, piece := range pieces {
		out = append(out, fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%q}}`, index, piece))
	}
	return append(out, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
}

func toolUse(index int, id, name, input string) []string {
	return []string{
		fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`, index, id, name),
		fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%q}}`, index, input),
		fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index),
	}
}

func stop(reason string, outputTokens int) []string {
	return []string{
		fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},"usage":{"output_tokens":%d}}`, reason, outputTokens),
		`{"type":"message_stop"}`,
	}
}

func events(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

var brief = service.ModelBrief{
	System: "Answer in plain text.",
	Tools: []service.ModelTool{{
		Name: "get_envelope", Description: "The envelope of a site.",
		Properties: map[string]any{"nmi": map[string]any{"type": "string"}},
		Required:   []string{"nmi"},
	}},
	Question:  "Why is XDLAB000014 limited?",
	MaxTokens: 2048,
}

func TestConversationRunsTheLookupLoop(t *testing.T) {
	t.Parallel()
	p, client := newProvider(t,
		reply{events: events(
			[]string{start(1200, 300, 40)},
			thinking(0, "sig-1"),
			text(1, "Looking ", "that up."),
			toolUse(2, "toolu_1", "get_envelope", `{"nmi":"XDLAB000014"}`),
			stop("tool_use", 90),
		)},
		reply{events: events(
			[]string{start(1500, 0, 0)},
			text(0, "Voltage at the end of the street."),
			stop("end_turn", 30),
		)},
	)
	ctx := context.Background()
	conversation := client.Start(brief)

	// The first turn: text as it comes, then a lookup to make.
	var heard []string
	onText := func(piece string) { heard = append(heard, piece) }
	turn, err := conversation.Next(ctx, nil, onText)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Stop != service.StopLookups || len(turn.Calls) != 1 {
		t.Fatalf("turn = %+v", turn)
	}
	if call := turn.Calls[0]; call.ID != "toolu_1" || call.Name != "get_envelope" || string(call.Input) != `{"nmi":"XDLAB000014"}` {
		t.Errorf("call = %+v, input %s", call, call.Input)
	}
	if got := strings.Join(heard, "|"); got != "Looking |that up." {
		t.Errorf("heard %q", got)
	}
	// 1200 in at $4, 90 out at $20, 300 read at $0.20 and 40 written at $5,
	// each for a million: 4800 + 1800 + 60 + 200 millionths of a dollar.
	if turn.CostMicroUSD != 6860 {
		t.Errorf("cost = %d, want 6860", turn.CostMicroUSD)
	}

	first := p.request(0)
	if first["model"] != "claude-opus-5-5" || first["max_tokens"] != 2048.0 || first["stream"] != true {
		t.Errorf("request = model %v, max_tokens %v, stream %v", first["model"], first["max_tokens"], first["stream"])
	}
	if got := jsonOf(t, first["system"]); got != `[{"text":"Answer in plain text.","type":"text"}]` {
		t.Errorf("system = %s", got)
	}
	if got := jsonOf(t, first["tools"]); got != `[{"description":"The envelope of a site.","input_schema":{"properties":{"nmi":{"type":"string"}},"required":["nmi"],"type":"object"},"name":"get_envelope"}]` {
		t.Errorf("tools = %s", got)
	}
	if got := jsonOf(t, first["messages"]); got != `[{"content":[{"text":"Why is XDLAB000014 limited?","type":"text"}],"role":"user"}]` {
		t.Errorf("messages = %s", got)
	}
	if got := jsonOf(t, first["output_config"]); got != `{"effort":"low"}` {
		t.Errorf("output_config = %s", got)
	}
	if got := jsonOf(t, first["cache_control"]); got != `{"type":"ephemeral"}` {
		t.Errorf("cache_control = %s", got)
	}
	// Reasoning is on for this model whatever is sent, and nothing is.
	if _, sent := first["thinking"]; sent {
		t.Errorf("thinking = %v, want it left out", first["thinking"])
	}
	if p.keys[0] != "key-for-tests" {
		t.Errorf("api key = %q", p.keys[0])
	}

	// The second turn takes the result, and the model's own turn goes back
	// whole: its reasoning with the signature, its text, and the call.
	turn, err = conversation.Next(ctx, []service.ToolResult{{CallID: "toolu_1", Content: `{"export_limit_w":2100}`}}, onText)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Stop != service.StopAnswered || len(turn.Calls) != 0 || turn.CostMicroUSD != 6600 {
		t.Errorf("the last turn = %+v", turn)
	}
	if heard[len(heard)-1] != "Voltage at the end of the street." {
		t.Errorf("heard %q", heard)
	}
	messages, _ := p.request(1)["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %s", jsonOf(t, messages))
	}
	if got := jsonOf(t, messages[1]); got != `{"content":[{"signature":"sig-1","thinking":"","type":"thinking"},{"text":"Looking that up.","type":"text"},{"id":"toolu_1","input":{"nmi":"XDLAB000014"},"name":"get_envelope","type":"tool_use"}],"role":"assistant"}` {
		t.Errorf("the model's turn went back as %s", got)
	}
	if got := jsonOf(t, messages[2]); got != `{"content":[{"content":[{"text":"{\"export_limit_w\":2100}","type":"text"}],"is_error":false,"tool_use_id":"toolu_1","type":"tool_result"}],"role":"user"}` {
		t.Errorf("the result went back as %s", got)
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestConversationEnds(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]service.ModelStop{
		"end_turn":   service.StopAnswered,
		"max_tokens": service.StopLength,
		"refusal":    service.StopDeclined,
		// A lookup turn with no lookup in it is not one.
		"tool_use": service.StopAnswered,
	} {
		_, client := newProvider(t, reply{events: events([]string{start(100, 0, 0)}, text(0, "Well."), stop(reason, 10))})
		turn, err := client.Start(brief).Next(context.Background(), nil, func(string) {})
		if err != nil || turn.Stop != want || turn.CostMicroUSD != 600 {
			t.Errorf("%s: %+v, %v, want stop %d", reason, turn, err, want)
		}
	}
}

func TestErrorResultsAreMarked(t *testing.T) {
	t.Parallel()
	p, client := newProvider(t,
		reply{events: events([]string{start(100, 0, 0)}, toolUse(0, "toolu_1", "get_envelope", `{"nmi":"nowhere"}`), stop("tool_use", 10))},
		reply{events: events([]string{start(100, 0, 0)}, text(0, "There is no such site."), stop("end_turn", 10))},
	)
	conversation := client.Start(brief)
	if _, err := conversation.Next(context.Background(), nil, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := conversation.Next(context.Background(), []service.ToolResult{{CallID: "toolu_1", Content: "site not found", IsError: true}}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	messages, _ := p.request(1)["messages"].([]any)
	if got := jsonOf(t, messages[len(messages)-1]); !strings.Contains(got, `"is_error":true`) || !strings.Contains(got, "site not found") {
		t.Errorf("the result went back as %s", got)
	}
}

func TestProviderFailuresSayNothingOfTheProvider(t *testing.T) {
	t.Parallel()
	const secret = "request req_011CSHoEeqs5C35K2UUqR7Fy was too hot"

	for name, r := range map[string]reply{
		"an error status": {
			status: http.StatusInternalServerError,
			body:   `{"type":"error","error":{"type":"api_error","message":"` + secret + `"}}`,
		},
		"an error in the stream": {events: []string{
			start(1000, 0, 0),
			`{"type":"error","error":{"type":"overloaded_error","message":"` + secret + `"}}`,
		}},
		"a stream that makes no sense": {events: []string{
			start(1000, 0, 0),
			`{"type":"content_block_start","index":4,"content_block":{"type":"text","text":"` + secret + `"}}`,
		}},
	} {
		p, client := newProvider(t, r)
		turn, err := client.Start(brief).Next(context.Background(), nil, func(string) {})
		if !errors.Is(err, domain.ErrRetryable) || strings.Contains(err.Error(), "req_") {
			t.Errorf("%s: %v", name, err)
		}
		if turn.Stop != service.StopAnswered || len(turn.Calls) != 0 {
			t.Errorf("%s: turn = %+v", name, turn)
		}
		// The operator can still read what happened.
		if logged := p.log.String(); !strings.Contains(logged, "the model failed") {
			t.Errorf("%s: logged %q", name, logged)
		}
	}

	// What arrived before a stream broke was paid for.
	_, client := newProvider(t, reply{events: []string{
		start(1000, 0, 0),
		`{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`,
	}})
	turn, _ := client.Start(brief).Next(context.Background(), nil, func(string) {})
	if turn.CostMicroUSD != 4020 {
		t.Errorf("cost of a broken turn = %d, want 4020", turn.CostMicroUSD)
	}
}

func TestAnAskerWhoLeavesIsNotAProviderFailure(t *testing.T) {
	t.Parallel()
	p, client := newProvider(t, reply{events: events([]string{start(100, 0, 0)}, text(0, "Well."), stop("end_turn", 10))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Start(brief).Next(ctx, nil, func(string) {})
	if !errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrRetryable) {
		t.Errorf("a cancelled turn: %v", err)
	}
	if logged := p.log.String(); logged != "" {
		t.Errorf("logged %q", logged)
	}
}
