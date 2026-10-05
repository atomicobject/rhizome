package mcp

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestExternalReferencesToolReturnsExplicitPathlessTarget(t *testing.T) {
	ctx := context.Background()
	store := newIntelStore(t)
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", SymbolPath: "useState", Kind: codeanchor.ExternalTargetSymbol})
	require.NoError(t, err)
	ref := codeanchor.SymbolRefRow{SrcPath: "src/app.ts", OwnerFQN: "src.App", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/app.ts": {ref}}))
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{"src/app.ts": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
		OwnerFQN: ref.OwnerFQN, RefKind: ref.RefKind, Raw: codeanchor.RawSymbolTargetKey{DstLang: ref.DstLang, DstPkg: ref.DstPkg, DstName: ref.DstName, DstFQN: ref.DstFQN}, Target: target,
		Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh},
	}}}}))

	res, err := ExternalReferencesTool(Config{IntelStore: store})(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "external_references", Arguments: map[string]any{"handle": target.Handle}}})
	require.NoError(t, err)
	var payload ExternalReferencesResponse
	decodeToolResult(t, res, &payload)
	require.Equal(t, "resolved", payload.Status)
	require.NotNil(t, payload.Target)
	require.True(t, payload.Target.External)
	require.True(t, payload.Target.Pathless)
	require.False(t, payload.Target.Indexed)
	require.False(t, payload.Target.SourceBacked)
	require.False(t, payload.Target.SourceAvailable)
	require.Len(t, payload.Calls, 1)
}

func TestExternalReferencesToolReturnsStructuredInputError(t *testing.T) {
	// An available store is required so the request reaches input validation
	// instead of stopping at the missing-index diagnostic.
	res, err := ExternalReferencesTool(Config{IntelStore: newIntelStore(t)})(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "external_references", Arguments: map[string]any{"ecosystem": "npm"}},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "structured external reference query requires ecosystem and module", res.Content[0].(mcp.TextContent).Text)
}
