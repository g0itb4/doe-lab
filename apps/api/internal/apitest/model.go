package apitest

import (
	"context"
	"sync"

	"doelab/api/internal/service"
)

// Model is a scripted language model: each turn of a conversation plays the
// next step of Script, whatever was asked. It records what it was given, so
// a test can check what the assistant sent.
type Model struct {
	mu     sync.Mutex
	script []Step
	briefs []service.ModelBrief
	// results holds, for each turn played, the results it was given.
	results [][]service.ToolResult
}

// Step is one turn of a script: the pieces of text the model writes, then
// how the turn ends.
type Step struct {
	Text []string
	Turn service.ModelTurn
	Err  error
}

// Play replaces the script.
func (m *Model) Play(steps ...Step) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.script = steps
}

// Briefs returns the brief of every conversation started.
func (m *Model) Briefs() []service.ModelBrief {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]service.ModelBrief(nil), m.briefs...)
}

// Results returns, for each turn played, the results it was given.
func (m *Model) Results() [][]service.ToolResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]service.ToolResult(nil), m.results...)
}

// Start opens a conversation on the script.
func (m *Model) Start(brief service.ModelBrief) service.Conversation {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.briefs = append(m.briefs, brief)
	return scripted{m}
}

type scripted struct{ m *Model }

// Next plays the next step. A script that has run out answers with nothing.
func (s scripted) Next(_ context.Context, results []service.ToolResult, onText func(string)) (service.ModelTurn, error) {
	s.m.mu.Lock()
	s.m.results = append(s.m.results, append([]service.ToolResult(nil), results...))
	var step Step
	if len(s.m.script) > 0 {
		step, s.m.script = s.m.script[0], s.m.script[1:]
	}
	s.m.mu.Unlock()

	for _, text := range step.Text {
		onText(text)
	}
	return step.Turn, step.Err
}

// Call is a lookup for a script: the tool, and its input as JSON.
func Call(id, name, input string) service.ToolCall {
	return service.ToolCall{ID: id, Name: name, Input: []byte(input)}
}
