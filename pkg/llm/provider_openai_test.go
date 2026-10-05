package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToOpenAIInputUsesOutputTextForAssistantMessages(t *testing.T) {
	t.Parallel()

	input := toOpenAIInput([]Message{
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
	})

	require.Len(t, input, 2)
	user := input[0].(openAIMessage)
	assistant := input[1].(openAIMessage)
	require.Equal(t, "input_text", user.Content[0].Type)
	require.Equal(t, "output_text", assistant.Content[0].Type)
}
