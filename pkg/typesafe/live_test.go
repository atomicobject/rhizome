//go:build live

package typesafe_test

import (
	"context"
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/stretchr/testify/require"
)

// Explicit opt-in only. Uses synthetic context, never repository content.
func TestLiveSystemOne(t *testing.T) {
	if os.Getenv("TYPESAFE_LIVE_TEST") != "1" {
		t.Skip("set TYPESAFE_LIVE_TEST=1 and TYPESAFE_API_KEY to exercise the live API")
	}
	client, err := typesafe.NewClient(os.Getenv("TYPESAFE_API_KEY"))
	require.NoError(t, err)
	models, err := client.Models(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, models.Models)
	response, err := client.Evaluate(context.Background(), typesafe.Request{
		State: struct {
			Message string `json:"message"`
		}{"This is a synthetic test. The sky is blue."},
		Questions: map[string]typesafe.Question{
			"color":     typesafe.Choice{Instructions: "Which color is explicitly named?", Criteria: map[string]any{"blue": map[string]any{"color": "blue"}, "red": nil}},
			"clarity":   typesafe.Score{Instructions: []string{"Rate how explicit the color statement is."}, Criteria: []any{map[string]any{"description": "no color stated"}, "color explicitly stated"}},
			"synthetic": typesafe.Noul{Instructions: "Does the message call itself synthetic?", Criteria: &typesafe.NoulCriteria{True: map[string]any{"means": "explicitly synthetic"}, False: "not stated"}},
		},
	})
	require.NoError(t, err)
	_, err = response.Choice("color")
	require.NoError(t, err)
	_, err = response.Score("clarity")
	require.NoError(t, err)
	_, err = response.Noul("synthetic")
	require.NoError(t, err)
	t.Logf("verified model=%s, questions=%d, input_tokens=%d, output_tokens=%d", response.Model, len(response.Answers), response.Usage.InputTokens, response.Usage.OutputTokens)
}
