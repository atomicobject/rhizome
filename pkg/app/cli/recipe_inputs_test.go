package actions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRecipeInputsJSONPreservesSupportedValues(t *testing.T) {
	got, err := ParseRecipeInputsJSON(`{
		" topics ": ["alpha", "beta"],
		"options": {"nested": true},
		"type": "SpecLike",
		"first": 3,
		"enabled": false,
		"nullable": null
	}`)
	require.NoError(t, err)
	require.JSONEq(t, `["alpha","beta"]`, got["topics"])
	require.JSONEq(t, `{"nested":true}`, got["options"])
	require.Equal(t, "SpecLike", got["type"])
	require.Equal(t, "3", got["first"])
	require.Equal(t, "false", got["enabled"])
	require.Empty(t, got["nullable"])
}

func TestParseRecipeInputsJSONRejectsEmptyTrimmedKey(t *testing.T) {
	_, err := ParseRecipeInputsJSON(`{"  ": "value"}`)
	require.EqualError(t, err, "input keys must be non-empty")
}
