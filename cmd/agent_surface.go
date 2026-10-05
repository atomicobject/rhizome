package cmd

import (
	"os"
	"sort"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type agentSurfaceResponse struct {
	Version            string                        `json:"version"`
	Command            string                        `json:"command"`
	Mode               string                        `json:"mode"`
	StatusSnapshotKind string                        `json:"statusSnapshotKind"`
	VaultPath          string                        `json:"vaultPath,omitempty"`
	DefaultBudget      int                           `json:"defaultBudgetChars"`
	Capabilities       agentapi.CapabilitiesResponse `json:"capabilities"`
	Indexes            agentSurfaceIndexStatus       `json:"indexes,omitempty"`
	Compression        agentSurfaceBehavior          `json:"compression"`
	Notes              []string                      `json:"notes"`
	RetrievalLoop      []string                      `json:"retrievalLoop"`
	Commands           []agentSurfaceCommand         `json:"commands"`
}

type agentSurfaceBehavior struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Reason  string `json:"reason"`
}

type agentSurfaceIndexStatus struct {
	UnifiedDBPath   string `json:"unifiedDbPath,omitempty"`
	UnifiedDBExists bool   `json:"unifiedDbExists,omitempty"`
}

type agentSurfaceCommand = agentapi.SurfaceCommand
type agentSurfaceFlag = agentapi.SurfaceFlag

func newAgentSurfaceCmd() *cobra.Command {
	var selector string
	cmd := &cobra.Command{
		Use:   "surface",
		Short: "Print the authoritative rzm agent command surface as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			payload, err := selectAgentSurface(selector)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, payload)
		},
	}
	cmd.Flags().StringVar(&selector, "command", "", "return only one exact command contract, e.g. file-context or query-recipe run")
	return cmd
}

func buildAgentSurface() agentSurfaceResponse {
	vaultDef, _ := vaultDefOrDefault()
	vaultPath := vaultDef.BasePath()
	budget := effectiveAgentBudgetChars(vaultPath, 0)
	capabilities, indexes := buildAgentCapabilitiesSnapshot()
	resp := agentSurfaceResponse{
		Version:            version.Version,
		Command:            "rzm agent",
		Mode:               "standalone-cli",
		StatusSnapshotKind: "local-snapshot",
		VaultPath:          vaultPath,
		DefaultBudget:      budget,
		Capabilities:       capabilities,
		Indexes:            indexes,
		Compression: agentSurfaceBehavior{
			Enabled: false,
			Mode:    "disabled",
			Reason:  "rzm agent disables LLM compression and returns raw packed output for predictable local CLI use",
		},
		Notes: []string{
			"Authoritative agent-safe command contract; prefer this surface over `rzm agent <command> --help` in skills and automation.",
			"`capabilities` is a cheap local config/index snapshot, not a live server readiness probe.",
			"Common operational flags include `session-id` (`session` alias), `vault`, `no-suppress`, and suppress-tag variants; treat command-specific flags as the decision surface.",
			"Start each conversation or coherent work period once with `rzm agent start`, then reuse the returned sessionId.",
			"Use minimal bootstrap by default; add `--profile vault`, `--ontology`, `--graph-summary`, or Markdown-note targets only when the task explicitly needs rich vault or ontology context.",
			"In typed-note workflows, run named `query-recipe` packets before ad hoc ontology queries; use `ontology-query` for gaps, debugging, or authoring new recipes.",
			"Use `rzm note move` for Markdown file renames or moves; raw filesystem moves do not preserve links/coderefs.",
			"`rzm agent` is one-shot CLI mode: no watchhub, no leader election, no background cache warmup, no LLM compression, and a larger local budget floor.",
			"`semantic_query` is orientation evidence; `code-symbol`, `code-references`, and `code-symbol-context` are proof-oriented indexed code reads.",
		},
		RetrievalLoop: []string{
			"1. `rzm agent start --intent \"<task>\"`; add only a few high-value repeatable `--file <seed>` inputs, then reuse `sessionId`.",
			"2. If a skill or repo guide names a packet, run `rzm agent query-recipe run --id <id> --anchor <anchor>` before rediscovery.",
			"3. Unknown seed: `rzm agent semantic-query --query <q>` with one or a few focused questions; routing infers overview/doc-code modes.",
			"4. Known path: `rzm agent file-context --file <path> --intent \"<task>\"`; add `--submodule-depth 1` only when immediate child-module docs matter.",
			"5. Exact reads: `rzm agent files --include-content true --input <path>` after discovery.",
			"6. Exact symbols: `rzm agent code-symbol --symbol <symbol>` then `rzm agent code-references --symbol <symbol>` when proof is needed.",
			"7. Before handoff: `rzm agent validate <check>` for one task-relevant check, `rzm agent validate all` for comprehensive checks, and `rzm agent validate audit` separately for maintenance checks.",
		},
	}
	agentCommands := make(map[string]*cobra.Command, len(agentCmd.Commands()))
	for _, child := range agentCmd.Commands() {
		if child != nil && !child.Hidden {
			agentCommands[child.Name()] = child
		}
	}

	descriptors := agentapi.AgentCLICommandDescriptors()
	resp.Commands = make([]agentSurfaceCommand, 0, len(descriptors)+2)
	for _, descriptor := range descriptors {
		child := agentCommands[descriptor.AgentCommand]
		if child == nil {
			continue
		}
		resp.Commands = append(resp.Commands, agentSurfaceCommand{
			Name:        child.Name(),
			Use:         child.CommandPath(),
			Source:      "agent",
			Category:    string(descriptor.AgentCLICategory),
			ToolName:    agentSurfaceToolName(descriptor),
			Short:       child.Short,
			Examples:    agentSurfaceExamples(child.Name()),
			Flags:       collectAgentSurfaceFlags(child),
			Subcommands: collectAgentSurfaceSubcommands(child, descriptor),
		})
	}
	// `surface` describes the catalog itself and therefore has no descriptor or
	// handler. Keep this explicit self-documentation exception after all
	// catalog-backed agent commands to preserve the public ordering.
	if surfaceCmd := agentCommands["surface"]; surfaceCmd != nil {
		resp.Commands = append(resp.Commands, agentSurfaceCommand{
			Name:     surfaceCmd.Name(),
			Use:      surfaceCmd.CommandPath(),
			Source:   "agent",
			Category: "self",
			Short:    surfaceCmd.Short,
			Examples: agentSurfaceExamples(surfaceCmd.Name()),
			Flags:    collectAgentSurfaceFlags(surfaceCmd),
		})
	}
	// `rzm note move` is a safe top-level CLI command, not an agent/MCP tool.
	// It remains the only cross-command exception advertised by this surface.
	if moveCmd != nil && !moveCmd.Hidden {
		resp.Commands = append(resp.Commands, agentSurfaceCommand{
			Name:     "note-move",
			Use:      moveCmd.CommandPath(),
			Source:   "cli",
			Category: "safe_mutation",
			Short:    "Move or rename notes/attachments with backlinks updated by default",
			Flags:    collectAgentSurfaceFlags(moveCmd),
		})
	}
	return resp
}

