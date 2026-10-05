package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	noteembsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestAgentSurfaceCommandOutputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "surface", "--vault", vault.name, "--session-id", "test-session"})
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)

	var resp struct {
		DefaultBudget      int      `json:"defaultBudgetChars"`
		StatusSnapshotKind string   `json:"statusSnapshotKind"`
		Notes              []string `json:"notes"`
		RetrievalLoop      []string `json:"retrievalLoop"`
		Capabilities       struct {
			Server struct {
				Ready bool `json:"ready"`
			} `json:"server"`
			Tools struct {
				Available []string `json:"available"`
				Mutating  []string `json:"mutating"`
			} `json:"tools"`
		} `json:"capabilities"`
		Compression struct {
			Enabled bool   `json:"enabled"`
			Mode    string `json:"mode"`
		} `json:"compression"`
		Commands []struct {
			Name     string   `json:"name"`
			Use      string   `json:"use"`
			Source   string   `json:"source"`
			Category string   `json:"category"`
			ToolName string   `json:"toolName"`
			Examples []string `json:"examples"`
			Flags    []struct {
				Name       string `json:"name"`
				Default    string `json:"default"`
				Repeatable bool   `json:"repeatable"`
			} `json:"flags"`
			Subcommands []struct {
				Name  string `json:"name"`
				Flags []struct {
					Name string `json:"name"`
				} `json:"flags"`
			} `json:"subcommands"`
		} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.Equal(t, agentCLIBudgetFloor, resp.DefaultBudget)
	require.Equal(t, "local-snapshot", resp.StatusSnapshotKind)
	require.True(t, resp.Capabilities.Server.Ready)
	require.NotContains(t, resp.Capabilities.Tools.Available, "start")
	require.NotContains(t, resp.Capabilities.Tools.Available, "validate")
	require.NotContains(t, resp.Capabilities.Tools.Available, "ontology_inspect")
	require.Contains(t, resp.Capabilities.Tools.Available, "semantic_query")
	require.Contains(t, resp.Capabilities.Tools.Available, "files")
	require.Contains(t, resp.Capabilities.Tools.Available, "report")
	require.Contains(t, resp.Capabilities.Tools.Available, "node_link")
	require.Contains(t, resp.Capabilities.Tools.Available, "note_rename_heading")
	require.Equal(t, []string{"note_rename_heading"}, resp.Capabilities.Tools.Mutating)
	require.Contains(t, resp.Capabilities.Tools.Available, "vault_health")
	require.Contains(t, resp.Capabilities.Tools.Available, "query_recipe")
	require.Contains(t, resp.Capabilities.Tools.Available, "code_symbol")
	require.Contains(t, resp.Capabilities.Tools.Available, "code_references")
	require.Contains(t, resp.Capabilities.Tools.Available, "code_symbol_context")
	require.NotContains(t, resp.Capabilities.Tools.Available, "context_recipe")
	require.False(t, resp.Compression.Enabled)
	require.Equal(t, "disabled", resp.Compression.Mode)
	require.NotEmpty(t, resp.Notes)
	require.NotEmpty(t, resp.RetrievalLoop)
	retrievalLoop := strings.Join(resp.RetrievalLoop, "\n")
	require.Contains(t, retrievalLoop, `agent start --intent "<task>"`)
	require.Contains(t, retrievalLoop, "few high-value repeatable")
	require.Contains(t, retrievalLoop, "query-recipe run")
	require.Contains(t, retrievalLoop, "agent validate <check>")
	require.NotContains(t, retrievalLoop, "--profile")
	require.NotContains(t, retrievalLoop, "--ontology")
	require.NotContains(t, retrievalLoop, "file-context --file <path> --submodule-depth 1")
	require.NotContains(t, retrievalLoop, "validate --check")
	require.Contains(t, strings.Join(resp.Notes, "\n"), "query-recipe` packets")

	commandNames := make([]string, 0, len(resp.Commands))
	require.NotEmpty(t, resp.Commands)
	require.Equal(t, "start", resp.Commands[0].Name)
	for _, command := range resp.Commands {
		commandNames = append(commandNames, command.Name)
		if command.Name == "start" || command.Name == "validate" || command.Name == "view" || command.Name == "ontology-inspect" {
			require.Empty(t, command.ToolName, "local-only command %q must not look CallJSON-dispatchable", command.Name)
		}
		if command.Name == "start" {
			require.Equal(t, "agent", command.Source)
			require.Equal(t, "session", command.Category)
			foundRepeatable := false
			for _, flag := range command.Flags {
				if flag.Name == "file" {
					foundRepeatable = flag.Repeatable
					break
				}
			}
			require.True(t, foundRepeatable)
		}
		if command.Name == "validate" {
			hasCheck := false
			hasMaxIssues := false
			for _, flag := range command.Flags {
				if flag.Name == "check" {
					hasCheck = true
				}
				if flag.Name == "max-issues" {
					hasMaxIssues = true
				}
			}
			require.False(t, hasCheck)
			require.True(t, hasMaxIssues)
		}
		if command.Name == "next-id" {
			require.Empty(t, command.ToolName, "next-id remains a local agent command, not an MCP tool")
			foundRepeatablePath := false
			for _, flag := range command.Flags {
				if flag.Name == "path" {
					foundRepeatablePath = flag.Repeatable
					break
				}
			}
			require.True(t, foundRepeatablePath)
		}
		if command.Name == "semantic-query" {
			require.Equal(t, "agent", command.Source)
			require.Equal(t, "discovery", command.Category)
			require.Equal(t, "semantic_query", command.ToolName)
			require.NotEmpty(t, command.Examples)
			flags := map[string]string{}
			for _, flag := range command.Flags {
				flags[flag.Name] = flag.Default
			}
			for _, name := range []string{"timings", "path-prefix", "note-type"} {
				require.Contains(t, flags, name)
			}
			require.Equal(t, "false", flags["timings"])
		}
		if command.Name == "files" {
			flags := map[string]bool{}
			for _, flag := range command.Flags {
				flags[flag.Name] = true
			}
			require.True(t, flags["session-id"])
			require.True(t, flags["session"])
		}
		if command.Name == "code-symbol" {
			require.Equal(t, "code", command.Category)
			require.NotEmpty(t, command.Examples)
		}
		if command.Name == "code-references" {
			require.Equal(t, "code", command.Category)
			require.NotEmpty(t, command.Examples)
		}
		if command.Name == "code-symbol-context" {
			require.Equal(t, "code", command.Category)
			require.NotEmpty(t, command.Examples)
		}
		if command.Name == "query-recipe" {
			require.Equal(t, "ontology", command.Category)
			require.NotEmpty(t, command.Examples)
			subcommandNames := make([]string, 0, len(command.Subcommands))
			runHasInputsJSON := false
			for _, subcommand := range command.Subcommands {
				subcommandNames = append(subcommandNames, subcommand.Name)
				if subcommand.Name == "run" {
					for _, flag := range subcommand.Flags {
						if flag.Name == "inputs-json" {
							runHasInputsJSON = true
						}
					}
				}
			}
			require.Contains(t, subcommandNames, "list")
			require.Contains(t, subcommandNames, "run")
			require.Contains(t, subcommandNames, "validate")
			require.True(t, runHasInputsJSON)
		}
		if command.Name == "note-move" {
			require.Equal(t, "rzm note move", command.Use)
			require.Equal(t, "cli", command.Source)
			require.Equal(t, "safe_mutation", command.Category)
			hasUpdateBacklinks := false
			hasToFolder := false
			for _, flag := range command.Flags {
				if flag.Name == "update-backlinks" {
					hasUpdateBacklinks = true
				}
				if flag.Name == "to-folder" {
					hasToFolder = true
				}
			}
			require.True(t, hasUpdateBacklinks)
			require.True(t, hasToFolder)
		}
	}
	require.Contains(t, commandNames, "semantic-query")
	require.Contains(t, commandNames, "code-symbol")
	require.Contains(t, commandNames, "code-references")
	require.Contains(t, commandNames, "code-symbol-context")
	require.Contains(t, commandNames, "external-references")
	require.Contains(t, commandNames, "files")
	require.Contains(t, commandNames, "file-context")
	require.Contains(t, commandNames, "report")
	require.Contains(t, commandNames, "ontology-reference")
	require.Contains(t, commandNames, "node-link")
	require.Contains(t, commandNames, "note-move")
	require.Contains(t, commandNames, "query-recipe")
	require.Contains(t, commandNames, "current-user")
	require.NotContains(t, commandNames, "context-recipe")
}

