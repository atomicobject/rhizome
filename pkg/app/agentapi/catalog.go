package agentapi

import (
	"context"
	"slices"
	"sort"
	"strings"

	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// ToolSurface identifies a consumer-facing registry that includes a tool.
// A descriptor may belong to more than one surface.
type ToolSurface uint8

const (
	SurfaceAgentAPI ToolSurface = 1 << iota
	SurfaceAgentCLI
	SurfaceCapabilities
	SurfaceCodeMode
)

// ToolAvailability controls whether a tool is advertised for a runtime mode.
type ToolAvailability uint8

const (
	AvailabilityAlways ToolAvailability = iota
	AvailabilityReadWrite
)

// ToolMutation controls when a tool is advertised as mutating.
type ToolMutation uint8

const (
	MutationNever ToolMutation = iota
	MutationAlways
	MutationReadWrite
)

// ToolKind distinguishes shared MCP-shaped handlers from local-only agent CLI
// commands that still need truthful surface metadata.
type ToolKind uint8

const (
	ToolKindShared ToolKind = iota
	ToolKindLocal
)

// AgentCLICommandCategory is the stable grouping rendered by `rzm agent surface`.
type AgentCLICommandCategory string

const (
	AgentCLICategorySession      AgentCLICommandCategory = "session"
	AgentCLICategoryValidation   AgentCLICommandCategory = "validation"
	AgentCLICategoryOntology     AgentCLICommandCategory = "ontology"
	AgentCLICategorySafeMutation AgentCLICommandCategory = "safe_mutation"
	AgentCLICategoryDiscovery    AgentCLICommandCategory = "discovery"
	AgentCLICategoryCode         AgentCLICommandCategory = "code"
	AgentCLICategoryContext      AgentCLICommandCategory = "context"
	AgentCLICategoryAnalysis     AgentCLICommandCategory = "analysis"
	AgentCLICategoryIdentity     AgentCLICommandCategory = "identity"
	AgentCLICategoryNavigation   AgentCLICommandCategory = "navigation"
)

// ToolHandlerFactory builds the MCP-shaped handler shared by in-process agent
// callers and any future server adapter.
type ToolHandlerFactory func(Config) func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error)

// ToolDescriptor is the authoritative identity and routing metadata for one
// shared handler or local-only agent command.
type ToolDescriptor struct {
	Name                 string
	AgentCommand         string
	Kind                 ToolKind
	Surfaces             ToolSurface
	AgentCapabilityOrder int
	AgentCLIOrder        int
	AgentCLICategory     AgentCLICommandCategory
	Availability         ToolAvailability
	Mutation             ToolMutation
	Handler              ToolHandlerFactory
	CodeLocal            CodeLocalHandler
	// CodeRuntimeRead reports whether one call of a local override is a read
	// the vault runtime may serve instead (SPEC-0115). Nil means never.
	CodeRuntimeRead func(map[string]any) bool
	CodeContract    *CodeOperationContract
	// CodeCLILeaves lists the concrete command paths represented by this code
	// operation. It is coverage metadata; callers still select Name.
	CodeCLILeaves []string
	CodeAccess    CodeAccess
}

