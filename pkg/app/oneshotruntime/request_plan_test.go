package oneshotruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRequestPlanArgsClassifiesExistingNotesOutsideCommandLayer(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Alpha.md"), []byte("# Alpha\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Alpha.html"), []byte("<h1>Alpha</h1>\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600))
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	noteRuntime, err := builtin.NewRuntime()
	require.NoError(t, err)

	noteArgs := map[string]any{"inputs": []string{"Alpha.md"}}
	require.NoError(t, NormalizeRequestPlanArgs("agent.files", noteArgs, &vaultPaths, &noteRuntime))
	require.False(t, boolArg(noteArgs, FilesRequestPlanHasPathInputsArg))
	htmlArgs := map[string]any{"inputs": []string{"Alpha.html"}}
	require.NoError(t, NormalizeRequestPlanArgs("agent.files", htmlArgs, &vaultPaths, &noteRuntime))
	require.False(t, boolArg(htmlArgs, FilesRequestPlanHasPathInputsArg))

	for _, input := range []string{"main.go", "missing.go"} {
		codeArgs := map[string]any{"inputs": []string{input}}
		require.NoError(t, NormalizeRequestPlanArgs("agent.files", codeArgs, &vaultPaths, &noteRuntime))
		require.True(t, boolArg(codeArgs, FilesRequestPlanHasPathInputsArg), input)
	}
}

func TestRequestPlanDistinguishesUndeclaredOperationsFromInvalidInput(t *testing.T) {
	_, err := RequestPlan("agent.file-context", nil)
	require.ErrorIs(t, err, ErrRequestPlanNotDeclared)

	_, err = RequestPlan("agent.report", map[string]any{"op": "not-a-report"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRequestPlanNotDeclared)
}

func TestRequestPlanKeepsNormalizedSessionOnlyNoteOperationsBelowCodeIndex(t *testing.T) {
	plan, err := RequestPlan("agent.files", map[string]any{
		FilesRequestPlanHasPathInputsArg: false,
		"sessionId":                      "stable",
	})
	require.NoError(t, err)
	require.Equal(t, SessionExistingOnly, plan.Session)
	require.False(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.NoError(t, plan.Validate())
}

func TestSemanticQueryRequestPlanUsesNormalizedExistingSession(t *testing.T) {
	plan, err := RequestPlan("agent.semantic-query", map[string]any{"sessionId": "  stable-query-session  "})
	require.NoError(t, err)
	require.Equal(t, SessionExistingOnly, plan.Session)
	require.Contains(t, plan.Readiness, ReadinessSession)
	require.True(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySemantic))
	require.True(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))

	withoutSession, err := RequestPlan("agent.semantic-query", map[string]any{"sessionId": " \t "})
	require.NoError(t, err)
	require.Equal(t, SessionNone, withoutSession.Session)
	require.NotContains(t, withoutSession.Readiness, ReadinessSession)
}

func TestRequestPlanUsesCodeIndexForDepthZeroCodePathFiles(t *testing.T) {
	plan, err := RequestPlan("agent.files", map[string]any{
		FilesRequestPlanHasPathInputsArg: true,
	})
	require.NoError(t, err)
	require.True(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.Contains(t, plan.Readiness, ReadinessCodeIndex)
	require.Equal(t, StoreExistingReadOnly, plan.StoreAccess)
	require.Equal(t, NoteStateLive, plan.NoteState)
}

func TestRequestPlanUsesNormalizedContinuationDepthForFilesGraph(t *testing.T) {
	plan, err := RequestPlan("agent.files", map[string]any{
		FilesRequestPlanMaxDepthArg: 1,
	})
	require.NoError(t, err)
	require.True(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySearch))
	require.True(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.Contains(t, plan.Readiness, ReadinessSearch)
	require.Contains(t, plan.Readiness, ReadinessCodeIndex)
}

func TestRequestPlanKeepsNormalizedZeroContinuationDepthAuthoritative(t *testing.T) {
	plan, err := RequestPlan("agent.files", map[string]any{
		FilesRequestPlanMaxDepthArg:      0,
		FilesRequestPlanHasPathInputsArg: false,
		"maxDepth":                       3,
	})
	require.NoError(t, err)
	require.False(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySearch))
	require.False(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
}

func TestRequestPlanDoesNotOpenSessionStateWithoutARequestedSession(t *testing.T) {
	for name, request := range map[string]struct {
		operation OperationID
		args      map[string]any
	}{
		"files":            {operation: "agent.files", args: map[string]any{FilesRequestPlanHasPathInputsArg: false}},
		"files graph":      {operation: "agent.files", args: map[string]any{"maxDepth": 1}},
		"find connections": {operation: "agent.find-connections", args: map[string]any{"note": "Alpha.md"}},
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := RequestPlan(request.operation, request.args)
			require.NoError(t, err)
			require.Equal(t, SessionNone, plan.Session)
			require.NotContains(t, plan.Readiness, ReadinessSession)
		})
	}
}

func TestRequestPlanDoesNotAwaitIgnoredTagOrPropertySessions(t *testing.T) {
	for _, operationID := range []OperationID{"agent.list-tags", "agent.list-properties"} {
		plan, err := RequestPlan(operationID, map[string]any{"sessionId": "ignored"})
		require.NoError(t, err)
		require.Equal(t, SessionNone, plan.Session)
		require.NotContains(t, plan.Readiness, ReadinessSession)
	}
}