func TestAgentSurfaceCommandOrderAndCategoriesRemainStable(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"Alpha.md": "# Alpha\n"})
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "surface", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)
	var surface agentSurfaceResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &surface))
	type commandContract struct {
		name     string
		category string
	}
	want := []commandContract{
		{"start", "session"},
		{"validate", "validation"},
		{"node-link", "ontology"},
		{"note-rename-heading", "safe_mutation"},
		{"semantic-query", "discovery"},
		{"code-symbol", "code"},
		{"code-references", "code"},
		{"code-symbol-context", "code"},
		{"files", "discovery"},
		{"file-context", "context"},
		{"vault-context", "context"},
		{"report", "analysis"},
		{"vault-health", "analysis"},
		{"list-tags", "discovery"},
		{"list-properties", "discovery"},
		{"community-list", "discovery"},
		{"find-connections", "discovery"},
		{"ontology-reference", "ontology"},
		{"ontology-authoring-guide", "ontology"},
		{"ontology-inspect", "ontology"},
		{"ontology-query-schema", "ontology"},
		{"ontology-query", "ontology"},
		{"query-recipe", "ontology"},
		{"view", "ontology"},
		{"current-user", "identity"},
		{"next-id", "ontology"},
		{"graph-path", "navigation"},
		{"code-rationale", "analysis"},
		{"external-references", "code"},
		{"code", "discovery"},
		{"evaluate", "analysis"},
		{"check-paths", "discovery"},
		{"evaluate-batch", "analysis"},
		{"surface", "self"},
		{"note-move", "safe_mutation"},
	}

	require.Len(t, surface.Commands, len(want))
	for i, expected := range want {
		require.Equal(t, expected.name, surface.Commands[i].Name, "command index %d", i)
		require.Equal(t, expected.category, surface.Commands[i].Category, "command %s", expected.name)
	}
}

func TestAgentSurfaceAdvertisesEveryCatalogCLICommandExactlyOnce(t *testing.T) {
	surface := buildAgentSurface()
	descriptors := agentapi.AgentCLICommandDescriptors()
	require.Len(t, surface.Commands, len(descriptors)+2, "catalog commands plus surface and note-move exceptions")

	seen := make(map[string]int, len(surface.Commands))
	for _, command := range surface.Commands {
		seen[command.Name]++
	}
	for i, descriptor := range descriptors {
		command := surface.Commands[i]
		require.Equal(t, descriptor.AgentCommand, command.Name)
		if descriptor.Kind == agentapi.ToolKindLocal {
			require.Empty(t, command.ToolName)
		} else {
			require.Equal(t, descriptor.Name, command.ToolName)
		}
		require.Equal(t, string(descriptor.AgentCLICategory), command.Category)
		require.Equal(t, 1, seen[descriptor.AgentCommand])

	}

	require.Equal(t, 1, seen["surface"])
	require.Equal(t, 1, seen["note-move"])
	require.Empty(t, surface.Commands[len(descriptors)].ToolName)
	require.Empty(t, surface.Commands[len(descriptors)+1].ToolName)
}

func TestAgentCommandSilencesCobraOutput(t *testing.T) {
	setupAgentTestVault(t, map[string]string{"Alpha.md": "# Alpha\n"})
	stdout, stderr, err := runRootCLIWithRootSilence(t, nil, []string{"agent", "report"}, false)
	require.Error(t, err)
	require.Empty(t, stdout)
	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
	require.Equal(t, "op is required", payload["error"])
	require.NotContains(t, stderr, "Usage:")
}

func TestAgentFilesSkipLinkFlags(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\n[[Beta#topic]] ![[Gamma]]\n",
		"Beta.md":  "# Beta\n\n## topic\n",
		"Gamma.md": "# Gamma\n",
	})
	for _, tc := range []struct{ name, flag, absent string }{
		{"anchors", "--skip-anchors", "Beta.md"},
		{"embeds", "--skip-embeds", "Gamma.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := []string{"agent", "files", "--vault", vault.name, "--input", "Alpha.md", "--max-depth", "1", "--include-content", "false"}
			plain, plainErr, err := runRootCLI(t, nil, base)
			require.NoError(t, err, plainErr)
			require.Contains(t, plain, tc.absent)
			filtered, filteredErr, err := runRootCLI(t, nil, append(base, tc.flag))
			require.NoError(t, err, filteredErr)
			require.NotContains(t, filtered, tc.absent)
			require.Contains(t, filtered, "Alpha.md")
		})
	}
}

func TestAgentListPropertiesValueCountsFlag(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"Alpha.md": "---\nstatus: active\n---\n# Alpha\n"})
	t.Cleanup(func() { indexCmd.SetErr(nil) })
	_, indexStderr, indexErr := runRootCLI(t, nil, []string{"index", "--vault", vault.name})
	require.NoError(t, indexErr, indexStderr)
	for _, tc := range []struct {
		name   string
		flag   string
		counts bool
	}{
		{"default", "", true}, {"disabled", "--value-counts=false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"agent", "list-properties", "--vault", vault.name}
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			stdout, stderr, err := runRootCLI(t, nil, args)
			require.NoError(t, err, stderr)
			require.Empty(t, stderr)
			require.Contains(t, stdout, `"name":"status"`)
			require.Contains(t, stdout, `"active"`)
			require.Equal(t, tc.counts, strings.Contains(stdout, `"enumValueCounts":{"active":1}`))
		})
	}
}

func TestAgentReportIncludeTestsFlag(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"go.mod":              "module example.com/fixture\n\ngo 1.24\n",
		"pkg/service.go":      "package pkg\nfunc Service() {}\n",
		"pkg/service_test.go": "package pkg\nfunc TestService() {}\n",
	})
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte("notes: {}\ncode:\n  enabled: true\n"), 0o644))
	t.Cleanup(func() { indexCmd.SetErr(nil) })
	_, indexStderr, indexErr := runRootCLI(t, nil, []string{"index", "--vault", vault.name})
	require.NoError(t, indexErr, indexStderr)
	for _, tc := range []struct {
		flag  string
		files int
	}{{"", 1}, {"--include-tests", 2}} {
		args := []string{"agent", "report", "--vault", vault.name, "--op", "relatedness", "--path", "pkg"}
		if tc.flag != "" {
			args = append(args, tc.flag)
		}
		stdout, stderr, err := runRootCLI(t, nil, args)
		require.NoError(t, err, stderr)
		var response struct {
			Data struct {
				Packages []struct {
					Files int `json:"files"`
				} `json:"packages"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &response))
		require.Len(t, response.Data.Packages, 1)
		require.Equal(t, tc.files, response.Data.Packages[0].Files)
	}
}

func TestAgentGraphSummaryFlag(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": "type Project @node(paths: [\"notes/projects/*.md\"]) {\n  name: String!\n}\n",
		"notes/projects/A.md":              "---\ntype: Project\nname: A\n---\n[[notes/projects/B]]\n",
		"notes/projects/B.md":              "---\ntype: Project\nname: B\n---\n[[notes/projects/C]]\n",
		"notes/projects/C.md":              "---\ntype: Project\nname: C\n---\n[[notes/projects/A]]\n",
	})
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	t.Cleanup(func() { indexCmd.SetErr(nil) })
	_, indexStderr, indexErr := runRootCLI(t, nil, []string{"index", "--vault", vault.name})
	require.NoError(t, indexErr, indexStderr)
	for _, tc := range []struct {
		name         string
		args         []string
		want, absent string
	}{
		{"ontology preferred", []string{"agent", "vault-context", "--profile", "vault", "--vault", vault.name}, "Project", "## Communities"},
		{"graph requested", []string{"agent", "vault-context", "--profile", "vault", "--graph-summary", "--vault", vault.name}, "## Communities", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runRootCLI(t, nil, tc.args)
			require.NoError(t, err, stderr)
			require.Contains(t, stdout, tc.want)
			if tc.absent != "" {
				require.NotContains(t, stdout, tc.absent)
			}
		})
	}
}

func TestAgentStartCommandOutputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "start", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp struct {
		SessionID      string                    `json:"sessionId"`
		SurfaceCommand string                    `json:"surfaceCommand"`
		Notes          []string                  `json:"notes"`
		Code           agentcode.SurfaceResponse `json:"code"`
		VaultContext   struct {
			Text string `json:"text"`
		} `json:"vaultContext"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stdout), &raw))
	require.NotContains(t, raw, "surface")
	require.NotContains(t, raw, "diagnostics")
	require.NotContains(t, string(raw["code"]), "inputSchema")
	require.NotContains(t, string(raw["code"]), "outputSchema")
	require.NotEmpty(t, resp.SessionID)
	require.Equal(t, "rzm agent surface", resp.SurfaceCommand)
	require.Equal(t, "1", resp.Code.FormatVersion)
	require.NotEmpty(t, resp.Code.VersionHash)
	require.NotEmpty(t, resp.Code.Examples)
	require.NotEmpty(t, resp.Code.Operations)
	var foundFiles, foundVaultContext bool
	for _, operation := range resp.Code.Operations {
		foundFiles = foundFiles || operation.Name == "files"
		foundVaultContext = foundVaultContext || operation.Name == "vault_context"
	}
	require.True(t, foundFiles)
	require.True(t, foundVaultContext)
	require.Contains(t, strings.Join(resp.Notes, "\n"), "Run `rzm agent surface` only when you need detailed command flags, examples, or subcommands")
	require.Contains(t, resp.VaultContext.Text, "tool: vault_context")
	require.NotContains(t, resp.VaultContext.Text, "hello world", "minimal start must not read or emit vault notes")
}

