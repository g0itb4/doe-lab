// Package llm is the language model behind the assistant: it implements
// service.Model over the Anthropic Messages API.
//
// It owns everything that is the provider's: the SDK, the wire types, the
// model's name and its prices. The service above it sees turns, lookups and
// a cost, and would not change if the provider did.
package llm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"doelab/api/internal/domain"
	"doelab/api/internal/service"
)

// model is the one model the assistant uses. Its prices are below; change
// them together.
const model = "claude-opus-5-5"

// Prices, in millionths of a US dollar for a million tokens.
const (
	inputPrice      = 4_000_000
	outputPrice     = 20_000_000
	cacheReadPrice  = 200_000
	cacheWritePrice = 5_000_000
)

// Client talks to the model.
type Client struct {
	api anthropic.Client
	log *slog.Logger
}

// New builds a client with an API key. opts are for a test, which points the
// client at its own server.
func New(apiKey string, log *slog.Logger, opts ...option.RequestOption) *Client {
	opts = append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	return &Client{api: anthropic.NewClient(opts...), log: log}
}

var _ service.Model = (*Client)(nil)

// Start opens a conversation. Nothing is sent until its first Next.
func (c *Client) Start(brief service.ModelBrief) service.Conversation {
	tools := make([]anthropic.ToolUnionParam, len(brief.Tools))
	for i, t := range brief.Tools {
		tools[i] = anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: t.Properties, Required: t.Required},
		}}
	}
	return &conversation{client: c, params: anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: int64(brief.MaxTokens),
		System:    []anthropic.TextBlockParam{{Text: brief.System}},
		Tools:     tools,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(brief.Question))},
		// This model always reasons before it answers; effort is how much.
		// The questions are small and the lookups do the work, so low: a
		// shorter wait and a smaller bill.
		OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow},
		// One breakpoint at the end of the request, which the provider moves
		// forward each turn: the turns of a question re-read the turns
		// before at the cache's price.
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}}
}

// conversation is the history of one question, in the provider's own types.
// The model's reasoning is part of it and goes back unchanged with each
// turn, which the provider requires.
type conversation struct {
	client *Client
	params anthropic.MessageNewParams
}

// Next runs one turn.
func (v *conversation) Next(ctx context.Context, results []service.ToolResult, onText func(string)) (service.ModelTurn, error) {
	if len(results) > 0 {
		blocks := make([]anthropic.ContentBlockParamUnion, len(results))
		for i, r := range results {
			blocks[i] = anthropic.NewToolResultBlock(r.CallID, r.Content, r.IsError)
		}
		v.params.Messages = append(v.params.Messages, anthropic.NewUserMessage(blocks...))
	}

	stream := v.client.api.Messages.NewStreaming(ctx, v.params)
	defer func() { _ = stream.Close() }()
	var message anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return service.ModelTurn{}, v.failed(ctx, err)
		}
		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok {
				onText(text.Text)
			}
		}
	}
	turn := service.ModelTurn{CostMicroUSD: cost(message.Usage)}
	if err := stream.Err(); err != nil {
		// What arrived before the failure was still paid for.
		return turn, v.failed(ctx, err)
	}

	switch message.StopReason {
	case anthropic.StopReasonRefusal:
		// A refused turn is not sent back: the conversation ends here.
		turn.Stop = service.StopDeclined
		return turn, nil
	case anthropic.StopReasonMaxTokens:
		turn.Stop = service.StopLength
		return turn, nil
	}
	v.params.Messages = append(v.params.Messages, message.ToParam())
	for _, block := range message.Content {
		if call, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			turn.Calls = append(turn.Calls, service.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input})
		}
	}
	if message.StopReason == anthropic.StopReasonToolUse && len(turn.Calls) > 0 {
		turn.Stop = service.StopLookups
	}
	return turn, nil
}

// failed logs what the provider said and returns an error that says none of
// it: the text of a provider's error can carry the request, and the caller's
// message goes to the client.
func (v *conversation) failed(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		// The asker left, or the answer ran out of time: not the provider's
		// failure.
		return ctx.Err()
	}
	v.client.log.WarnContext(ctx, "the model failed", "model", model, "err", err)
	return fmt.Errorf("the assistant could not answer just now: %w", domain.ErrRetryable)
}

// cost prices the tokens of one turn, in millionths of a US dollar.
func cost(u anthropic.Usage) int64 {
	return (u.InputTokens*inputPrice +
		u.OutputTokens*outputPrice +
		u.CacheReadInputTokens*cacheReadPrice +
		u.CacheCreationInputTokens*cacheWritePrice) / 1_000_000
}