var toolCatalog = []ToolDescriptor{
	agentCLI(agentCapability(descriptor("check_paths", "check-paths", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CheckPathsTool), 28), 32, AgentCLICategoryDiscovery),
	agentCLI(agentCapability(descriptor("evaluate_batch", "evaluate-batch", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.EvaluateBatchTool), 29), 33, AgentCLICategoryAnalysis),
	descriptor("capabilities", "", SurfaceAgentAPI|SurfaceCapabilities, MutationNever, capabilitiesTool),
	agentCLI(agentCapability(descriptor("files", "files", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.FilesTool), 7), 9, AgentCLICategoryDiscovery),
	agentCLI(agentCapability(descriptor("list_tags", "list-tags", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ListTagsTool), 11), 14, AgentCLICategoryDiscovery),
	agentCLI(agentCapability(descriptor("list_properties", "list-properties", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ListPropertiesTool), 10), 15, AgentCLICategoryDiscovery),
	agentCLI(agentCapability(descriptor("community_list", "community-list", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CommunityListTool), 5), 16, AgentCLICategoryDiscovery),
	agentCLI(agentCapability(descriptor("file_context", "file-context", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.FileContextTool), 6), 10, AgentCLICategoryContext),
	agentCLI(agentCapability(descriptor("vault_context", "vault-context", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.VaultContextTool), 24), 11, AgentCLICategoryContext),
	agentCLI(agentCapability(descriptor("semantic_query", "semantic-query", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.SemanticQueryTool), 21), 5, AgentCLICategoryDiscovery),
	agentCLI(agentCapability(descriptor("code_symbol", "code-symbol", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CodeSymbolTool), 3), 6, AgentCLICategoryCode),
	agentCLI(agentCapability(descriptor("code_references", "code-references", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CodeReferencesTool), 2), 7, AgentCLICategoryCode),
	agentCLI(agentCapability(descriptor("code_symbol_context", "code-symbol-context", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CodeSymbolContextTool), 4), 8, AgentCLICategoryCode),
	agentCLI(agentCapability(descriptor("external_references", "external-references", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ExternalReferencesTool), 26), 29, AgentCLICategoryCode),
	agentCLI(agentCapability(descriptor("find_connections", "find-connections", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.FindConnectionsTool), 8), 17, AgentCLICategoryDiscovery),
	agentCLI(agentCapability(descriptor("report", "report", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ReportTool), 20), 12, AgentCLICategoryAnalysis),
	agentCLI(agentCapability(descriptor("node_link", "node-link", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationReadWrite, mcpapi.NodeLinkTool), 12), 3, AgentCLICategoryOntology),
	codeLocal(agentCLI(agentCapability(descriptor("note_rename_heading", "note-rename-heading", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationAlways, mcpapi.RenameHeadingTool), 13), 4, AgentCLICategorySafeMutation), CodeLocalService.RenameHeading),
	agentCLI(agentCapability(descriptor("vault_health", "vault-health", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.VaultHealthTool), 25), 13, AgentCLICategoryAnalysis),
	codeLocal(agentCLI(agentCapability(descriptor("ontology_query_schema", "ontology-query-schema", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.OntologyQuerySchemaTool), 16), 21, AgentCLICategoryOntology), CodeLocalService.OntologyQuerySchema),
	runtimeRead(codeLocal(agentCLI(agentCapability(descriptor("ontology_query", "ontology-query", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.OntologyQueryTool), 15), 22, AgentCLICategoryOntology), CodeLocalService.OntologyQuery), func(map[string]any) bool { return true }),
	runtimeRead(codeLocal(agentCLI(agentCapability(descriptor("query_recipe", "query-recipe", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.QueryRecipeTool), 18), 23, AgentCLICategoryOntology), CodeLocalService.QueryRecipe), runtimeReadActions("run")),
	agentCLI(agentCapability(descriptor("graph_path", "graph-path", SurfaceAgentAPI|SurfaceAgentCLI, MutationNever, mcpapi.GraphPathTool), 9), 27, AgentCLICategoryNavigation),
	agentCLI(agentCapability(descriptor("evaluate", "evaluate", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.EvaluateTool), 27), 31, AgentCLICategoryAnalysis),
	codeLocal(localDescriptor("code_rationale", "code-rationale", 28, AgentCLICategoryAnalysis), CodeLocalService.CodeRationale),
	codeLocal(localDescriptor("ontology_authoring_guide", "ontology-authoring-guide", 19, AgentCLICategoryOntology), CodeLocalService.OntologyAuthoringGuide),
	codeLocal(localDescriptor("ontology_inspect", "ontology-inspect", 20, AgentCLICategoryOntology), CodeLocalService.OntologyInspect),
	codeLocal(localDescriptor("ontology_reference", "ontology-reference", 18, AgentCLICategoryOntology), CodeLocalService.OntologyReference),
	runtimeRead(codeLocal(localMutatingDescriptor("view", "view", 24, AgentCLICategoryOntology), CodeLocalService.View), runtimeReadActions("list", "show", "validate", "run")),
	codeLocal(localDescriptor("start", "start", 1, AgentCLICategorySession), CodeLocalService.Start),
	codeLocal(localMutatingDescriptor("validate", "validate", 2, AgentCLICategoryValidation), CodeLocalService.Validate),
	codeLocal(localMutatingDescriptor("current_user", "current-user", 25, AgentCLICategoryIdentity), CodeLocalService.CurrentUser),
	codeLocal(localDescriptor("next_id", "next-id", 26, AgentCLICategoryOntology), CodeLocalService.NextID),
	localDescriptor("code", "code", 30, AgentCLICategoryDiscovery),
	{Name: "surface", Kind: ToolKindLocal, Surfaces: SurfaceCodeMode, Availability: AvailabilityAlways, Mutation: MutationNever, CodeLocal: CodeLocalService.Surface},
	{
		Name: "note_move", AgentCommand: "note move", Kind: ToolKindLocal,
		Surfaces: SurfaceCodeMode, Availability: AvailabilityReadWrite, Mutation: MutationReadWrite,
		CodeLocal: CodeLocalService.NoteMove,
	},
}