func TestAgentStartCommandTimingsExposeStableDiagnostics(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "start", "--timings", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp struct {
		Diagnostics struct {
			Phases []struct {
				Label      string `json:"label"`
				DurationMs int64  `json:"durationMs"`
				Count      int64  `json:"count"`
			} `json:"phases"`
			Operations []struct {
				Label     string `json:"label"`
				Count     int64  `json:"count"`
				Available bool   `json:"available"`
			} `json:"operations"`
		} `json:"diagnostics"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.NotEmpty(t, resp.Diagnostics.Phases)
	require.Equal(t, "agent_start.total", resp.Diagnostics.Phases[0].Label)
	require.Equal(t, int64(1), resp.Diagnostics.Phases[0].Count)
	require.GreaterOrEqual(t, resp.Diagnostics.Phases[0].DurationMs, int64(0))
	require.NotEmpty(t, resp.Diagnostics.Operations)
	require.Equal(t, "agent_start.note.passes", resp.Diagnostics.Operations[0].Label)
	require.Zero(t, resp.Diagnostics.Operations[0].Count)
	operationCounts := make(map[string]int64, len(resp.Diagnostics.Operations))
	operationAvailability := make(map[string]bool, len(resp.Diagnostics.Operations))
	for _, operation := range resp.Diagnostics.Operations {
		operationCounts[operation.Label] = operation.Count
		operationAvailability[operation.Label] = operation.Available
	}
	require.Zero(t, operationCounts["agent_start.repo.walks"])
	require.Zero(t, operationCounts["agent_start.note.reads"])
	require.False(t, operationAvailability["agent_start.sqlite.schema_statements"])
	require.Zero(t, operationCounts["agent_start.sqlite.schema_statements"])
}

func TestAgentStartExactRichDirectoryUsesIndexedFailSoftPathWithZeroLiveWork(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"docs/specs/example.md": "# Example\n",
		"pkg/example.go":        "package example\n",
	})
	t.Chdir(vault.path)
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte("notes: {}\ncode:\n  enabled: true\n"), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "start", "--timings", "--profile", "code", "--ontology",
		"--file", "docs/specs", "--submodule-depth", "1",
		"--intent", "Test out some rhizome stuff", "--vault", vault.name,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp agentStartResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.NotEmpty(t, resp.SessionID)
	require.NotNil(t, resp.VaultContext.IndexedEnrichment)
	require.Equal(t, actions.IndexedContextMissing, resp.VaultContext.IndexedEnrichment.Status)
	require.NotNil(t, resp.Ontology)
	require.False(t, resp.Ontology.Available)
	require.Empty(t, resp.OntologyError)
	require.NotEmpty(t, resp.VaultContext.IndexedEnrichment.Warnings)
	require.Contains(t, resp.VaultContext.Text, "Repository guidance")
	foundSemanticPhase := false
	for _, phase := range resp.Diagnostics.Phases {
		if phase.Label == "agent_start.runtime.semantic_ready" {
			foundSemanticPhase = true
			require.Zero(t, phase.Count, "omitted semantic capability must not be awaited")
		}
	}
	require.True(t, foundSemanticPhase)

	counts := make(map[string]int64)
	for _, operation := range resp.Diagnostics.Operations {
		counts[operation.Label] = operation.Count
	}
	for _, label := range []string{
		indexingperf.AgentStartOpNotePasses,
		indexingperf.AgentStartOpRepoWalks,
		indexingperf.AgentStartOpNoteReads,
		indexingperf.AgentStartOpCodeReads,
		indexingperf.AgentStartOpIndexWrites,
	} {
		_, present := counts[label]
		require.True(t, present, label)
		require.Zero(t, counts[label], label)
	}
	_, present := counts[indexingperf.AgentStartOpIndexedReads]
	require.True(t, present)
	require.Positive(t, counts[indexingperf.AgentStartOpIndexedReads])
	_, present = counts[indexingperf.AgentStartOpIndexedStatusMissing]
	require.True(t, present)
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpIndexedStatusMissing])

	t.Cleanup(func() { indexCmd.SetErr(nil) })
	_, indexStderr, indexErr := runRootCLI(t, nil, []string{"index", "--vault", vault.name})
	require.NoError(t, indexErr, indexStderr)
	currentStdout, currentStderr, currentErr := runRootCLI(t, nil, []string{
		"agent", "start", "--timings", "--profile", "vault", "--file", "docs/specs", "--vault", vault.name,
	})
	require.NoError(t, currentErr)
	require.Empty(t, currentStderr)
	var current agentStartResponse
	require.NoError(t, json.Unmarshal([]byte(currentStdout), &current))
	require.NotEmpty(t, current.SessionID)
	require.NotNil(t, current.VaultContext.IndexedEnrichment)
	require.Positive(t, current.VaultContext.IndexedEnrichment.Reads)
	currentCounts := make(map[string]int64)
	for _, operation := range current.Diagnostics.Operations {
		currentCounts[operation.Label] = operation.Count
	}
	require.Equal(t, int64(2), currentCounts[indexingperf.AgentStartOpIndexedReads])
	require.Equal(t, int64(1), currentCounts[indexingperf.AgentStartOpIndexedStatusMissing])
	for _, label := range []string{indexingperf.AgentStartOpNotePasses, indexingperf.AgentStartOpRepoWalks, indexingperf.AgentStartOpNoteReads, indexingperf.AgentStartOpCodeReads, indexingperf.AgentStartOpIndexWrites} {
		_, present := currentCounts[label]
		require.True(t, present, label)
		require.Zero(t, currentCounts[label], label)
	}
}

func TestAgentStartWithoutIndexedStoreKeepsGuidanceAndOmitsOntology(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"docs/specs/example.md": "# Example\n"})
	indexPath := filepath.Join(vault.path, ".rhizome", "db.sqlite")
	require.NoError(t, os.Remove(indexPath))
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "start", "--vault", vault.name, "--file", "docs/specs", "--ontology"})
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)
	var resp agentStartResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.NotEmpty(t, resp.SessionID)
	require.Contains(t, resp.VaultContext.Text, "Repository guidance")
	require.Nil(t, resp.Ontology)
	require.Empty(t, resp.OntologyError)
	for _, path := range []string{indexPath, indexPath + "-wal", indexPath + "-shm"} {
		require.NoFileExists(t, path)
	}
}

func TestAgentStartMinimalDirectoryOmitsSemanticReadiness(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"docs/specs/example.md": "# Example\n", "pkg/example.go": "package example\n"})
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "start", "--timings", "--vault", vault.name, "--profile", "code", "--file", "docs/specs"})
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)
	var resp agentStartResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.NotEmpty(t, resp.SessionID)
	found := false
	for _, phase := range resp.Diagnostics.Phases {
		if phase.Label == "agent_start.runtime.semantic_ready" {
			found = true
			require.Zero(t, phase.Count)
		}
	}
	require.True(t, found)
}

func TestAgentValidateBrokenLinksReturnsJSONFailure(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"A.md": "link [[Missing]]\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "broken-links", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	var resp struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
		Checks     []struct {
			Name       string `json:"name"`
			IssueCount int    `json:"issueCount"`
		} `json:"checks"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.False(t, resp.OK)
	require.Greater(t, resp.IssueCount, 0)
	require.NotEmpty(t, resp.Checks)
	require.Equal(t, "broken-links", resp.Checks[0].Name)
	require.Greater(t, resp.Checks[0].IssueCount, 0)
}

func TestAgentValidateCodeFrontmatterKeepsStderrJSONOnly(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/config.yml": "notes: {}\ncode:\n  enabled: true\n",
		"BadAnchors.md": `---
code-anchors:
  go:
    - label: bad
      symbol: Foo
---
# Bad Anchors
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "code-frontmatter", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	var resp struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
		Checks     []struct {
			Name       string `json:"name"`
			IssueCount int    `json:"issueCount"`
			Summary    string `json:"summary"`
		} `json:"checks"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.False(t, resp.OK)
	require.Greater(t, resp.IssueCount, 0)
	found := false
	for _, check := range resp.Checks {
		if check.Name != "code-frontmatter" {
			continue
		}
		found = true
		require.Greater(t, check.IssueCount, 0)
		require.Contains(t, check.Summary, "invalid notes")
	}
	require.True(t, found, "expected code_frontmatter check in response")
}

func TestAgentFilesCommandOutputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "files", "--vault", vault.name, "--input", "Alpha.md", "--include-content", "true", "--limit", "1"})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp agentapi.FilesResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.Len(t, resp.Files, 1)
	require.Equal(t, "Alpha.md", resp.Files[0].Path)
	require.Contains(t, resp.Files[0].Content, "hello world")
}

