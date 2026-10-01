package controller

import (
	"context"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/interceptor"
	"doelab/api/internal/service"
)

// Assistant serves doelab.v1.AssistantService.
type Assistant struct {
	svc *service.Assistant
	// trustProxy says whether X-Forwarded-For names the asker: see
	// interceptor.ClientIP.
	trustProxy bool
}

// NewAssistant builds the handler.
func NewAssistant(svc *service.Assistant, trustProxy bool) *Assistant {
	return &Assistant{svc: svc, trustProxy: trustProxy}
}

var _ doelabv1connect.AssistantServiceHandler = (*Assistant)(nil)

var assistantUnavailable = map[service.AssistantUnavailable]doelabv1.AssistantUnavailable{
	service.AssistantOff:         doelabv1.AssistantUnavailable_ASSISTANT_UNAVAILABLE_OFF,
	service.AssistantBudgetSpent: doelabv1.AssistantUnavailable_ASSISTANT_UNAVAILABLE_BUDGET_SPENT,
}

var assistantTools = map[service.AssistantTool]doelabv1.AssistantTool{
	service.ToolGetEnvelope:          doelabv1.AssistantTool_ASSISTANT_TOOL_GET_ENVELOPE,
	service.ToolGetBindingConstraint: doelabv1.AssistantTool_ASSISTANT_TOOL_GET_BINDING_CONSTRAINT,
	service.ToolListBreaches:         doelabv1.AssistantTool_ASSISTANT_TOOL_LIST_BREACHES,
	service.ToolGetConfig:            doelabv1.AssistantTool_ASSISTANT_TOOL_GET_CONFIG,
}

var answerEnds = map[service.AnswerEnd]doelabv1.AnswerEnd{
	service.EndComplete:       doelabv1.AnswerEnd_ANSWER_END_COMPLETE,
	service.EndCutShort:       doelabv1.AnswerEnd_ANSWER_END_CUT_SHORT,
	service.EndDeclined:       doelabv1.AnswerEnd_ANSWER_END_DECLINED,
	service.EndTooManyLookups: doelabv1.AnswerEnd_ANSWER_END_TOO_MANY_LOOKUPS,
}

// GetAssistantStatus says whether the assistant can be asked.
func (c *Assistant) GetAssistantStatus(context.Context, *connect.Request[doelabv1.GetAssistantStatusRequest]) (*connect.Response[doelabv1.GetAssistantStatusResponse], error) {
	status := c.svc.Status()
	return connect.NewResponse(&doelabv1.GetAssistantStatusResponse{
		Available: status.Available, Unavailable: assistantUnavailable[status.Reason],
		MaxQuestionChars: service.MaxQuestionChars,
	}), nil
}

// Ask streams the answer to one question.
func (c *Assistant) Ask(ctx context.Context, req *connect.Request[doelabv1.AskRequest], stream *connect.ServerStream[doelabv1.AskResponse]) error {
	question := service.Question{
		FeederCode: req.Msg.GetFeederCode(), Text: req.Msg.GetQuestion(), NMI: req.Msg.GetNmi(),
		Client: interceptor.ClientIP(req.Peer().Addr, req.Header(), c.trustProxy),
	}
	return c.svc.Ask(ctx, question, func(step service.AnswerStep) error {
		switch {
		case step.Lookup != nil:
			return stream.Send(&doelabv1.AskResponse{Step: &doelabv1.AskResponse_Lookup{Lookup: &doelabv1.AssistantLookup{
				Tool: assistantTools[step.Lookup.Tool], Subject: step.Lookup.Subject, Found: step.Lookup.Found,
			}}})
		case step.End != "":
			return stream.Send(&doelabv1.AskResponse{Step: &doelabv1.AskResponse_End{End: answerEnds[step.End]}})
		default:
			return stream.Send(&doelabv1.AskResponse{Step: &doelabv1.AskResponse_Text{Text: step.Text}})
		}
	})
}
