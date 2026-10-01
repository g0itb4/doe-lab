package server_test

import (
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/apitest"
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

func (a *api) assistant() doelabv1connect.AssistantServiceClient {
	return doelabv1connect.NewAssistantServiceClient(a.HTTP, a.URL)
}

// ask puts a question and returns every step of the answer.
func (a *api) ask(msg *doelabv1.AskRequest) ([]*doelabv1.AskResponse, error) {
	stream, err := a.assistant().Ask(ctx, req(msg))
	if err != nil {
		return nil, err
	}
	var steps []*doelabv1.AskResponse
	for stream.Receive() {
		steps = append(steps, stream.Msg())
	}
	return steps, stream.Err()
}

func TestAssistantAnswersOverTheWire(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	if _, err := a.Store.CreateEnvelopeConfig(repotest.Ctx(), repotest.Config(a.fixture.Feeder.ID)); err != nil {
		t.Fatal(err)
	}
	nmi := a.fixture.SiteA.NMI

	// Anyone may ask, with no token.
	status, err := a.assistant().GetAssistantStatus(ctx, req(&doelabv1.GetAssistantStatusRequest{}))
	noErr(t, "status", err)
	if !status.Msg.GetAvailable() || status.Msg.GetUnavailable() != doelabv1.AssistantUnavailable_ASSISTANT_UNAVAILABLE_UNSPECIFIED ||
		status.Msg.GetMaxQuestionChars() != 500 {
		t.Errorf("status = %v", status.Msg)
	}

	// A lookup that exists, one that does not, and then the answer in two
	// pieces.
	a.Model.Play(
		apitest.Step{Turn: service.ModelTurn{Stop: service.StopLookups, Calls: []service.ToolCall{
			apitest.Call("call-1", "get_config", `{}`),
			apitest.Call("call-2", "trigger_backstop", `{}`),
		}}},
		apitest.Step{Text: []string{"Capacity is shared ", "equally."}, Turn: service.ModelTurn{Stop: service.StopAnswered}},
	)
	steps, err := a.ask(&doelabv1.AskRequest{FeederCode: "LV10", Question: "How is capacity shared?", Nmi: &nmi})
	noErr(t, "ask", err)
	if len(steps) != 5 {
		t.Fatalf("steps = %v", steps)
	}
	if l := steps[0].GetLookup(); l.GetTool() != doelabv1.AssistantTool_ASSISTANT_TOOL_GET_CONFIG || l.GetSubject() != "config version 1" || !l.GetFound() {
		t.Errorf("the first lookup = %v", l)
	}
	if l := steps[1].GetLookup(); l.GetTool() != doelabv1.AssistantTool_ASSISTANT_TOOL_UNSPECIFIED || l.GetSubject() != "trigger_backstop" || l.GetFound() {
		t.Errorf("the lookup that does not exist = %v", l)
	}
	if steps[2].GetText()+steps[3].GetText() != "Capacity is shared equally." || steps[4].GetEnd() != doelabv1.AnswerEnd_ANSWER_END_COMPLETE {
		t.Errorf("the answer = %v", steps[2:])
	}
	if briefs := a.Model.Briefs(); len(briefs) != 1 || !strings.Contains(briefs[0].Question, "The asker has site "+nmi+" on screen.") {
		t.Errorf("briefs = %+v", briefs)
	}

	// The other ends reach the client as they are.
	for stop, want := range map[service.ModelStop]doelabv1.AnswerEnd{
		service.StopLength:   doelabv1.AnswerEnd_ANSWER_END_CUT_SHORT,
		service.StopDeclined: doelabv1.AnswerEnd_ANSWER_END_DECLINED,
	} {
		a.Model.Play(apitest.Step{Turn: service.ModelTurn{Stop: stop}})
		steps, err := a.ask(&doelabv1.AskRequest{FeederCode: "LV10", Question: "?"})
		if err != nil || len(steps) != 1 || steps[0].GetEnd() != want {
			t.Errorf("stop %d: %v, %v", stop, steps, err)
		}
	}
}

func TestAssistantRefusalsOverTheWire(t *testing.T) {
	t.Parallel()
	a := newAPI(t)

	// The shape of a question is checked before anything is spent on it.
	for name, msg := range map[string]*doelabv1.AskRequest{
		"no question":         {FeederCode: "LV10"},
		"a question too long": {FeederCode: "LV10", Question: strings.Repeat("why ", 126)},
		"a feeder code":       {FeederCode: "lv 10", Question: "?"},
		"an NMI":              {FeederCode: "LV10", Question: "?", Nmi: repotest.Ptr("this one")},
	} {
		_, err := a.ask(msg)
		wantCode(t, name, err, connect.CodeInvalidArgument)
	}
	_, err := a.ask(&doelabv1.AskRequest{FeederCode: "LV99", Question: "?"})
	wantCode(t, "no such feeder", err, connect.CodeNotFound)
	if len(a.Model.Briefs()) != 0 {
		t.Errorf("the model was started %d times", len(a.Model.Briefs()))
	}

	// The provider fails: the client is told to try again, and nothing more.
	a.Assistant.PerMinute = 1
	a.Model.Play(apitest.Step{Err: fmt.Errorf("the assistant could not answer just now: %w", domain.ErrRetryable)})
	_, err = a.ask(&doelabv1.AskRequest{FeederCode: "LV10", Question: "?"})
	wantCode(t, "the model fails", err, connect.CodeUnavailable)

	// One question a minute, and this client has had it.
	_, err = a.ask(&doelabv1.AskRequest{FeederCode: "LV10", Question: "?"})
	wantCode(t, "over the ration", err, connect.CodeResourceExhausted)

	// Nothing left of the day's budget.
	a.Assistant.DailyBudgetMicroUSD = 0
	status, err := a.assistant().GetAssistantStatus(ctx, req(&doelabv1.GetAssistantStatusRequest{}))
	noErr(t, "status", err)
	if status.Msg.GetAvailable() || status.Msg.GetUnavailable() != doelabv1.AssistantUnavailable_ASSISTANT_UNAVAILABLE_BUDGET_SPENT {
		t.Errorf("status with no budget = %v", status.Msg)
	}
	_, err = a.ask(&doelabv1.AskRequest{FeederCode: "LV10", Question: "?"})
	wantCode(t, "over the budget", err, connect.CodeResourceExhausted)
}