func TestAgentFilesDepthZeroCodePathQueryIncludesIndexedCode(t *testing.T) {
	for _, tc := range []struct{ name, input, path, body string }{
		{"code file", "pkg/example.go", "pkg/example.go", "package example\n\nfunc Run() {}\n"},
		{"markdown named directory", "docs.md", "docs.md/main.go", "package docs\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := setupAgentTestVault(t, map[string]string{tc.path: tc.body})
			require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte("notes: {}\ncode:\n  enabled: true\n"), 0o644))
			store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			require.NoError(t, store.ReplaceFileSummary(context.Background(), codeanchor.FileSummary{FilePath: tc.path, Lang: codeanchor.LangGo, Hash: tc.name}))
			config, err := obsidian.LoadLocalConfig(vault.path)
			require.NoError(t, err)
			require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
			require.NoError(t, store.SetScopeConfigHash(context.Background(), config.ScopeConfigHash()))
			require.NoError(t, store.Close())
			stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "files", "--vault", vault.name, "--input", tc.input, "--include-content", "false"})
			require.NoError(t, err, "stderr=%s", stderr)
			require.Empty(t, stderr)
			var resp agentapi.FilesResponse
			require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
			require.Len(t, resp.Files, 1)
			require.Equal(t, tc.path, resp.Files[0].Path)
		})
	}
}

func TestAgentFilesContinuationUsesDecodedGraphDepth(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\n[[Beta]] [[Gamma]]\n",
		"Beta.md":  "# Beta\n",
		"Gamma.md": "# Gamma\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "files", "--vault", vault.name,
		"--input", "Alpha.md", "--max-depth", "1", "--limit", "1",
	})
	require.NoError(t, err, "stderr=%s", stderr)
	require.Empty(t, stderr)
	var first agentapi.FilesResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &first))
	require.Len(t, first.Files, 1)
	require.Equal(t, "Alpha.md", first.Files[0].Path)
	require.Equal(t, 3, first.Total)
	require.NotEmpty(t, first.ContinuationToken)

	stdout, stderr, err = runRootCLI(t, nil, []string{
		"agent", "files", "--vault", vault.name,
		"--continuation-token", first.ContinuationToken,
	})
	require.NoError(t, err, "stderr=%s", stderr)
	require.Empty(t, stderr)
	var next agentapi.FilesResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &next))
	require.Len(t, next.Files, 1)
	require.Equal(t, "Beta.md", next.Files[0].Path)
	require.Equal(t, 3, next.Total)
	require.NotEmpty(t, next.ContinuationToken)
	stdout, stderr, err = runRootCLI(t, nil, []string{"agent", "files", "--vault", vault.name, "--continuation-token", next.ContinuationToken})
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)
	var last agentapi.FilesResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &last))
	require.Len(t, last.Files, 1)
	require.Equal(t, "Gamma.md", last.Files[0].Path)
	require.Equal(t, 3, last.Total)
	require.Empty(t, last.ContinuationToken)
}

func TestAgentReportValidationErrorIsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"Alpha.md": "# Alpha\n"})
	indexPath := filepath.Join(vault.path, ".rhizome", "db.sqlite")
	require.NoError(t, os.Remove(indexPath))
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing operation", nil, "op is required"},
		{"invalid operation", []string{"--op", "unknown"}, "op must be one of doc_coverage, complexity, hotspots, rationale_attention, relatedness, code_similarity"},
		{"session ID", []string{"--session-id", "test-session"}, "op is required"},
		{"session alias", []string{"--session", "test-session"}, "op is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"agent", "report", "--vault", vault.name}, tc.args...)
			stdout, stderr, err := runRootCLI(t, nil, args)
			require.Error(t, err)
			require.Empty(t, stdout)
			var payload map[string]string
			require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
			require.Equal(t, tc.want, payload["error"])
			require.NoFileExists(t, indexPath)
		})
	}
}

func TestAgentReportMissingIndexReturnsStructuredRemediationWithoutCreatingState(t *testing.T) {
	for _, state := range []string{"missing", "stale"} {
		t.Run(state, func(t *testing.T) {
			vault := setupAgentTestVault(t, map[string]string{"Alpha.md": "# Alpha\n"})
			indexPath := filepath.Join(vault.path, ".rhizome", "db.sqlite")
			if state == "missing" {
				require.NoError(t, os.Remove(indexPath))
			} else {
				store, err := semdb.Open(indexPath)
				require.NoError(t, err)
				require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
				require.NoError(t, store.SetScopeConfigHash(context.Background(), "stale-scope"))
				require.NoError(t, store.Close())
			}
			stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "report", "--vault", vault.name, "--op", "doc_coverage"})
			require.Error(t, err)
			require.Empty(t, stdout)
			var payload map[string]string
			require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
			require.Equal(t, "indexed-context-"+state, payload["code"])
			require.Equal(t, "rzm index", payload["remediation"])
			require.NotEmpty(t, payload["message"])
			require.NotContains(t, payload, "error")
			if state == "missing" {
				require.NoFileExists(t, indexPath)
			}
		})
	}
}

func TestAgentSessionAliasSharesDedupeStateWithSessionID(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "files", "--vault", vault.name, "--session", "shared-session",
		"--input", "Alpha.md", "--include-content", "true",
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var first agentapi.FilesResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &first))
	require.Len(t, first.Files, 1)
	require.Contains(t, first.Files[0].Content, "hello world")
	require.Empty(t, first.Files[0].ContentOmittedReason)

	stdout, stderr, err = runRootCLI(t, nil, []string{
		"agent", "files", "--vault", vault.name, "--session-id", "shared-session",
		"--input", "Alpha.md", "--include-content", "true",
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var second agentapi.FilesResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &second))
	require.Len(t, second.Files, 1)
	require.Equal(t, "deduped", second.Files[0].ContentOmittedReason)
	require.Greater(t, second.DedupeHits, 0)
}

