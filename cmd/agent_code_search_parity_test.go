package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/stretchr/testify/require"
)

const agentCodeSearchHelperEnv = "RZM_AGENT_CODE_SEARCH_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(agentCodeSearchHelperEnv) != "1" {
		// Commands that ensure a vault runtime would otherwise spawn this test
		// binary as `serve --headless`, which re-runs the whole suite. Tests
		// that want auto-start opt in with t.Setenv.
		if _, set := os.LookupEnv(appruntime.AutostartEnv); !set {
			_ = os.Setenv(appruntime.AutostartEnv, "0")
		}
		os.Exit(m.Run())
	}
	rootCmd.SetIn(os.Stdin)
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetArgs(os.Args[1:])
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestAgentCodeSemanticQueryMatchesAgentCLIContract(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for JavaScript execution")
	}
	t.Cleanup(func() {
		resetCommandContexts(rootCmd)
		indexCmd.SetErr(nil)
	})
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/config.yml": `
notes: {}
noteEmbeddings:
  enabled: true
  provider: test
  model: deterministic
  dimensions: 32
codeEmbeddings:
  enabled: true
  provider: test
  model: deterministic
  dimensions: 32
code:
  enabled: true
  go:
    roots: ["."]
`,
		"calculator.go": `package calculator

func CalculateTotal(values []int) int {
	total := 0
	for _, value := range values { total += value }
	return total
}
`,
		"invoice.go": `package calculator

func InvoiceTotal(subtotal, tax int) int { return subtotal + tax }
`,
		"note.md": "# Calculating totals\n\nThe calculator adds each input value.\n",
	})
	// Execute launches os.Executable (this test binary); TestMain routes that child to Cobra.
	t.Setenv(agentCodeSearchHelperEnv, "1")
	indexOutput, stderr, err := runRootCLI(t, context.Background(), []string{"index", "--vault", vault.name})
	require.NoError(t, err, "stdout=%s stderr=%s", indexOutput, stderr)

	cliJSON, stderr, err := runRootCLI(t, context.Background(), []string{
		"agent", "semantic-query", "--vault", vault.name,
		"--query", "calculate total", "--scope", "code", "--limit", "1", "--budget-chars", "24000",
	})
	require.NoError(t, err, "stdout=%s stderr=%s", cliJSON, stderr)
	var cli agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal([]byte(cliJSON), &cli), cliJSON)
	require.NotEmpty(t, cli.Matches)
	require.Equal(t, searchapplication.ProfileAgent, cli.Profile)
	require.NotNil(t, cli.Policy)
	require.Equal(t, searchapplication.ProfileAgent, cli.Policy.Profile)

	code := `return await rzm.semanticQuery({queries:["calculate total"], scope:"code", limit:1, budgetChars:24000});`
	codeJSON, stderr, err := runRootCLI(t, context.Background(), []string{
		"agent", "code", "execute", "--vault", vault.name, "--code", code,
	})
	require.NoError(t, err, "stdout=%s stderr=%s", codeJSON, stderr)
	var execution agentcode.ExecuteResult
	require.NoError(t, json.Unmarshal([]byte(codeJSON), &execution), codeJSON)
	require.True(t, execution.OK, codeJSON)
	var outcome struct {
		OK      bool            `json:"ok"`
		Payload json.RawMessage `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(execution.Result, &outcome), string(execution.Result))
	require.True(t, outcome.OK, string(execution.Result))
	var viaCode agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal(outcome.Payload, &viaCode), string(outcome.Payload))
	require.NotEmpty(t, viaCode.Matches, string(execution.Result))

	require.Equal(t, semanticResponseIdentities(cli), semanticResponseIdentities(viaCode))
	require.Equal(t, cli.Profile, viaCode.Profile)
	require.Equal(t, cli.Policy, viaCode.Policy)
	require.Equal(t, cli.TargetStatus, viaCode.TargetStatus)
	require.Equal(t, cli.ResolutionConfidence, viaCode.ResolutionConfidence)
	require.Equal(t, cli.Coverage, viaCode.Coverage)
	require.Equal(t, cli.Confidence, viaCode.Confidence)
	require.Equal(t, cli.Remaining, viaCode.Remaining)
	require.NotEmpty(t, cli.ContinuationToken)
	require.NotEmpty(t, viaCode.ContinuationToken)

	cliNextJSON, stderr, err := runRootCLI(t, context.Background(), []string{
		"agent", "semantic-query", "--vault", vault.name,
		"--query", "calculate total", "--scope", "code", "--limit", "1", "--budget-chars", "24000",
		"--continuation-token", cli.ContinuationToken,
	})
	require.NoError(t, err, "stdout=%s stderr=%s", cliNextJSON, stderr)
	var cliNext agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal([]byte(cliNextJSON), &cliNext), cliNextJSON)

	tokenJSON, err := json.Marshal(viaCode.ContinuationToken)
	require.NoError(t, err)
	nextCode := fmt.Sprintf(`return await rzm.semanticQuery({queries:["calculate total"], scope:"code", limit:1, budgetChars:24000, continuationToken:%s});`, tokenJSON)
	codeNextJSON, stderr, err := runRootCLI(t, context.Background(), []string{
		"agent", "code", "execute", "--vault", vault.name, "--code", nextCode,
	})
	require.NoError(t, err, "stdout=%s stderr=%s", codeNextJSON, stderr)
	viaCodeNext := decodeAgentCodeSemanticResponse(t, codeNextJSON)
	require.NotEmpty(t, cliNext.Matches)
	require.Equal(t, semanticResponseIdentities(cliNext), semanticResponseIdentities(viaCodeNext))
	require.Equal(t, cliNext.Policy, viaCodeNext.Policy)
	require.Equal(t, cliNext.TargetStatus, viaCodeNext.TargetStatus)
	require.Equal(t, cliNext.Coverage, viaCodeNext.Coverage)
	require.Equal(t, cliNext.Confidence, viaCodeNext.Confidence)
	require.NotEqual(t, semanticResponseIdentities(cli), semanticResponseIdentities(cliNext))
}

func decodeAgentCodeSemanticResponse(t *testing.T, encoded string) agentapi.SemanticQueryResponse {
	t.Helper()
	var execution agentcode.ExecuteResult
	require.NoError(t, json.Unmarshal([]byte(encoded), &execution), encoded)
	require.True(t, execution.OK, encoded)
	var outcome struct {
		OK      bool            `json:"ok"`
		Payload json.RawMessage `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(execution.Result, &outcome), string(execution.Result))
	require.True(t, outcome.OK, string(execution.Result))
	var response agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal(outcome.Payload, &response), string(outcome.Payload))
	return response
}

func semanticResponseIdentities(response agentapi.SemanticQueryResponse) []string {
	out := make([]string, len(response.Matches))
	for i, match := range response.Matches {
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
