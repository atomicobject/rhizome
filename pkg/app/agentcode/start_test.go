package agentcode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
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
		input StartRequestInput
		rich  bool
	}{
		{"bare", vault, StartRequestInput{}, false},
		{"code profile", vault, StartRequestInput{Profile: "code"}, false},
		{"absolute code file", vault, StartRequestInput{Profile: "code", Files: []string{filepath.Join(root, "main.go")}}, false},
		{"directory", vault, StartRequestInput{Files: []string{"docs/specs"}}, false},
		{"markdown named directory", vault, StartRequestInput{Files: []string{"docs/guide.md"}}, false},
		{"unconfigured html", vault, StartRequestInput{Files: []string{"docs/report.html"}}, false},
		{"vault profile", vault, StartRequestInput{Profile: "vault"}, true},
		{"ontology", vault, StartRequestInput{IncludeOntology: true}, true},
		{"graph summary", vault, StartRequestInput{GraphSummary: true}, true},
		{"context file", vault, StartRequestInput{ContextFiles: []string{"CONTEXT.md"}}, true},
		{"markdown note", vault, StartRequestInput{Files: []string{"docs/decision.md"}}, true},
		{"nonexistent markdown note", vault, StartRequestInput{Files: []string{"docs/missing.md"}}, true},
		{"configured markdown", configured, StartRequestInput{Files: []string{"docs/decision.md"}}, true},
		{"configured html", configured, StartRequestInput{Files: []string{"docs/report.html"}}, true},
		{"ontology and directory", vault, StartRequestInput{Profile: "code", IncludeOntology: true, Files: []string{"docs/specs"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanStartRequest(tc.vault, formats, tc.input)
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