func TestAgentSemanticQueryDedupeAcrossSeparateInvocations(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nKickoff project context.\n",
	})
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte(`
notes: {}
noteEmbeddings:
  enabled: true
  provider: test
  model: test
  dimensions: 8
code:
  enabled: true
`), 0o644))

	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	section := codeanchor.IntelDocSection{
		SectionID: "alpha-section", Path: "Alpha.md", Title: "Alpha", Level: 1,
		Content: "kickoff project", Fingerprint: "alpha-fingerprint",
	}
	require.NoError(t, store.ReplaceIntelDocSections(context.Background(), "Alpha.md", []codeanchor.IntelDocSection{section}, nil, nil))
	chunk := codeanchor.IntelChunk{
		ChunkID: "alpha-chunk", OwnerID: section.SectionID, OwnerType: "doc_section", Ord: 0,
		Granularity: "section", Breadcrumb: "Alpha", Heading: "Alpha", ContentHash: "alpha-content",
	}
	require.NoError(t, store.ReplaceIntelChunks(context.Background(), []string{section.SectionID}, []codeanchor.IntelChunk{chunk}))
	vectors, err := provider.EmbedTexts(context.Background(), []string{section.Content})
	require.NoError(t, err)
	require.NoError(t, store.UpsertEmbeddings(context.Background(), map[string]embeddings.Embedding{chunk.ChunkID: vectors[0]}))
	require.NoError(t, store.Close())
	noteStore, err := noteembsqlite.OpenWithMetadata(
		context.Background(),
		filepath.Join(vault.path, ".rhizome", "db.sqlite"),
		provider,
		embeddings.MetadataForProvider(provider, embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8}),
	)
	require.NoError(t, err)
	require.NoError(t, noteStore.Close())
	localCfg, err := obsidian.LoadLocalConfig(vault.path)
	require.NoError(t, err)
	store, err = semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(context.Background(), localCfg.ScopeConfigHash()))
	require.NoError(t, store.Close())

	args := []string{
		"agent", "semantic-query", "--vault", vault.name,
		"--session-id", "semantic-dedupe-session", "--query", "kickoff project", "--limit", "1",
	}
	stdout, stderr, err := runRootCLI(t, nil, args)
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var first agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &first))
	require.Equal(t, "semantic-dedupe-session", first.SessionID)
	require.Zero(t, first.DedupeHits)
	require.NotEmptyf(t, first.Matches, "semantic query response: %s", stdout)

	stdout, stderr, err = runRootCLI(t, nil, args)
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var second agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &second))
	require.Equal(t, "semantic-dedupe-session", second.SessionID)
	require.Positive(t, second.DedupeHits)
}

func TestAgentFileContextDedupeAcrossSeparateInvocations(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	args := []string{
		"agent", "file-context", "--vault", vault.name,
		"--session-id", "shared-file-context-session",
		"--file", "Alpha.md", "--profile", "vault",
		"--anchor-kind", "function",
	}

	stdout, stderr, err := runRootCLI(t, nil, args)
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var first struct {
		DedupeHits int    `json:"dedupeHits"`
		Text       string `json:"text"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &first))
	require.Zero(t, first.DedupeHits)
	require.Contains(t, first.Text, "## Alpha (note)")

	stdout, stderr, err = runRootCLI(t, nil, args)
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var second struct {
		DedupeHits int    `json:"dedupeHits"`
		Text       string `json:"text"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &second))
	require.Positive(t, second.DedupeHits)
	require.NotContains(t, second.Text, "## Alpha (note)")
}

func TestAgentVaultContextDedupeDegradesWithoutSuppressingFirstResponse(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	args := []string{
		"agent", "vault-context", "--vault", vault.name,
		"--session-id", "shared-vault-context-session", "--profile", "code",
	}

	stdout, stderr, err := runRootCLI(t, nil, args)
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var first struct {
		DedupeHits int    `json:"dedupeHits"`
		Text       string `json:"text"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &first))
	require.Contains(t, first.Text, "tool: vault_context")

	stdout, stderr, err = runRootCLI(t, nil, args)
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var second struct {
		DedupeHits int    `json:"dedupeHits"`
		Text       string `json:"text"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &second))
	require.Positive(t, second.DedupeHits)
	require.Contains(t, second.Text, "tool: vault_context")
}

func TestAgentFilesInvalidContinuationTokenIsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "files", "--vault", vault.name, "--continuation-token", "not-a-token"})
	require.Error(t, err)
	require.Empty(t, stdout)

	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
	require.Contains(t, payload["error"], "invalid continuationToken")
}

func TestBuildAgentConfigDisablesCompressionForAgentCLI(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	t.Setenv("CEREBRAS_API_KEY", "test-key")
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte("notes: {}\ncompression:\n  enabled: true\n"), 0o644))

	transport := &promptTestTransport{}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "files", "--vault", vault.name, "--input", "Alpha.md", "--include-content", "compress"})
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)
	var response agentapi.FilesResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &response))
	require.Len(t, response.Files, 1)
	require.Contains(t, response.Files[0].Content, "hello world")
	require.Zero(t, transport.calls)
	vaultName = vault.name
	for _, tc := range []struct{ override, want int }{{0, 150000}, {4321, 4321}} {
		cfg, rt, err := buildAgentConfigForOperation(context.Background(), tc.override, "files", "agent.files")
		require.NoError(t, err)
		require.Equal(t, tc.want, cfg.BudgetChars())
		if rt != nil {
			require.NoError(t, rt.Close())
		}
	}
}

func TestFilesRequestPlanUsesSessionWriterWithoutCodeIndex(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	vaultName = vault.name
	args := map[string]any{"inputs": []string{"Alpha.md"}, "sessionId": "stable"}
	require.NoError(t, normalizeAgentRequestPlanArgs(context.Background(), "agent.files", args))
	plan, err := oneshotruntime.RequestPlan("agent.files", args)
	require.NoError(t, err)
	cfg, rt, err := buildAgentConfigForRequestPlan(context.Background(), 0, "files", "agent.files", plan)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rt.Close())
		waitForVaultFileRelease(t, vault.path)
	})

	require.Eventually(t, func() bool {
		return cfg.Runtime.Snapshot().Code.Done
	}, 2*time.Second, 10*time.Millisecond)

	snapshot := cfg.Runtime.Snapshot()
	require.True(t, snapshot.Code.Done)
	require.False(t, snapshot.Code.Ready)
	require.ErrorContains(t, snapshot.Code.Err, "not requested")
	require.NotNil(t, snapshot.SessionStore)
	require.Nil(t, cfg.GetIntelStore())
}

func TestFilesRequestPlanWithoutSessionDoesNotOpenRuntimeOrSQLiteSidecars(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	vaultName = vault.name
	args := map[string]any{"inputs": []string{"Alpha.md"}}
	require.NoError(t, normalizeAgentRequestPlanArgs(context.Background(), "agent.files", args))
	plan, err := oneshotruntime.RequestPlan("agent.files", args)
	require.NoError(t, err)
	require.False(t, plan.RequiresRuntime())
	cfg, rt, err := buildAgentConfigForRequestPlan(context.Background(), 0, "files", "agent.files", plan)
	require.NoError(t, err)
	require.Nil(t, rt)

	payload, err := agentapi.CallJSON(context.Background(), cfg, "files", map[string]any{"inputs": []any{"Alpha.md"}})
	require.NoError(t, err)
	require.Contains(t, string(payload), "Alpha.md")
	for _, suffix := range []string{"-wal", "-shm"} {
		_, statErr := os.Stat(filepath.Join(vault.path, ".rhizome", "db.sqlite") + suffix)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}
}

