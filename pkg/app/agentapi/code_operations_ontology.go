package agentapi

import (
	"fmt"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"reflect"
)

func codeOntologyQuerySchemaDescriptor() ToolDescriptor {
	d := codeLocal(agentCLI(agentCapability(descriptor("ontology_query_schema", "ontology-query-schema", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.OntologyQuerySchemaTool), 16), 21, AgentCLICategoryOntology), CodeLocalService.OntologyQuerySchema)
	c := contract("Return the full executable schema or bounded type fragments with related output types.", input(map[string]codeSchema{"type": str(), "types": codeSchema{"type": "array", "items": str(), "maxItems": ontologyquery.SchemaDiscoveryMaxTypes}, "depth": codeSchema{"type": "integer", "minimum": 0, "maximum": ontologyquery.SchemaDiscoveryMaxDepth}, "maxBytes": codeSchema{"type": "integer", "minimum": 0, "maximum": ontologyquery.SchemaDiscoveryMaxBytes}}), schemaForType(reflect.TypeOf(ontologyquery.SchemaDiscovery{}), map[reflect.Type]bool{}), map[string]any{"type": "Project", "depth": 1}, "reads ontology configuration")
	return completeCodeDescriptor(d, c, []string{"agent ontology-query-schema"}, map[string]codeInputFieldMetadata{
		"type":     {Description: "One public type to discover; mutually exclusive with types. Omit both for the full executable schema."},
		"types":    {Description: "Public types to discover together, at most 32; mutually exclusive with type."},
		"depth":    {Description: "Related output-type hops to include, 0..2; requires type or types. Inputs, enums, and scalars are always included.", Default: 0, HasDefault: true},
		"maxBytes": {Description: "Maximum bytes of selected SDL, at most 131072; zero uses 32768. Requires type or types. Whole definitions are retained; omitted related types remain in referencedTypes.", Default: 0, HasDefault: true},
	}, "Omitting type and types preserves full discovery. Selected discovery includes root arguments and input dependencies; depth includes related output types within maxBytes and the 32-type ceiling. Inspect includedTypes, referencedTypes, limits, truncated, and limitReasons before assuming a related type was returned.")
}

func codeOntologyQueryDescriptor() ToolDescriptor {
	d := runtimeRead(codeLocal(agentCLI(agentCapability(descriptor("ontology_query", "ontology-query", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.OntologyQueryTool), 15), 22, AgentCLICategoryOntology), CodeLocalService.OntologyQuery), func(map[string]any) bool { return true })
	c := contract("Execute a read-only ontology GraphQL query. Typed root coverage is in extensions.typedRoots by response alias.", input(map[string]codeSchema{"query": str(), "variables": unknownObject()}, "query"), output(map[string]codeSchema{"data": unknownObject(), "errors": objectsSchema(), "extensions": unknownObject()}), map[string]any{"query": "query { types { name } }"}, "reads the ontology projection", "query data is intentionally dynamic")
	return completeCodeDescriptor(d, c, []string{"agent ontology-query"}, map[string]codeInputFieldMetadata{
		"query":     {Description: "Read-only GraphQL query text."},
		"variables": {Description: "Optional JSON object of GraphQL variables."},
	}, "The GraphQL payload may contain execution errors and returns exitCode 1 when it does; query data remains dynamic. Generated typed roots add independent coverage under extensions.typedRoots[responseAlias]; use offset for continuation where nextOffset is present. Non-fatal warnings, such as a relation value left out of its list, are in extensions.warnings.")
}

func codeOntologyReferenceDescriptor() ToolDescriptor {
	d := codeLocal(localDescriptor("ontology_reference", "ontology-reference", 18, AgentCLICategoryOntology), CodeLocalService.OntologyReference)
	d.validateCodeInput = func(value map[string]any) error {
		if value["compact"] == true {
			return requireInputString(value, "type")
		}
		return nil
	}

	c := contract("Render ontology reference JSON; compact authoring contracts require a type.", input(map[string]codeSchema{"type": str(), "compact": boolean()}), object([]string{"types"}, map[string]codeSchema{"types": codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(schemadoc.TypeDoc{}), map[reflect.Type]bool{})}, "format": str(), "schemaHash": str(), "expand": schemaForType(reflect.TypeOf([]schemadoc.Expansion{}), map[reflect.Type]bool{})}), map[string]any{}, "reads ontology configuration")
	return completeCodeDescriptor(d, c, []string{"agent ontology-reference"}, map[string]codeInputFieldMetadata{
		"type":    {Description: "Type to document. Required when compact is true."},
		"compact": {Description: "Render the compact authoring contract; it requires type.", Default: false, HasDefault: true},
	}, "compact=true is an authoring contract for one type, so type is required in that mode.")
}

