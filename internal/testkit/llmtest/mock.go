package llmtest

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/llm"
)

// MockProvider is a test double for LLM providers.
type MockProvider struct {
	Responses []llm.Response
	Calls     []llm.Request
	Err       error
}

func (m *MockProvider) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	m.Calls = append(m.Calls, req)
	if m.Err != nil {
		return llm.Response{}, m.Err
	}
	if len(m.Responses) == 0 {
		return llm.Response{}, nil
	}
	resp := m.Responses[0]
	m.Responses = m.Responses[1:]
	return resp, nil
}