func TestPlannedLiveFallbackActionsDoNotCreateMetadataStore(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		want  string
		files map[string]string
	}{
		{name: "files", args: []string{"files", "--input", "Alpha.md", "--include-content", "true"}, want: "Alpha.md"},
		{name: "list tags", args: []string{"list-tags", "--match", "find:Alpha"}, want: "tags"},
		{name: "list properties", args: []string{"list-properties", "--match", "find:Alpha"}, want: "properties"},
		{name: "community graph", args: []string{"community-list"}, want: "communities"},
		{name: "vault health", args: []string{"vault-health"}, want: "totalNotes"},
		{name: "code profile vault context", args: []string{"vault-context", "--profile", "code", "--file", "pkg/example.go"}, want: "pkg/example.go", files: map[string]string{"pkg/example.go": "package example\n\nfunc Run() {}\n"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := tt.files
			if files == nil {
				files = map[string]string{
					"Alpha.md": "---\nstatus: active\n---\n# Alpha\n\n#alpha\n[[Beta]]\n",
					"Beta.md":  "# Beta\n",
				}
			}
			vault := setupAgentTestVault(t, files)
			indexPath := filepath.Join(vault.path, ".rhizome", "db.sqlite")
			require.NoError(t, os.Remove(indexPath))

			args := append([]string{"agent"}, tt.args...)
			args = append(args, "--vault", vault.name)
			stdout, stderr, err := runRootCLI(t, nil, args)
			require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
			require.Empty(t, stderr)
			require.Contains(t, stdout, tt.want)
			if tt.name == "files" {
				var response agentapi.FilesResponse
				require.NoError(t, json.Unmarshal([]byte(stdout), &response))
				require.Len(t, response.Files, 1)
				require.Equal(t, "Alpha.md", response.Files[0].Path)
				require.Contains(t, response.Files[0].Content, "status: active")
			}
			for _, path := range []string{indexPath, indexPath + "-wal", indexPath + "-shm"} {
				require.NoFileExists(t, path)
			}
		})
	}
}

func TestPlannedReadOnlyActionsDoNotMutateSQLite(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "files graph enrichment", args: []string{"files", "--input", "Alpha.md", "--max-depth", "1"}, want: "Alpha.md"},
		{name: "community graph", args: []string{"community-list"}, want: "communities"},
		{name: "vault health without graph snapshot", args: []string{"vault-health"}, want: `"totalNotes":2`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vault := setupAgentTestVault(t, map[string]string{
				"Alpha.md": "# Alpha\n",
				"Beta.md":  "# Beta\n",
			})
			if tt.name == "files graph enrichment" {
				t.Cleanup(func() { indexCmd.SetErr(nil) })
				_, stderr, err := runRootCLI(t, nil, []string{"index", "--vault", vault.name})
				require.NoError(t, err, stderr)
			}
			indexPath := filepath.Join(vault.path, ".rhizome", "db.sqlite")
			store, err := semdb.Open(indexPath)
			require.NoError(t, err)
			if tt.name == "files graph enrichment" {
				require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), semdb.OntologySnapshot{
					Edges:       []semdb.OntologyEdgeRow{{SrcPath: "Alpha.md", RelationName: "related", DstPath: "Beta.md", DstType: "Note", Provenance: "field", Structural: true}},
					SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
				}))
			}
			cfg, err := obsidian.LoadLocalConfig(vault.path)
			require.NoError(t, err)
			require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
			require.NoError(t, store.SetScopeConfigHash(context.Background(), cfg.ScopeConfigHash()))
			require.NoError(t, store.Close())
			for _, path := range []string{indexPath + "-wal", indexPath + "-shm"} {
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					require.NoError(t, err)
				}
			}
			before, err := os.ReadFile(indexPath)
			require.NoError(t, err)

			args := append([]string{"agent"}, tt.args...)
			args = append(args, "--vault", vault.name)
			stdout, stderr, err := runRootCLI(t, nil, args)
			require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
			require.Empty(t, stderr)
			require.Contains(t, stdout, tt.want)
			if tt.name == "files graph enrichment" {
				require.Contains(t, stdout, "Beta.md")
			}
			after, err := os.ReadFile(indexPath)
			require.NoError(t, err)
			require.Equal(t, before, after, "query-only reads must not migrate, repair, or mutate indexed data")
		})
	}
}

func TestBuildAgentConfigFileContextUsesScopedReadOnlyRuntime(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	markAgentTestIndexCurrent(t, vault)

	vaultName = vault.name
	cfg, rt, err := buildAgentConfigForOperation(context.Background(), 0, "file_context", agentOperationIDForToolName("file_context"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rt.Close())
		waitForVaultFileRelease(t, vault.path)
	})

	snapshot := cfg.Runtime.Snapshot()
	require.True(t, snapshot.Search.Done)
	require.False(t, snapshot.Search.Ready)
	require.ErrorContains(t, snapshot.Search.Err, "not requested")
	require.True(t, snapshot.Semantic.Done)
	require.False(t, snapshot.Semantic.Ready)
	require.ErrorContains(t, snapshot.Semantic.Err, "not requested")
	require.True(t, snapshot.Code.Done)
	require.True(t, snapshot.Code.Ready, "code index readiness error: %v", snapshot.Code.Err)
	require.Nil(t, cfg.Cache)
	require.Nil(t, snapshot.NoteIndex)
	require.NotNil(t, cfg.GetIntelStore())
	require.NotNil(t, snapshot.SessionStore)
	require.NotSame(t, cfg.GetIntelStore(), snapshot.SessionStore)
	require.Error(t, cfg.GetIntelStore().EnsureSession(context.Background(), "read-only-session"))
	require.NoError(t, snapshot.SessionStore.EnsureSession(context.Background(), "writable-session"))
	require.Nil(t, rt.NoteSyncer())
	require.Nil(t, rt.CodeSyncer())
}

func TestBuildAgentConfigCodeSymbolContextExemptionKeepsLiveStore(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"pkg/a.go": "package pkg\n",
	})

	vaultName = vault.name
	cfg, rt, err := buildAgentConfigForOperation(context.Background(), 0, "code_symbol_context", agentOperationIDForToolName("code_symbol_context"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rt.Close())
		waitForVaultFileRelease(t, vault.path)
	})

	require.NotNil(t, cfg.GetIntelStore())
	require.Equal(t, mcp.IntelStoreFallbackAllowed, cfg.IntelStorePolicy)
	require.NoError(t, cfg.GetIntelStore().EnsureSession(context.Background(), "full-runtime-parity"))
}

func TestBuildAgentConfigExactIndexPlansExposeNoSessionStore(t *testing.T) {
	for _, toolName := range []string{"code_symbol", "code_references", "code_rationale", "graph_path"} {
		t.Run(toolName, func(t *testing.T) {
			vault := setupAgentTestVault(t, map[string]string{
				"pkg/a.go": "package pkg\n",
			})
			markAgentTestIndexCurrent(t, vault)
			vaultName = vault.name
			cfg, rt, err := buildAgentConfigForOperation(context.Background(), 0, toolName, agentOperationIDForToolName(toolName))
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, rt.Close())
				waitForVaultFileRelease(t, vault.path)
			})

			snapshot := cfg.Runtime.Snapshot()
			require.True(t, snapshot.Code.Done)
			require.True(t, snapshot.Code.Ready)
			require.True(t, snapshot.Search.Done)
			require.False(t, snapshot.Search.Ready)
			require.ErrorContains(t, snapshot.Search.Err, "not requested")
			require.True(t, snapshot.Semantic.Done)
			require.False(t, snapshot.Semantic.Ready)
			require.ErrorContains(t, snapshot.Semantic.Err, "not requested")
			require.Nil(t, cfg.Cache)
			require.NotNil(t, cfg.GetIntelStore())
			require.Nil(t, snapshot.SessionStore)
			require.Equal(t, mcp.IntelStoreManagedReadOnly, cfg.IntelStorePolicy)
			require.Error(t, cfg.GetIntelStore().EnsureSession(context.Background(), "read-only-session"))
		})
	}
}

func TestLiveRuntimeReportsExplicitlyOmittedCapabilitiesUnavailable(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"Alpha.md": "# Alpha\n"})
	rt, err := bootstrap.NewLiveRuntime(context.Background(), bootstrap.LiveOptions{
		VaultName:         vault.name,
		DisableLeaderWork: true,
		Requirements:      bootstrap.RequireRuntimeCapabilities(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rt.Close())
		waitForVaultFileRelease(t, vault.path)
	})

	require.ErrorContains(t, rt.WaitForSearch(context.Background()), "not requested")
	require.ErrorContains(t, rt.WaitForSemantic(context.Background()), "not requested")
	require.ErrorContains(t, rt.WaitForCodeIndex(context.Background()), "not requested")
	snapshot := rt.Snapshot()
	require.False(t, snapshot.Search.Ready)
	require.False(t, snapshot.Semantic.Ready)
	require.False(t, snapshot.Code.Ready)
	require.Nil(t, rt.Cache())
	require.Nil(t, snapshot.NoteIndex)
	require.Nil(t, snapshot.CodeIndex)
	require.Nil(t, snapshot.IntelStore)
	require.Nil(t, snapshot.SessionStore)
	require.Nil(t, rt.NoteSyncer())
	require.Nil(t, rt.CodeSyncer())
}