func TestRequestPlanUsesCurrentPropertyProjectionWithoutFallbackWrites(t *testing.T) {
	plan, err := RequestPlan("agent.list-properties", map[string]any{"sessionId": "ignored"})
	require.NoError(t, err)
	require.True(t, plan.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.Equal(t, StoreExistingReadOnly, plan.StoreAccess)
	require.Equal(t, NoteStateLive, plan.NoteState)
	require.Equal(t, UnavailableDegrade, plan.Unavailable)
	require.Equal(t, SessionNone, plan.Session)
}

func TestRequestPlanPreservesIndexedGraphAndHealthEnrichment(t *testing.T) {
	files, err := RequestPlan("agent.files", map[string]any{"maxDepth": 1})
	require.NoError(t, err)
	require.True(t, files.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySearch))
	require.True(t, files.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.True(t, files.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeRefDiscovery))

	community, err := RequestPlan("agent.community-list", nil)
	require.NoError(t, err)
	require.True(t, community.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.Equal(t, UnavailableDegrade, community.Unavailable)

	health, err := RequestPlan("agent.vault-health", nil)
	require.NoError(t, err)
	require.True(t, health.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySearch))
	require.True(t, health.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.True(t, health.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityCodeRefDiscovery))
	require.Contains(t, health.Readiness, ReadinessNoteCache)

	connections, err := RequestPlan("agent.find-connections", map[string]any{"note": "Alpha.md", "sessionId": "stable"})
	require.NoError(t, err)
	require.Equal(t, SessionExistingOnly, connections.Session)
	require.Contains(t, connections.Readiness, ReadinessCodeIndex)
	require.Contains(t, connections.Readiness, ReadinessSession)
}

func TestRequestPlanDerivesReportFromNormalizedOperation(t *testing.T) {
	plan, err := RequestPlan("agent.report", map[string]any{"op": "doc-coverage"})
	require.NoError(t, err)
	require.Equal(t, []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex}, plan.Capabilities)
	require.Equal(t, []Readiness{ReadinessCodeIndex}, plan.Readiness)
}

func TestCodeSymbolContextHasAnExplicitFullRuntimeParityExemption(t *testing.T) {
	_, err := RequestPlan("agent.code-symbol-context", nil)
	require.ErrorIs(t, err, ErrRequestPlanNotDeclared)

	declaration, ok := DefaultRegistry().Declaration("agent.code-symbol-context")
	require.True(t, ok)
	require.Equal(t, PlanSourceExempt, declaration.PlanSource)
	require.Equal(t, RuntimeExemptionFullRuntimeParity, declaration.Exemption)
	require.NotNil(t, declaration.ExemptionPlan)
	require.True(t, declaration.ExemptionPlan.FullRuntime)
	require.Equal(t, []Readiness{ReadinessSearch, ReadinessNoteCache, ReadinessCodeIndex}, declaration.ExemptionPlan.Readiness)
	require.Equal(t, bootstrap.RuntimeRequirements{}, declaration.ExemptionPlan.RuntimeRequirements())
}

func TestRequestPlansDeclareCodeIndexFreshnessWithoutOperationSwitches(t *testing.T) {
	for _, tc := range []struct {
		operation string
		args      map[string]any
		want      CodeIndexFreshness
	}{
		{operation: "agent.files", args: map[string]any{"maxDepth": 1}, want: CodeIndexFreshnessCurrent},
		{operation: "agent.semantic-query", want: CodeIndexFreshnessCurrent},
		{operation: "agent.list-properties", want: CodeIndexFreshnessNone},
		{operation: "agent.find-connections", want: CodeIndexFreshnessNone},
	} {
		plan, err := RequestPlan(OperationID(tc.operation), tc.args)
		require.NoError(t, err)
		require.Equal(t, tc.want, plan.CodeIndexFreshness, tc.operation)
	}
}

func TestRequestPlanDerivesVaultContextFromProfileAndOptions(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"default":       {},
		"code profile":  {"profile": "code"},
		"vault profile": {"profile": "vault", "includeOntology": true},
		"graph summary": {"graphSummary": true},
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := RequestPlan("agent.vault-context", args)
			require.NoError(t, err)
			require.Equal(t, NoteStateCacheSnapshot, plan.NoteState)
			require.Equal(t, []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilitySearch, bootstrap.RuntimeCapabilityCodeIndex, bootstrap.RuntimeCapabilityCodeRefDiscovery}, plan.Capabilities)
			require.Equal(t, []Readiness{ReadinessSearch, ReadinessNoteCache, ReadinessCodeIndex, ReadinessSession}, plan.Readiness)
		})
	}

	minimal, err := RequestPlan("agent.vault-context", map[string]any{"requestScope": "minimal_bootstrap"})
	require.NoError(t, err)
	require.Equal(t, NoteStateLive, minimal.NoteState)
	require.Empty(t, minimal.Capabilities)

	indexed, err := RequestPlan("agent.vault-context", map[string]any{"requestScope": "indexed_bootstrap"})
	require.NoError(t, err)
	require.Equal(t, []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex}, indexed.Capabilities)
	require.Contains(t, indexed.Readiness, ReadinessSession)
}

func TestRequestPlanKeepsOptionalFilesGraphEnrichmentLiveOnIndexFailure(t *testing.T) {
	plan, err := RequestPlan("agent.files", map[string]any{"maxDepth": 1})
	require.NoError(t, err)
	require.Equal(t, UnavailableDegrade, plan.Unavailable)
}
