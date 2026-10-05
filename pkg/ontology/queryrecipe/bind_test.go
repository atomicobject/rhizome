package queryrecipe

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBindVariablesDiagnosticOrderIsStable(t *testing.T) {
	recipe := Recipe{InputSpec: InputSpec{Inputs: []Input{{Name: "z", Required: true}, {Name: "a", Required: true}}}}
	for i := 0; i < 100; i++ {
		_, _, issues := BindVariables(recipe, map[string]string{"y": "", "b": ""})
		require.Len(t, issues, 2)
		require.Contains(t, issues[0].Message, "input b ")
		require.Contains(t, issues[1].Message, "input y ")
		_, _, issues = BindVariables(recipe, nil)
		require.Len(t, issues, 1)
		require.Contains(t, issues[0].Message, "input a ")
	}
}