func TestBuildAgentConfigSemanticQueryWaitsForIndexedRuntimeWithoutCache(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	localCfg, err := obsidian.LoadLocalConfig(vault.path)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(context.Background(), localCfg.ScopeConfigHash()))
	require.NoError(t, store.Close())

	vaultName = vault.name
	plan, err := oneshotruntime.RequestPlan("agent.semantic-query", map[string]any{"sessionId": "query-session"})
	require.NoError(t, err)
	cfg, rt, err := buildAgentConfigForRequestPlan(context.Background(), 0, "semantic_query", "agent.semantic-query", plan)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rt.Close())
		waitForVaultFileRelease(t, vault.path)
	}()

	require.Same(t, rt, cfg.Runtime)
	require.True(t, cfg.IndexedReadOnlySemanticQuery)
	require.Nil(t, cfg.Cache)
	require.NotNil(t, cfg.GetIntelStore())
	require.NotNil(t, cfg.Runtime.Snapshot().SessionStore)
	snapshot := cfg.Runtime.Snapshot()
	require.True(t, snapshot.Semantic.Done)
	require.True(t, snapshot.Code.Done)
	require.True(t, snapshot.Code.Ready)
}

func TestBuildAgentConfigSemanticQueryWithholdsStaleIndexedRuntime(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})
	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(context.Background(), "stale-scope"))
	require.NoError(t, store.Close())

	vaultName = vault.name
	plan, err := oneshotruntime.RequestPlan("agent.semantic-query", nil)
	require.NoError(t, err)
	cfg, rt, err := buildAgentConfigForRequestPlan(context.Background(), 0, "semantic_query", "agent.semantic-query", plan)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, rt.Close())
		waitForVaultFileRelease(t, vault.path)
	})

	require.NotNil(t, cfg.IndexedContextUnavailable)
	require.Equal(t, actions.IndexedContextStale, cfg.IndexedContextUnavailable.State)
	require.Equal(t, "indexed-context-stale", cfg.IndexedContextUnavailable.WarningCode)
	require.Equal(t, "rzm index", cfg.IndexedContextUnavailable.Remediation)
	require.Nil(t, cfg.GetIntelStore(), "stale indexed state must not be published to semantic retrieval")

	payload, err := agentapi.CallJSON(context.Background(), cfg, "semantic_query", map[string]any{
		"queries": []string{"hello world"},
	})
	require.NoError(t, err)
	var response agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal(payload, &response))
	require.Len(t, response.Warnings, 1)
	require.Equal(t, "indexed-context-stale", response.Warnings[0].Code)
	require.Contains(t, response.Text, "rzm index")
}

func TestIndexedContextUnavailableClassifiesEmbeddingMetadataMismatchAsIncompatible(t *testing.T) {
	err := fmt.Errorf("note embedding index incompatible: %w", embeddings.MetadataError{Err: errors.New("dimensions mismatch: have 1024, expected 1536")})
	state := indexedContextUnavailableForError(err)
	require.Equal(t, actions.IndexedContextIncompatible, state.State)
	require.Equal(t, "indexed-context-incompatible", state.WarningCode)
	require.Equal(t, "rzm index --rebuild", state.Remediation)
}

func TestAgentRuntimeRejectsUndeclaredOperationBeforeOpeningStore(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprintf("absent=%t", absent), func(t *testing.T) {
			vault := setupAgentTestVault(t, map[string]string{"Alpha.md": "# Alpha\n"})
			indexPath := filepath.Join(vault.path, ".rhizome", "db.sqlite")
			if absent {
				require.NoError(t, os.Remove(indexPath))
			}
			var before []byte
			if !absent {
				var err error
				before, err = os.ReadFile(indexPath)
				require.NoError(t, err)
			}
			vaultName = vault.name
			cfg, rt, err := prepareAgentJSONTool(context.Background(), 0, "unknown", "agent.unknown", nil)
			if absent {
				for _, path := range []string{indexPath, indexPath + "-wal", indexPath + "-shm"} {
					require.NoFileExists(t, path)
				}
			} else {
				after, err := os.ReadFile(indexPath)
				require.NoError(t, err)
				require.Equal(t, before, after)
			}
			require.ErrorIs(t, err, oneshotruntime.ErrRequestPlanNotDeclared)
			require.ErrorContains(t, err, `operation "agent.unknown" has no runtime plan or exemption`)
			require.Nil(t, rt)
			require.Nil(t, cfg.Runtime)
		})
	}
}

