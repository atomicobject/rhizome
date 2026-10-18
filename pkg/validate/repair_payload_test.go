package validate

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepairOperationJSONOmitsSourcePayloads(t *testing.T) {
	const source = "private complete source body"
	encoded, err := json.Marshal(RepairOperation{
		ID: "op", Kind: RepairOperationWrite, Path: "note.md", SourceHash: SourceHash([]byte(source)), Content: []byte(source),
		Expected:         []ExpectedText{{StartByte: 0, EndByte: len(source), Text: source, Replacement: source + " updated"}},
		DestinationState: &RepairDestinationState{Kind: RepairDestinationOccupied, Content: []byte(source)},
	})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), source)
	require.NotContains(t, string(encoded), base64.StdEncoding.EncodeToString([]byte(source)))
	require.NotContains(t, string(encoded), `"content"`)
	require.NotContains(t, string(encoded), `"text"`)
	require.NotContains(t, string(encoded), `"replacement"`)
	require.Contains(t, string(encoded), `"sourceHash"`)
}
