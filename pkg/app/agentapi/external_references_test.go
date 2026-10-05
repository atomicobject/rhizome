package agentapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/stretchr/testify/require"
)

func TestExternalReferencesCallJSONMatchesSharedContract(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", Kind: codeanchor.ExternalTargetModule})
	require.NoError(t, err)
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(context.Background(), map[string]codeanchor.ExternalEvidenceBatch{"src/app.ts": {Imports: []codeanchor.ExternalImportEvidenceInput{{
		Module: "react", BindingOrdinal: 0, Target: target, Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMSideEffect, Confidence: codeanchor.ExternalConfidenceHigh},
	}}}}))
	payload, err := CallJSON(context.Background(), Config{IntelStore: store}, "external_references", map[string]any{"handle": target.Handle, "sessionId": "test-session"})
	require.NoError(t, err)
	var response mcp.ExternalReferencesResponse
	require.NoError(t, json.Unmarshal(payload, &response))
	require.Equal(t, "resolved", response.Status)
	require.Len(t, response.Imports, 1)
}