func TestAgentFindConnectionsUsesCurrentSemanticStoreReadOnly(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "---\noffice: AOGR\n---\n# Alpha\n\nkickoff project context\n",
		"Beta.md":  "# Beta\n\nkickoff project followup\n",
	})
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte("notes: {}\nnoteEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\ncode:\n  enabled: true\n"), 0o644))
	t.Cleanup(func() { indexCmd.SetErr(nil) })
	_, indexStderr, indexErr := runRootCLI(t, nil, []string{"index", "--vault", vault.name})
	require.NoError(t, indexErr, indexStderr)
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	indexPath := filepath.Join(vault.path, ".rhizome", "db.sqlite")
	store, err := semdb.Open(indexPath)
	require.NoError(t, err)
	for _, tc := range []struct{ path, id, content string }{
		{"Alpha.md", "alpha", "kickoff project context"},
		{"Beta.md", "beta", "kickoff project followup"},
	} {
		section := codeanchor.IntelDocSection{SectionID: tc.id + "-section", Path: tc.path, Title: tc.id, Level: 1, Content: tc.content, Fingerprint: tc.id + "-fingerprint"}
		require.NoError(t, store.ReplaceIntelDocSections(context.Background(), tc.path, []codeanchor.IntelDocSection{section}, nil, nil))
		chunk := codeanchor.IntelChunk{ChunkID: tc.id + "-chunk", OwnerID: section.SectionID, OwnerType: "doc_section", Ord: 0, Granularity: "section", Breadcrumb: tc.id, Heading: tc.id, ContentHash: tc.id + "-content"}
		require.NoError(t, store.ReplaceIntelChunks(context.Background(), []string{section.SectionID}, []codeanchor.IntelChunk{chunk}))
		vectors, err := provider.EmbedTexts(context.Background(), []string{section.Content})
		require.NoError(t, err)
		require.NoError(t, store.UpsertEmbeddings(context.Background(), map[string]embeddings.Embedding{chunk.ChunkID: vectors[0]}))
	}
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	localCfg, err := obsidian.LoadLocalConfig(vault.path)
	require.NoError(t, err)
	require.NoError(t, store.SetScopeConfigHash(context.Background(), localCfg.ScopeConfigHash()))
	require.NoError(t, store.Close())
	noteStore, err := noteembsqlite.OpenWithMetadata(context.Background(), indexPath, provider, embeddings.MetadataForProvider(provider, embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8}))
	require.NoError(t, err)
	require.NoError(t, noteStore.Close())
	store, err = semdb.Open(indexPath)
	require.NoError(t, err)
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(context.Background(), "stale-code-scope"))
	require.NoError(t, store.Close())
	before, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	for _, tc := range []struct{ name, flag, value, want string }{
		{"text", "--text", "kickoff project", "Alpha.md"},
		{"note", "--note", "Alpha.md", "Beta.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "find-connections", "--vault", vault.name, tc.flag, tc.value})
			require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
			require.Empty(t, stderr)
			var payload struct {
				InputType string `json:"inputType"`
				Matches   []struct {
					Path string `json:"path"`
				} `json:"matches"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
			require.Equal(t, tc.name, payload.InputType)
			require.NotEmpty(t, payload.Matches, stdout)
			paths := make([]string, 0, len(payload.Matches))
			for _, match := range payload.Matches {
				paths = append(paths, match.Path)
			}
			require.Contains(t, paths, tc.want)
			after, err := os.ReadFile(indexPath)
			require.NoError(t, err)
			require.Equal(t, before, after, "query must not rewrite indexed metadata")
		})
	}
	store, err = semdb.Open(indexPath)
	require.NoError(t, err)
	require.NoError(t, store.SetScopeConfigHash(context.Background(), localCfg.ScopeConfigHash()))
	require.NoError(t, store.Close())
	before, err = os.ReadFile(indexPath)
	require.NoError(t, err)
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "semantic-query", "--vault", vault.name, "--query", "kickoff project", "--limit", "1"})
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var semantic agentapi.SemanticQueryResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &semantic))
	require.NotEmpty(t, semantic.Matches, stdout)
	after, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "semantic query must not rewrite indexed metadata")

	store, err = semdb.Open(indexPath)
	require.NoError(t, err)
	require.NoError(t, store.SetScopeConfigHash(context.Background(), "stale-code-scope"))
	require.NoError(t, store.Close())
	stdout, stderr, err = runRootCLI(t, nil, []string{"agent", "list-properties", "--vault", vault.name, "--only", "office"})
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)
	var properties mcp.PropertyListResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &properties))
	require.Len(t, properties.Properties, 1, stdout)
	require.Equal(t, "office", properties.Properties[0].Name)
}

type agentTestVault struct {
	name string
	path string
}

var agentTestDBTemplate struct {
	sync.Once
	data []byte
	err  error
}

func writeAgentTestDB(t *testing.T, target string) {
	t.Helper()
	agentTestDBTemplate.Do(func() {
		path := filepath.Join(t.TempDir(), "agent-test-template.db")
		store, err := semdb.Open(path)
		if err != nil {
			agentTestDBTemplate.err = err
			return
		}
		if err := store.Checkpoint(context.Background(), true); err != nil {
			_ = store.Close()
			agentTestDBTemplate.err = err
			return
		}
		if err := store.Close(); err != nil {
			agentTestDBTemplate.err = err
			return
		}
		agentTestDBTemplate.data, agentTestDBTemplate.err = os.ReadFile(path)
	})
	require.NoError(t, agentTestDBTemplate.err)
	require.NoError(t, os.WriteFile(target, agentTestDBTemplate.data, 0o600))
}

func markAgentTestIndexCurrent(t *testing.T, vault agentTestVault) {
	t.Helper()
	localCfg, err := obsidian.LoadLocalConfig(vault.path)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(context.Background(), localCfg.ScopeConfigHash()))
	require.NoError(t, store.Close())
}

func setupAgentTestVault(t *testing.T, files map[string]string) agentTestVault {
	t.Helper()
	resetAgentTestGlobals()
	resetFlags(rootCmd)

	rootDir := t.TempDir()
	vaultDir := filepath.Join(rootDir, "testvault")
	require.NoError(t, os.MkdirAll(vaultDir, 0o755))
	for rel, body := range files {
		target := filepath.Join(vaultDir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, []byte(body), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	if _, err := os.Stat(filepath.Join(vaultDir, ".rhizome", "config.yml")); errors.Is(err, os.ErrNotExist) {
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	}
	writeAgentTestDB(t, filepath.Join(vaultDir, ".rhizome", "db.sqlite"))

	configFile := filepath.Join(rootDir, "obsidian.json")
	configBody, err := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"testvault": map[string]string{"path": vaultDir},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configBody, 0o644))

	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return configFile, nil }
	t.Cleanup(func() {
		obsidian.ObsidianConfigFile = origConfig
		waitForVaultFileRelease(t, vaultDir)
		resetAgentTestGlobals()
		resetFlags(rootCmd)
		rootCmd.SetArgs([]string{})
	})

	return agentTestVault{name: "testvault", path: vaultDir}
}

func runRootCLI(t *testing.T, ctx context.Context, args []string) (stdout string, stderr string, runErr error) {
	return runRootCLIWithRootSilence(t, ctx, args, true)
}

func runRootCLIWithRootSilence(t *testing.T, ctx context.Context, args []string, rootSilence bool) (stdout string, stderr string, runErr error) {
	t.Helper()
	resetFlags(rootCmd)
	clearArrayFlags(rootCmd)

	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origStdout := os.Stdout
	origStderr := os.Stderr
	origSilenceUsage := rootCmd.SilenceUsage
	origSilenceErrors := rootCmd.SilenceErrors

	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	var (
		outBytes []byte
		errBytes []byte
		readWG   sync.WaitGroup
		readErrs = make(chan error, 2)
	)
	readPipe := func(dst *[]byte, src *os.File) {
		defer readWG.Done()
		data, readErr := io.ReadAll(src)
		if readErr != nil {
			readErrs <- readErr
			return
		}
		*dst = data
	}
	readWG.Add(2)
	go readPipe(&outBytes, outR)
	go readPipe(&errBytes, errR)

	os.Stdout = outW
	os.Stderr = errW
	rootCmd.SetOut(outW)
	rootCmd.SetErr(errW)
	rootCmd.SilenceUsage = rootSilence
	rootCmd.SilenceErrors = rootSilence
	rootCmd.SetArgs(args)
	if ctx != nil {
		runErr = rootCmd.ExecuteContext(ctx)
	} else {
		runErr = rootCmd.Execute()
	}
	rootCmd.SetArgs([]string{})

	require.NoError(t, outW.Close())
	require.NoError(t, errW.Close())
	readWG.Wait()
	close(readErrs)
	for readErr := range readErrs {
		require.NoError(t, readErr)
	}
	require.NoError(t, outR.Close())
	require.NoError(t, errR.Close())

	rootCmd.SetOut(origOut)
	rootCmd.SetErr(origErr)
	rootCmd.SilenceUsage = origSilenceUsage
	rootCmd.SilenceErrors = origSilenceErrors
	os.Stdout = origStdout
	os.Stderr = origStderr

	return strings.TrimSpace(string(outBytes)), strings.TrimSpace(string(errBytes)), runErr
}

func clearArrayFlags(cmd *cobra.Command) {
	clearArrayFlagSet(cmd.Flags())
	clearArrayFlagSet(cmd.PersistentFlags())
	for _, child := range cmd.Commands() {
		clearArrayFlags(child)
	}
}

func clearArrayFlagSet(fs *pflag.FlagSet) {
	if fs == nil {
		return
	}
	fs.VisitAll(func(f *pflag.Flag) {
		switch f.Value.Type() {
		case "stringSlice", "stringArray":
			if slice, ok := f.Value.(pflag.SliceValue); ok {
				_ = slice.Replace(nil)
			}
			f.Changed = false
		}
	})
}

func resetAgentTestGlobals() {
	vaultName = ""
	suppressTags = nil
	noSuppress = false
	debug = false
	skipAnchors = false
	skipEmbeds = false
}

func waitForVaultFileRelease(t *testing.T, vaultDir string) {
	t.Helper()
	paths := []string{
		filepath.Join(vaultDir, ".rhizome", "db.sqlite"),
		filepath.Join(vaultDir, ".rhizome", "db.sqlite-wal"),
		filepath.Join(vaultDir, ".rhizome", "db.sqlite-shm"),
	}
	rhizomeDir := filepath.Join(vaultDir, ".rhizome")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if sqliteFilesReleased(paths) && rhizomeDirQuiescent(rhizomeDir) {
			first, ok := snapshotDirTree(vaultDir)
			if ok {
				time.Sleep(100 * time.Millisecond)
				second, ok := snapshotDirTree(vaultDir)
				if ok && first == second && sqliteFilesReleased(paths) && rhizomeDirQuiescent(rhizomeDir) {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for vault files to be released: %s", vaultDir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func sqliteFilesReleased(paths []string) bool {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false
		}
		tmpPath := path + ".tmpcheck"
		if err := os.Rename(path, tmpPath); err != nil {
			return false
		}
		if err := os.Rename(tmpPath, path); err != nil {
			return false
		}
	}
	return true
}

func rhizomeDirQuiescent(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return os.IsNotExist(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		switch name {
		case "runtime.json", "runtime.json.tmp", "index.lock":
			return false
		}
	}
	return true
}

func snapshotDirTree(root string) (string, bool) {
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.WriteString(rel)
		b.WriteByte('|')
		if d.IsDir() {
			b.WriteString("dir")
		} else {
			b.WriteString(fmt.Sprintf("%d|%d", info.Size(), info.ModTime().UnixNano()))
		}
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		return "", false
	}
	return b.String(), true
}
