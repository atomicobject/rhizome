package agentcode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeclarationsRetainUnionsAndDynamicMapValues(t *testing.T) {
	tests := []struct{ schema, expected string }{
		{`{"anyOf":[{"type":"boolean"},{"type":"string","enum":["true","false","auto"]}]}`, `boolean | "true" | "false" | "auto"`},
		{`{"type":["string","null"]}`, `string | null`},
		{`{"type":"object","additionalProperties":{"type":"array","items":{"type":"string"}}}`, `{ [key: string]: string[]; }`},
		{`{"oneOf":[{"type":"object","properties":{"status":{"type":"string"}},"required":["status"]},{"type":"null"}]}`, `{ "status": string; } | null`},
	}
	for _, test := range tests {
		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(test.schema), &schema))
		actual, err := projectSchema(schema, nil)
		require.NoError(t, err)
		require.Equal(t, test.expected, actual)
	}
}
