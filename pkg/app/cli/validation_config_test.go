package actions

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestValidationSuiteConfigFromLocalCopiesEveryOverlay(t *testing.T) {
	t.Parallel()

	local := obsidian.LocalValidationConfig{
		Default: obsidian.LocalValidationSuiteConfig{
			Add:  []string{"link-hygiene", "views"},
			Skip: []string{"broken-links"},
		},
		All: obsidian.LocalValidationSuiteConfig{
			Add:  []string{"frozen-scope-drift"},
			Skip: []string{"code-anchors", "companion-docs"},
		},
	}

	got := ValidationSuiteConfigFromLocal(local)

	require.Equal(t, validate.SuiteConfig{
		Default: validate.SuiteOverlay{
			Add:  []string{"link-hygiene", "views"},
			Skip: []string{"broken-links"},
		},
		All: validate.SuiteOverlay{
			Add:  []string{"frozen-scope-drift"},
			Skip: []string{"code-anchors", "companion-docs"},
		},
	}, got)

	local.Default.Add[0] = "source-mutated"
	local.All.Skip[0] = "source-mutated"
	require.Equal(t, "link-hygiene", got.Default.Add[0])
	require.Equal(t, "code-anchors", got.All.Skip[0])

	got.Default.Skip[0] = "result-mutated"
	got.All.Add[0] = "result-mutated"
	require.Equal(t, "broken-links", local.Default.Skip[0])
	require.Equal(t, "frozen-scope-drift", local.All.Add[0])
}

func TestValidationSuiteConfigFromLocalPreservesNilSlices(t *testing.T) {
	t.Parallel()

	got := ValidationSuiteConfigFromLocal(obsidian.LocalValidationConfig{})

	require.Nil(t, got.Default.Add)
	require.Nil(t, got.Default.Skip)
	require.Nil(t, got.All.Add)
	require.Nil(t, got.All.Skip)
}
