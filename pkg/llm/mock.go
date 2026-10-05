package llm

import "context"

// MockProvider is a test double for LLM providers.
type MockProvider struct {
	Responses []Response
	Calls     []Request
	Err       error
}

func (m *MockProvider) Complete(_ context.Context, req Request) (Response, error) {
	m.Calls = append(m.Calls, req)
	if m.Err != nil {
		return Response{}, m.Err
	}
	if len(m.Responses) == 0 {
		return Response{}, nil
	}
	resp := m.Responses[0]
	m.Responses = m.Responses[1:]
	return resp, nil
}