func collectAgentSurfaceSubcommands(cmd *cobra.Command, descriptor agentapi.ToolDescriptor) []agentSurfaceCommand {
	var out []agentSurfaceCommand
	for _, child := range cmd.Commands() {
		if child == nil || child.Hidden {
			continue
		}
		out = append(out, agentSurfaceCommand{
			Name:     child.Name(),
			Use:      child.CommandPath(),
			Source:   "agent",
			Category: string(descriptor.AgentCLICategory),
			ToolName: agentSurfaceToolName(descriptor),
			Short:    child.Short,
			Flags:    collectAgentSurfaceFlags(child),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func agentSurfaceToolName(descriptor agentapi.ToolDescriptor) string {
	if descriptor.Kind == agentapi.ToolKindLocal {
		return ""
	}
	return descriptor.Name
}

func buildAgentCapabilitiesSnapshot() (agentapi.CapabilitiesResponse, agentSurfaceIndexStatus) {
	def, err := vaultDefOrDefault()
	if err != nil {
		return agentapi.CapabilitiesResponse{
			Server: agentapi.CapabilitiesServer{
				Version:   version.Version,
				ReadWrite: false,
				Ready:     true,
			},
			Tools:   agentCapabilitiesTools(),
			Limits:  agentapi.CapabilitiesLimits{BudgetChars: agentCLIBudgetFloor},
			Reports: agentCapabilitiesReports(),
		}, agentSurfaceIndexStatus{}
	}
	vaultPath := def.BasePath()
	budget := effectiveAgentBudgetChars(vaultPath, 0)
	resp := agentapi.CapabilitiesResponse{
		Server: agentapi.CapabilitiesServer{
			Version:   version.Version,
			ReadWrite: false,
			Ready:     true,
		},
		Vault: agentapi.CapabilitiesVault{
			Name:         def.Name,
			Path:         vaultPath,
			IsCollection: def.IsCollection(),
		},
		Tools:   agentCapabilitiesTools(),
		Limits:  agentapi.CapabilitiesLimits{BudgetChars: budget},
		Reports: agentCapabilitiesReports(),
	}
	indexes := agentSurfaceIndexStatus{}

	if codeCfg, err := obsidian.LoadCodeConfig(vaultPath); err == nil {
		resp.Features.CodeAnchors = codeCfg.Enabled
		resp.Features.CodeIndex = codeCfg.Enabled
		resp.Langs = configuredCodeLangs(codeCfg)
		if indexes.UnifiedDBPath == "" {
			indexes.UnifiedDBPath = obsidian.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
		}
	}
	if embCfg, err := obsidian.LoadEmbeddingsConfig(vaultPath); err == nil {
		resp.Features.NoteEmbeddings = embCfg.Enabled
		if indexes.UnifiedDBPath == "" {
			indexes.UnifiedDBPath = obsidian.UnifiedIndexPath(vaultPath, embCfg.IndexPath)
		}
		if codeEmbCfg, _, codeEmbErr := obsidian.EffectiveCodeEmbeddingsConfig(vaultPath, embCfg); codeEmbErr == nil {
			resp.Features.CodeEmbeddings = codeEmbCfg.Enabled
		}
	}
	if indexes.UnifiedDBPath != "" {
		if _, err := os.Stat(indexes.UnifiedDBPath); err == nil {
			indexes.UnifiedDBExists = true
		}
	}
	return resp, indexes
}

func agentCapabilitiesTools() agentapi.CapabilitiesTools {
	return agentapi.CapabilitiesTools{
		Available: agentapi.AgentCapabilityToolNames(),
		Mutating:  agentapi.AgentCapabilityMutatingToolNames(false),
	}
}

func agentCapabilitiesReports() agentapi.CapabilitiesReports {
	return agentapi.CapabilitiesReports{
		Ops: []string{"doc_coverage", "complexity", "hotspots", "rationale_attention", "relatedness", "code_similarity"},
	}
}

func configuredCodeLangs(cfg codeanchor.Config) []string {
	langs := make([]string, 0, 5)
	if len(cfg.GoRoots) > 0 {
		langs = append(langs, "go")
	}
	if len(cfg.PythonRoots) > 0 {
		langs = append(langs, "python")
	}
	if len(cfg.TSRoots) > 0 {
		langs = append(langs, "ts")
	}
	if len(cfg.CSharpRoots) > 0 {
		langs = append(langs, "cs")
	}
	if len(cfg.PHPRoots) > 0 {
		langs = append(langs, "php")
	}
	return langs
}

func agentSurfaceExamples(name string) []string {
	switch name {
	case "node-link":
		return []string{
			"rzm agent node-link --target 'docs/specs/technical/example.md#Story A' --ensure plan",
			"rzm agent node-link --ref '{\"notePath\":\"docs/specs/technical/example.md\",\"fragment\":\"^SPEC-0001-US1\",\"kind\":\"EMBEDDED\"}'",
		}
	case "semantic-query":
		return []string{
			"rzm agent semantic-query --query \"search ranking pipeline overview\"",
			"rzm agent semantic-query --path pkg/search --query \"search subsystem overview\"",
			"rzm agent semantic-query --path pkg/search/service.go --query \"docs for this implementation\"",
		}
	case "code-symbol":
		return []string{
			"rzm agent code-symbol --symbol Service.Search --language go",
			"rzm agent code-symbol --symbol Search --path pkg/search/service.go --language go",
		}
	case "code-references":
		return []string{
			"rzm agent code-references --symbol Service.Search --language go",
			"rzm agent code-references --symbol Search --path pkg/search/service.go --language go",
		}
	case "code-symbol-context":
		return []string{
			"rzm agent code-symbol-context --symbol Service.Search --language go",
			"rzm agent code-symbol-context --symbol Search --path pkg/search/service.go --language go",
		}
	case "external-references":
		return []string{
			"rzm agent external-references --handle 'external:ecosystem=npm&module=react&symbol=useState&kind=symbol'",
			"rzm agent external-references --ecosystem npm --module react --symbol-prefix use",
		}
	case "ontology-query":
		return []string{
			"rzm agent ontology-query --json '{\"query\":\"query Spec($path: String!) { technicalSpec(path: $path) { path title } }\",\"variables\":{\"path\":\"docs/specs/technical/saved-query-recipes.md\"}}'",
			"rzm agent ontology-query --query '{ technicalSpec(first: 5) { path title status } }'",
		}
	case "query-recipe":
		return []string{
			"rzm agent query-recipe list",
			"rzm agent query-recipe validate",
			"rzm agent query-recipe run --id note-by-path --anchor docs/specs/technical/saved-query-recipes.md",
		}
	case "view":
		return []string{
			"rzm agent view list",
			"rzm agent view validate",
			"rzm agent view run --id generated.type.ProductSpec.table --first 20",
		}
	default:
		return nil
	}
}

func collectAgentSurfaceFlags(cmd *cobra.Command) []agentSurfaceFlag {
	var flags []agentSurfaceFlag
	seen := make(map[string]struct{})
	addFlags := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		fs.VisitAll(func(f *pflag.Flag) {
			if f.Hidden {
				return
			}
			if shouldOmitSurfaceFlag(f.Name) {
				return
			}
			if _, ok := seen[f.Name]; ok {
				return
			}
			seen[f.Name] = struct{}{}
			flags = append(flags, agentSurfaceFlag{
				Name:       f.Name,
				Shorthand:  f.Shorthand,
				Type:       f.Value.Type(),
				Default:    f.DefValue,
				Repeatable: isRepeatableFlag(f),
				Usage:      f.Usage,
			})
		})
	}

	addFlags(cmd.InheritedFlags())
	addFlags(cmd.NonInheritedFlags())
	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Name < flags[j].Name
	})
	return flags
}

func shouldOmitSurfaceFlag(name string) bool {
	switch name {
	case "help", "no-pager":
		return true
	default:
		return false
	}
}

func isRepeatableFlag(f *pflag.Flag) bool {
	if f == nil || f.Value == nil {
		return false
	}
	switch f.Value.Type() {
	case "stringArray", "stringSlice":
		return true
	default:
		return false
	}
}