func codeOntologyAuthoringGuideDescriptor() ToolDescriptor {
	d := codeLocal(localDescriptor("ontology_authoring_guide", "ontology-authoring-guide", 19, AgentCLICategoryOntology), CodeLocalService.OntologyAuthoringGuide)
	c := contract("Render ontology note-authoring guidance.", input(map[string]codeSchema{"type": str(), "types": stringsSchema()}), str(), map[string]any{}, "reads ontology configuration")
	return completeCodeDescriptor(d, c, []string{"agent ontology-authoring-guide"}, nil, "Supply type or types to focus the generated authoring guidance.")
}

func codeOntologyInspectDescriptor() ToolDescriptor {
	d := codeLocal(localDescriptor("ontology_inspect", "ontology-inspect", 20, AgentCLICategoryOntology), CodeLocalService.OntologyInspect)

	c := contract("Inspect ontology inputs and live projection.", input(map[string]codeSchema{"inputs": stringsSchema()}, "inputs"), schemaForType(reflect.TypeOf(ontology.InspectResult{}), map[reflect.Type]bool{}), map[string]any{"inputs": []string{"Spec"}}, "reads ontology configuration and projection")
	return completeCodeDescriptor(d, c, []string{"agent ontology-inspect"}, nil, "inputs is required and selects ontology inputs to inspect against the live projection.")
}

func codeNodeLinkDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("node_link", "node-link", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationReadWrite, mcpapi.NodeLinkTool), 12), 3, AgentCLICategoryOntology)
	d.validateCodeInput = func(value map[string]any) error {
		if _, refs := value["refs"]; !refs {
			if _, targets := value["targets"]; !targets {
				return fmt.Errorf("input.refs or input.targets is required")
			}
		}
		return nil
	}

	c := contract("Resolve or plan durable node link targets.", input(map[string]codeSchema{"refs": codeSchema{"type": "array", "items": codeSchema{"oneOf": []any{str(), unknownObject()}}}, "targets": stringsSchema(), "ensure": enumeration("never", "plan", "apply")}), object([]string{"ensure", "locators"}, map[string]codeSchema{
		"ensure": str(), "locators": codeSchema{"type": "object", "additionalProperties": schemaForType(reflect.TypeOf(ontology.NodeLocator{}), map[reflect.Type]bool{})},
	}), map[string]any{"targets": []string{"docs/example.md#Heading"}, "ensure": "plan"}, "reads ontology and notes", "agent CLI rejects apply")
	return completeCodeDescriptor(d, c, []string{"agent node-link"}, map[string]codeInputFieldMetadata{
		"refs":    {Description: "Inline node references to resolve. Supply refs or targets."},
		"targets": {Description: "Explicit node targets to resolve. Supply refs or targets."},
		"ensure":  {Description: "Choose never or plan when resolving targets. apply is rejected in code mode."},
	}, "refs or targets is required. Code mode supports never and plan; ensure=apply is rejected even on a read-write connection.")
}

func codeNoteRenameHeadingDescriptor() ToolDescriptor {
	d := codeLocal(agentCLI(agentCapability(descriptor("note_rename_heading", "note-rename-heading", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationAlways, mcpapi.RenameHeadingTool), 13), 4, AgentCLICategorySafeMutation), CodeLocalService.RenameHeading)

	c := contract("Plan a heading rename and inbound link rewrites.", input(map[string]codeSchema{"path": str(), "oldHeading": str(), "newHeading": str(), "apply": boolean()}, "path", "oldHeading", "newHeading"), schemaForType(reflect.TypeOf(actions.RenameHeadingResult{}), map[reflect.Type]bool{}), map[string]any{"path": "docs/example.md", "oldHeading": "Old", "newHeading": "New"}, "reads target and inbound links", "agent CLI rejects apply")
	return completeCodeDescriptor(d, c, []string{"agent note-rename-heading"}, map[string]codeInputFieldMetadata{
		"apply": {Description: "Request an applied rename. Code mode remains plan-only and rejects true.", Default: false, HasDefault: true},
	}, "This operation only plans a rename in code mode; apply=true returns a failed outcome.")
}

func codeNextIdDescriptor() ToolDescriptor {
	d := codeLocal(localDescriptor("next_id", "next-id", 26, AgentCLICategoryOntology), CodeLocalService.NextID)

	c := contract("Allocate identifiers for a typed note family.", input(map[string]codeSchema{"type": str(), "count": integer(), "paths": stringsSchema()}, "type"), schemaForType(reflect.TypeOf(idalloc.Result{}), map[reflect.Type]bool{}), map[string]any{"type": "Spec", "count": 1}, "reads ontology and identifier claims", "may refresh ontology metadata; returned identifiers are not reserved")
	return completeCodeDescriptor(d, c, []string{"agent next-id"}, map[string]codeInputFieldMetadata{
		"type":  {Description: "Typed note family whose identifiers are allocated."},
		"count": {Description: "Number of identifiers to return; the runtime caps it at the allocator batch maximum.", Default: 1, HasDefault: true},
	}, "Returned identifiers are not reservations; allocation may refresh metadata while reading identifier claims.")
}
