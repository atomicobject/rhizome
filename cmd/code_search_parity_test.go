package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	appweb "github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSearchCLIUsesCanonicalApplicationOrdering(t *testing.T) {
	testEmbedding := &embeddings.Config{Enabled: true, Provider: "test", Model: "deterministic", Dimensions: 32}
	vault := behaviorTestVault(t, behaviorVaultOptions{
		noteEmbeddings: testEmbedding,
		codeEmbeddings: testEmbedding,
		codeEnabled:    true,
	})
	require.NoError(t, os.WriteFile(filepath.Join(vault, "calculator.go"), []byte(`package calculator

func CalculateTotal(values []int) int {
	total := 0
	for _, value := range values { total += value }
	return total
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vault, "invoice.go"), []byte(`package calculator

func InvoiceTotal(subtotal, tax int) int { return subtotal + tax }
`), 0o644))

	_, _, err := runCLIWithVault(t, []string{"index"}, "", vault)
	require.NoError(t, err)
	stdout, _, err := runCLIWithVault(t, []string{
		"search", "calculate total", "--json", "--limit", "3", "--max-per-owner", "2", "--budget-chars", "24000", "--timeout", "30s",
	}, "", vault)
	require.NoError(t, err)

	cfgDir, cfg, err := obsidian.FindLocalConfig(vault)
	require.NoError(t, err)
	vaultDef := obsidian.LocalConfigToDefinition(cfgDir, cfg)
	embCfg, err := obsidian.LoadEmbeddingsConfig(vault)
	require.NoError(t, err)
	embCfg.IndexPath = obsidian.UnifiedIndexPath(vault, embCfg.IndexPath)
	result, err := unifiedsearch.Execute(context.Background(), unifiedsearch.ApplicationOptions{
		Profile:      unifiedsearch.ProfileInteractive,
		VisibleLimit: 3,
		BudgetChars:  24000,
		MaxPerOwner:  2,
		Runtime: unifiedsearch.Options{
			Query:       "calculate total",
			Limit:       3,
			BudgetChars: 24000,
			MaxPerOwner: 2,
			SeedLimit:   5,
			UseVector:   true,
			UseIntel:    true,
			UseGraph:    true,
			UseRefs:     true,
			VaultPath:   vault,
			VaultDef:    vaultDef,
			EmbCfg:      embCfg,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Sources)

	var cliResult unifiedsearch.ApplicationResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(stdout)), &cliResult))
	require.Equal(t, canonicalApplicationSourceIDs(result.Sources), canonicalApplicationSourceIDs(cliResult.Sources))
	require.Equal(t, result.Target, cliResult.Target)
	require.Equal(t, result.Answer.Coverage, cliResult.Answer.Coverage)
	require.Equal(t, result.Answer.Confidence, cliResult.Answer.Confidence)
	require.NotEmpty(t, result.Continuation)
	require.Equal(t, result.Request.Policy.Profile, cliResult.Request.Policy.Profile)
	require.Equal(t, result.Request.Policy.VisibleLimit, cliResult.Request.Policy.VisibleLimit)
	require.Equal(t, result.Request.Policy.CandidateWindow, cliResult.Request.Policy.CandidateWindow)
	require.Equal(t, result.Request.Policy.MaxPerOwner, cliResult.Request.Policy.MaxPerOwner)
	require.Equal(t, result.Request.Policy.BudgetChars, cliResult.Request.Policy.BudgetChars)
	require.Equal(t, result.Request.Policy.DeadlineMillis, cliResult.Request.Policy.DeadlineMillis)
	require.Equal(t, unifiedsearch.ProfileInteractive, result.Request.Policy.Profile)
	require.Equal(t, 3, result.Request.Policy.VisibleLimit)
	require.Equal(t, 2, result.Request.Policy.MaxPerOwner)
	require.NotEmpty(t, result.Answer.Confidence.Level)

	formatRuntime, err := builtin.NewRuntime()
	require.NoError(t, err)
	noteMetadata, err := notemeta.NewIndexer(formatRuntime)
	require.NoError(t, err)
	srv, err := appweb.NewServer(context.Background(), appweb.Config{
		Vault:        &obsidian.Vault{Name: vault},
		VaultPath:    vault,
		VaultDef:     vaultDef,
		NoteMetadata: noteMetadata,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	httpResp, err := http.Get(httpSrv.URL + "/api/v1/search?q=" + url.QueryEscape("calculate total") + "&limit=3&budget=24000")
	require.NoError(t, err)
	defer httpResp.Body.Close()
	require.Equal(t, http.StatusOK, httpResp.StatusCode)
	var webResult appweb.SearchResponse
	require.NoError(t, json.NewDecoder(httpResp.Body).Decode(&webResult))
	require.Equal(t, canonicalApplicationSourceIDs(cliResult.Sources), canonicalWebSourceIDs(webResult.Matches))
	require.Equal(t, cliResult.Target.Status, webResult.TargetStatus)
	require.Equal(t, cliResult.Answer.Coverage, webResult.Coverage)
	require.Equal(t, cliResult.Answer.Confidence, webResult.Confidence)
	require.NotEmpty(t, cliResult.Continuation)
	require.NotEmpty(t, webResult.ContinuationToken)
	require.Equal(t, cliResult.Request.Policy.Profile, webResult.Policy.Profile)
	require.Equal(t, cliResult.Request.Policy.VisibleLimit, webResult.Policy.VisibleLimit)
	require.Equal(t, cliResult.Request.Policy.CandidateWindow, webResult.Policy.CandidateWindow)
	require.Equal(t, cliResult.Request.Policy.MaxPerOwner, webResult.Policy.MaxPerOwner)
	require.Equal(t, cliResult.Request.Policy.BudgetChars, webResult.Policy.BudgetChars)
	require.Equal(t, int64(3000), webResult.Policy.DeadlineMillis) // HTTP keeps the interactive default deadline

	cliNextJSON, _, err := runCLIWithVault(t, []string{
		"search", "calculate total", "--json", "--continuation", cliResult.Continuation, "--limit", "3", "--max-per-owner", "2", "--budget-chars", "24000", "--timeout", "30s",
	}, "", vault)
	require.NoError(t, err)
	var cliNext unifiedsearch.ApplicationResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(cliNextJSON)), &cliNext))
	httpNext, err := http.Get(httpSrv.URL + "/api/v1/search?q=" + url.QueryEscape("calculate total") + "&continuationToken=" + url.QueryEscape(webResult.ContinuationToken) + "&limit=3&budget=24000")
	require.NoError(t, err)
	defer httpNext.Body.Close()
	require.Equal(t, http.StatusOK, httpNext.StatusCode)
	var webNext appweb.SearchResponse
	require.NoError(t, json.NewDecoder(httpNext.Body).Decode(&webNext))
	require.NotEmpty(t, cliNext.Sources)
	require.Equal(t, canonicalApplicationSourceIDs(cliNext.Sources), canonicalWebSourceIDs(webNext.Matches))
	require.Equal(t, cliNext.Target.Status, webNext.TargetStatus)
	require.Equal(t, cliNext.Answer.Coverage, webNext.Coverage)
	require.Equal(t, cliNext.Answer.Confidence, webNext.Confidence)
}

func canonicalApplicationSourceIDs(sources []unifiedsearch.SourceAssessment) []string {
	out := make([]string, len(sources))
	for i, source := range sources {
		out[i] = searchapplication.CanonicalSourceIdentity(source.Result)
	}
	return out
}

func canonicalWebSourceIDs(matches []appweb.SearchMatch) []string {
	out := make([]string, len(matches))
	for i, match := range matches {
		switch {
		case match.NodeRef != nil && match.NodeRef.Kind != "" && match.NodeRef.Kind != "NOTE":
			out[i] = strings.Join([]string{"node", match.NodeRef.NotePath, match.NodeRef.NodeID, match.NodeRef.Fragment, match.NodeRef.Structural}, "\x00")
		case match.FQN != "":
			out[i] = strings.Join([]string{"fqn", match.FQN, match.Path}, "\x00")
		default:
			out[i] = "path\x00" + match.Path
		}
	}
	return out
}

func TestSearchCLIRawRendersCodeAfterSearchResourcesClose(t *testing.T) {
	vault := behaviorTestVault(t, behaviorVaultOptions{codeEnabled: true})
	require.NoError(t, os.WriteFile(filepath.Join(vault, "render.go"), []byte(`package render

func RenderNeedle() string {
	return "materialized source body"
}
`), 0o644))
	_, _, err := runCLIWithVault(t, []string{"index"}, "", vault)
	require.NoError(t, err)

	stdout, _, err := runCLIWithVault(t, []string{"search", "RenderNeedle", "--seed", "symbol:RenderNeedle", "--mode", "go_to_def", "--raw", "--fast", "--limit", "5", "--timeout", "0s"}, "", vault)
	require.NoError(t, err)
	require.Contains(t, stdout, `return "materialized source body"`, "raw output must retain the indexed source span after Execute closes its store")

	jsonOutput, timingsOutput, err := runCLIWithVault(t, []string{"search", "RenderNeedle", "--json", "--timings", "--fast", "--limit", "5", "--timeout", "0s"}, "", vault)
	require.NoError(t, err)
	var decoded unifiedsearch.ApplicationResult
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &decoded), "timings must not append human text to JSON stdout")
	require.Contains(t, timingsOutput, "Timings:")
}
