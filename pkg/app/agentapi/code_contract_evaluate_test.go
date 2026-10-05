package agentapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateContractDiscriminatesQuestions(t *testing.T) {
	for _, test := range []struct {
		input string
		valid bool
	}{
		{`{"state":{"content":"hello"},"questions":{"a":{"type":"choice","criteria":{"keep":null,"update":{"why":"stale"}}},"b":{"type":"score","criteria":["low","high"]},"c":{"type":"noul","instructions":"Relevant?"}}}`, true},
		{`{"state":["hello"],"questions":{"a":{"type":"noul","criteria":{"true":"yes","false":null}}}}`, true},
		{`{"state":null,"questions":{"a":{"type":"noul"}}}`, false},
		{`{"state":"hello","questions":{"a":{"type":"noul","criteria":null,"instructions":null}}}`, true},
		{`{"state":"hello","questions":{"a":{"type":"noul","instructions":true}}}`, false},
		{`{"state":"hello","questions":{"a":{"type":"choice","criteria":["low","high"]}}}`, false},
		{`{"state":"hello","questions":{"a":{"type":"score","criteria":{"low":"x"}}}}`, false},
		{`{"state":"hello","questions":{"a":{"type":"noul","criteria":{"typo":"yes"}}}}`, false},
		{`{"state":"hello","questions":{"a":{"type":"unknown"}}}`, false},
	} {
		var input map[string]any
		require.NoError(t, json.Unmarshal([]byte(test.input), &input))
		err := ValidateCodeInput("evaluate", input)
		if test.valid {
			require.NoError(t, err, test.input)
		} else {
			require.Error(t, err, test.input)
		}
	}
}
