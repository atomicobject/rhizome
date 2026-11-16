package agentchat

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func options(names ...string) []harness.ModelOption {
	models := make([]harness.ModelOption, 0, len(names))
	for _, name := range names {
		models = append(models, harness.ModelOption{ID: name, DisplayName: name})
	}
	return models
}

func featured(models []harness.ModelOption) (names []string, defaults []string) {
	for _, model := range models {
		if model.Featured {
			names = append(names, model.DisplayName)
		}
		if model.Default {
			defaults = append(defaults, model.DisplayName)
		}
	}
	return names, defaults
}

// The lists Claude Code 2.1 and Codex reported on 2026-11-16, out of version order.
func TestCurateModelsFeaturesLatestOfEachFamily(t *testing.T) {
	claude := options("Opus 4.8", "Fable 5.2", "Sonnet 5.5", "Haiku 5.5", "Fable 5.1", "Opus 5.5", "Sonnet 5", "Opus 5", "Fable 5", "Opus 4.10", "Sonnet 4.6")
	claude[0].Default = true
	names, defaults := featured(curateModels(harness.KindClaude, claude))
	require.Equal(t, []string{"Fable 5.2", "Sonnet 5.5", "Opus 5.5"}, names)
	require.Equal(t, []string{"Opus 5.5"}, defaults, "the latest Opus is the default, whatever the CLI says")

	codex := options("GPT-6-Astra", "GPT-6.1 Terra", "GPT-6-Sol", "GPT-6.1-Sol", "GPT-6-Luna", "GPT-5.6-Sol", "GPT-5.6-Terra", "Daybreak Blue")
	names, defaults = featured(curateModels(harness.KindCodex, codex))
	require.Equal(t, []string{"GPT-6-Astra", "GPT-6.1 Terra", "GPT-6.1-Sol"}, names)
	require.Equal(t, []string{"GPT-6.1-Sol"}, defaults)
}

// Signed out, Claude Code lists only its aliases, which name the latest models.
func TestCurateModelsTreatsABareFamilyNameAsTheLatest(t *testing.T) {
	names, defaults := featured(curateModels(harness.KindClaude, options("Opus 5", "Opus", "Fable", "Sonnet", "Haiku")))
	require.Equal(t, []string{"Opus", "Fable", "Sonnet"}, names)
	require.Equal(t, []string{"Opus"}, defaults)
}

func TestCurateModelsKeepsTheAgentDefaultWithoutAKnownFamily(t *testing.T) {
	models := options("gpt-test", "other")
	models[1].Default = true
	curated := curateModels(harness.KindCodex, models)
	names, defaults := featured(curated)
	require.Empty(t, names)
	require.Equal(t, []string{"other"}, defaults)
	require.False(t, models[0].Featured || models[0].Default, "the driver's list is not changed")
}
