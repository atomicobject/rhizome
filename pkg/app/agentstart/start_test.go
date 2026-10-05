package agentstart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPlanRequestSelectsMinimalAndIndexedBootstrap(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "specs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "guide.md"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "decision.md"), []byte("# Decision\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "report.html"), []byte("<h1>Report</h1>\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644))
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	configured := obsidian.VaultDefinition{Root: root, Includes: []string{"docs/*.md", "docs/*.html"}}
	for _, tc := range []struct {
		name  string
		vault obsidian.VaultDefinition
		input RequestInput
		rich  bool
	}{
		{"bare", vault, RequestInput{}, false},
		{"code profile", vault, RequestInput{Profile: "code"}, false},
		{"absolute code file", vault, RequestInput{Profile: "code", Files: []string{filepath.Join(root, "main.go")}}, false},
		{"directory", vault, RequestInput{Files: []string{"docs/specs"}}, false},
		{"markdown named directory", vault, RequestInput{Files: []string{"docs/guide.md"}}, false},
		{"unconfigured html", vault, RequestInput{Files: []string{"docs/report.html"}}, false},
		{"vault profile", vault, RequestInput{Profile: "vault"}, true},
		{"ontology", vault, RequestInput{IncludeOntology: true}, true},
		{"graph summary", vault, RequestInput{GraphSummary: true}, true},
		{"context file", vault, RequestInput{ContextFiles: []string{"CONTEXT.md"}}, true},
		{"markdown note", vault, RequestInput{Files: []string{"docs/decision.md"}}, true},
		{"nonexistent markdown note", vault, RequestInput{Files: []string{"docs/missing.md"}}, true},
		{"configured markdown", configured, RequestInput{Files: []string{"docs/decision.md"}}, true},
		{"configured html", configured, RequestInput{Files: []string{"docs/report.html"}}, true},
		{"ontology and directory", vault, RequestInput{Profile: "code", IncludeOntology: true, Files: []string{"docs/specs"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanRequest(tc.vault, formats, tc.input)
			require.NoError(t, err)
			want := actions.VaultContextRequestScopeMinimalBootstrap
			if tc.rich {
				want = actions.VaultContextRequestScopeIndexedBootstrap
			}
			require.Equal(t, want, plan.Scope)
			require.Equal(t, tc.rich, plan.Requirements.Includes(bootstrap.RuntimeCapabilityCodeIndex))
			require.False(t, plan.Requirements.Includes(bootstrap.RuntimeCapabilitySearch))
			require.False(t, plan.Requirements.Includes(bootstrap.RuntimeCapabilityCodeRefDiscovery))
		})
	}
}