func init() {
	initializeCodeContracts()
	for i := range toolCatalog {
		if toolCatalog[i].CodeContract != nil {
			toolCatalog[i].Surfaces |= SurfaceCodeMode
			switch toolCatalog[i].Name {
			case "evaluate", "evaluate_batch", "check_paths":
				toolCatalog[i].CodeAccess = CodeIndependent
			case "code_symbol", "code_references", "external_references", "graph_path", "code_rationale":
				toolCatalog[i].CodeAccess = CodeVaultRead
			}
		}
	}
}

type catalogInventory struct{}

func (catalogInventory) AvailableToolNames(readWrite bool) []string {
	return CapabilityToolNames(readWrite)
}

func (catalogInventory) MutatingToolNames(readWrite bool) []string {
	return CapabilityMutatingToolNames(readWrite)
}

func capabilitiesTool(config Config) func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	config.ToolInventory = catalogInventory{}
	return mcpapi.CapabilitiesTool(config)
}

func descriptor(name, command string, surfaces ToolSurface, mutation ToolMutation, handler ToolHandlerFactory) ToolDescriptor {
	return ToolDescriptor{
		Name:         name,
		AgentCommand: command,
		Surfaces:     surfaces,
		Availability: AvailabilityAlways,
		Mutation:     mutation,
		Handler: func(cfg Config) func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpapi.ObserveTool(name, cfg, handler(cfg))
		},
	}
}

func agentCapability(descriptor ToolDescriptor, order int) ToolDescriptor {
	descriptor.AgentCapabilityOrder = order
	return descriptor
}

func agentCLI(descriptor ToolDescriptor, order int, category AgentCLICommandCategory) ToolDescriptor {
	descriptor.AgentCLIOrder = order
	descriptor.AgentCLICategory = category
	return descriptor
}

func codeLocal(descriptor ToolDescriptor, handler CodeLocalHandler) ToolDescriptor {
	descriptor.CodeLocal = handler
	return descriptor
}

func runtimeRead(descriptor ToolDescriptor, read func(map[string]any) bool) ToolDescriptor {
	descriptor.CodeRuntimeRead = read
	return descriptor
}

// runtimeReadActions admits the listed actions over the configured catalog. A
// caller-supplied path resolves against the host's working directory, so it
// stays local.
func runtimeReadActions(actions ...string) func(map[string]any) bool {
	return func(input map[string]any) bool {
		if path, _ := input["path"].(string); strings.TrimSpace(path) != "" {
			return false
		}
		action, _ := input["action"].(string)
		if action == "" {
			action, _ = input["op"].(string)
		}
		return slices.Contains(actions, strings.TrimSpace(action))
	}
}

func localDescriptor(name, command string, cliOrder int, category AgentCLICommandCategory) ToolDescriptor {
	return agentCLI(ToolDescriptor{
		Name:         name,
		AgentCommand: command,
		Kind:         ToolKindLocal,
		Surfaces:     SurfaceAgentCLI,
		Availability: AvailabilityAlways,
		Mutation:     MutationNever,
	}, cliOrder, category)
}

func localMutatingDescriptor(name, command string, cliOrder int, category AgentCLICommandCategory) ToolDescriptor {
	descriptor := localDescriptor(name, command, cliOrder, category)
	descriptor.Mutation = MutationReadWrite
	return descriptor
}

// AgentCapabilityToolNames returns the stable standalone-agent capability list.
// Local CLI commands remain discoverable through AgentCLICommandDescriptors,
// but are excluded here because capability names represent CallJSON tools.
func AgentCapabilityToolNames() []string {
	descriptors := agentCapabilityDescriptors()
	names := make([]string, len(descriptors))
	for i, descriptor := range descriptors {
		names[i] = descriptor.Name
	}
	return names
}

