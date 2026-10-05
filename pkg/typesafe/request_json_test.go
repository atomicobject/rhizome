package typesafe

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestJSONRoundTrip(t *testing.T) {
	input := `{"state":{"notes":["one","two"]},"model":"jev-latest","questions":{"action":{"type":"choice","criteria":{"keep":null,"update":{"description":"outdated"}}},"priority":{"type":"score","criteria":["low","high"]},"stale":{"type":"noul","criteria":{"true":"outdated","false":"current"}}}}`
	var request Request
	require.NoError(t, json.Unmarshal([]byte(input), &request))
	require.IsType(t, &Choice{}, request.Questions["action"])
	require.IsType(t, &Score{}, request.Questions["priority"])
	require.IsType(t, &Noul{}, request.Questions["stale"])
	data, err := json.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, input, string(data))
	require.Error(t, json.Unmarshal([]byte(`{"state":"x","questions":{"bad":{"type":"noul","instructions":3}}}`), &request))
	require.Equal(t, "jev-latest", request.Model, "failed decoding leaves the receiver intact")
}