// AgentCapabilityMutatingToolNames returns the mutating subset of the
// standalone capability list for the requested authorization mode.
func AgentCapabilityMutatingToolNames(readWrite bool) []string {
	names := make([]string, 0)
	for _, descriptor := range agentCapabilityDescriptors() {
		if descriptor.Mutation == MutationNever || (descriptor.Mutation == MutationReadWrite && !readWrite) {
			continue
		}
		names = append(names, descriptor.Name)
	}
	return names
}

func agentCapabilityDescriptors() []ToolDescriptor {
	descriptors := make([]ToolDescriptor, 0)
	for _, descriptor := range toolCatalog {
		if descriptor.AgentCapabilityOrder > 0 {
			descriptors = append(descriptors, descriptor)
		}
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].AgentCapabilityOrder < descriptors[j].AgentCapabilityOrder
	})
	return descriptors
}

// ToolCatalog returns a copy so callers cannot mutate the authoritative order
// or metadata.
func ToolCatalog() []ToolDescriptor {
	descriptors := make([]ToolDescriptor, len(toolCatalog))
	for i, descriptor := range toolCatalog {
		descriptors[i] = cloneToolDescriptor(descriptor)
	}
	return descriptors
}

func cloneToolDescriptor(descriptor ToolDescriptor) ToolDescriptor {
	if descriptor.CodeContract != nil {
		contract := *descriptor.CodeContract
		contract.Effects = append([]string(nil), contract.Effects...)
		contract.InputSchema = append([]byte(nil), contract.InputSchema...)
		contract.OutputSchema = append([]byte(nil), contract.OutputSchema...)
		contract.Example = cloneContractMap(contract.Example)
		contract.Interpretation = append([]string(nil), contract.Interpretation...)
		descriptor.CodeContract = &contract
	}
	descriptor.CodeCLILeaves = append([]string(nil), descriptor.CodeCLILeaves...)
	return descriptor
}

// AgentCLICommandDescriptors returns the advertised agent command descriptors
// in their stable display order.
func AgentCLICommandDescriptors() []ToolDescriptor {
	descriptors := make([]ToolDescriptor, 0)
	for _, descriptor := range toolCatalog {
		if descriptor.Surfaces&SurfaceAgentCLI != 0 {
			descriptors = append(descriptors, descriptor)
		}
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].AgentCLIOrder < descriptors[j].AgentCLIOrder
	})
	return descriptors
}

// ToolForAgentCommand resolves a shared or local-only cataloged agent command.
func ToolForAgentCommand(command string) (ToolDescriptor, bool) {
	for _, descriptor := range toolCatalog {
		if descriptor.Surfaces&SurfaceAgentCLI != 0 && descriptor.AgentCommand == command {
			return descriptor, true
		}
	}
	return ToolDescriptor{}, false
}

// CapabilityToolNames returns the tools advertised as available for a runtime.
func CapabilityToolNames(readWrite bool) []string {
	return catalogNamesForSurface(SurfaceCapabilities, readWrite)
}

// CapabilityMutatingToolNames returns the advertised mutating subset.
func CapabilityMutatingToolNames(readWrite bool) []string {
	names := make([]string, 0)
	for _, mutation := range []ToolMutation{MutationAlways, MutationReadWrite} {
		if mutation == MutationReadWrite && !readWrite {
			continue
		}
		for _, descriptor := range toolCatalog {
			if descriptor.Surfaces&SurfaceCapabilities == 0 || !descriptorAvailable(descriptor, readWrite) || descriptor.Mutation != mutation {
				continue
			}
			names = append(names, descriptor.Name)
		}
	}
	return names
}

func catalogNamesForSurface(surface ToolSurface, readWrite bool) []string {
	names := make([]string, 0)
	for _, descriptor := range toolCatalog {
		if descriptor.Surfaces&surface != 0 && descriptorAvailable(descriptor, readWrite) {
			names = append(names, descriptor.Name)
		}
	}
	return names
}

func descriptorAvailable(descriptor ToolDescriptor, readWrite bool) bool {
	return descriptor.Availability != AvailabilityReadWrite || readWrite
}

func descriptorForName(name string, surface ToolSurface) (ToolDescriptor, bool) {
	for _, descriptor := range toolCatalog {
		if descriptor.Name == name && descriptor.Surfaces&surface != 0 {
			return descriptor, true
		}
	}
	return ToolDescriptor{}, false
}
