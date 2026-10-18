package query

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func (e *executor) resolveLocator(ctx context.Context, ref ontology.NodeRef) map[string]any {
	// Docs: [[ontology-browser-workspace#^spec-0014-us5-ac3]] requires GraphQL
	// locator payloads to expose requires-fix status instead of rendering
	// fragile embedded-node links. [[ontology-graphql-query-contract]]
	// keeps the query field aligned with NodeWorkspace/copy-link semantics.
	locator := ontology.NodeLocator{
		Ref:           ref,
		Kind:          ref.Kind,
		SourceLocator: queryLocatorKey(ref),
		Status:        ontology.NodeLocatorUnresolved,
	}
	if e.loaders != nil && e.loaders.scope != nil {
		result, err := e.loaders.scope.Locators(ctx, []ontology.NodeRef{ref})
		if err == nil {
			if resolved, ok := result[queryLocatorKey(ref)]; ok {
				locator = resolved
			}
		}
	}
	return locatorGraphQLValue(locator)
}

// Public refs retain the projected fingerprint. Internal scheduling refs stay
// stable so enriching an output cannot change keys in the warmed read plan.
func (e *executor) publicSectionRef(ctx context.Context, node *sectionRecord) (ontology.NodeRef, error) {
	ref := e.sectionNodeRef(node)
	if node == nil || node.Projection != nil || !node.catalogRef.IsZero() {
		return ref, nil
	}
	projection, err := e.projectSectionRecord(ctx, node)
	if err != nil {
		return ontology.NodeRef{}, fmt.Errorf("cannot project section ref: %w", err)
	}
	if projection == nil {
		return ontology.NodeRef{}, fmt.Errorf("section ref projection is unavailable")
	}
	return projection.Ref, nil
}

func (e *executor) sectionNodeRef(node *sectionRecord) ontology.NodeRef {
	if node == nil {
		return ontology.NodeRef{}
	}
	if node.Projection != nil {
		return node.Projection.Ref
	}
	if !node.catalogRef.IsZero() {
		return node.catalogRef
	}
	if node.Node == nil {
		return ontology.NodeRef{}
	}
	kind := ontology.NodeKindSection
	if noteType := e.schema.Types[node.TypeName]; noteType != nil && noteType.Role == ontoschema.TypeRoleEmbeddedNode {
		kind = ontology.NodeKindEmbedded
	}
	ref := ontology.NodeRef{
		NotePath: node.Node.NotePath,
		Kind:     kind,
		NodeID:   node.Node.ID,
		TypeName: node.TypeName,
	}
	if blockID := strings.TrimSpace(strings.TrimPrefix(node.Node.BlockID, "^")); blockID != "" {
		ref.Fragment = "^" + blockID
	} else {
		ref.Fragment = queryFragmentFromSectionID(node.Node.NotePath, node.Node.ID)
	}
	return ref
}

func nodeCatalogTitleFromProjection(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	if projection.Ref.NodeID != "" {
		if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
			return span.Title
		}
	}
	return projection.Ref.String()
}

func projectionRecordContent(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return strings.TrimSpace(span.Content)
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return ontology.SectionBody(section)
	}
	return ""
}

func codeNodeRef(node *codeRecord) ontology.NodeRef {
	ref := ontology.NodeRef{NotePath: node.Path, Kind: "CODE_FILE", TypeName: "CodeFile"}
	if node.Anchor != nil {
		ref.Kind = "CODE_SYMBOL"
		ref.TypeName = "CodeSymbol"
		ref.NodeID = node.Anchor.AnchorID
		ref.Fragment = "symbol:" + firstNonEmpty(node.Anchor.FQN, node.Anchor.Symbol, node.Anchor.AnchorID)
	}
	return ref
}

func isCodeNodeKind(kind ontology.NodeKind) bool {
	switch kind {
	case "CODE_FILE", "CODE_SYMBOL", "MODULE":
		return true
	default:
		return false
	}
}

func allowsCodeFileFallback(ref ontology.NodeRef) bool {
	if ref.Kind != "" && ref.Kind != ontology.NodeKindNote && ref.Kind != ontology.NodeKindSection {
		return false
	}
	if strings.TrimSpace(ref.NodeID) != "" || strings.TrimSpace(ref.Structural) != "" {
		return false
	}
	// A catalog miss may still name an indexed code symbol. Do not use the
	// filename extension to decide: rootCode confirms code ownership before it
	// returns a code record.
	return true
}

func locatorGraphQLValue(locator ontology.NodeLocator) map[string]any {
	kind := locator.Kind
	if kind == "" {
		kind = locator.Ref.Kind
	}
	out := map[string]any{
		"ref":           nodeRefGraphQLValue(locator.Ref),
		"kind":          graphQLNodeKind(kind),
		"sourceLocator": locator.SourceLocator,
		"status":        string(locator.Status),
		"diagnostics":   locatorDiagnosticsGraphQLValue(locator.Diagnostics),
		"fixActions":    locatorFixActionsGraphQLValue(locator.FixActions),
		"exists":        false,
		"requiresFix":   locator.Status == ontology.NodeLocatorRequiresFix,
	}
	if locator.LinkTarget != nil {
		out["linkTarget"] = linkTargetGraphQLValue(*locator.LinkTarget)
		out["wikilink"] = locator.LinkTarget.Wikilink
		out["markdown"] = locator.LinkTarget.Markdown
		out["exists"] = locator.LinkTarget.Exists
		out["requiresFix"] = locator.LinkTarget.RequiresFix
	}
	return out
}

func nodeRefGraphQLValue(ref ontology.NodeRef) map[string]any {
	kind := graphQLNodeKind(ref.Kind)
	pathValue := ref.NotePath
	return map[string]any{
		"ref":        queryLocatorKey(ref),
		"notePath":   notePathForNodeKind(kind, ref.NotePath),
		"path":       pathValue,
		"fragment":   emptyNil(ref.Fragment),
		"kind":       kind,
		"nodeId":     emptyNil(ref.NodeID),
		"structural": emptyNil(ref.Structural),
		"typeName":   emptyNil(ref.TypeName),
	}
}

func notePathForNodeKind(kind string, notePath string) any {
	switch kind {
	case "NOTE", "SECTION", "EMBEDDED":
		return notePath
	default:
		return nil
	}
}

func graphQLNodeKind(kind ontology.NodeKind) string {
	switch kind {
	case ontology.NodeKindNote:
		return "NOTE"
	case ontology.NodeKindSection:
		return "SECTION"
	case ontology.NodeKindEmbedded:
		return "EMBEDDED"
	case "CODE_SYMBOL":
		return "CODE_SYMBOL"
	case "MODULE":
		return "MODULE"
	default:
		return "CODE_FILE"
	}
}

func emptyNil(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func linkTargetGraphQLValue(target ontology.NodeLinkTarget) map[string]any {
	return map[string]any{
		"ref":          nodeRefGraphQLValue(target.Ref),
		"markdown":     target.Markdown,
		"wikilink":     target.Wikilink,
		"displayLabel": target.DisplayLabel,
		"exists":       target.Exists,
		"requiresFix":  target.RequiresFix,
		"blockId":      target.BlockID,
	}
}

func locatorDiagnosticsGraphQLValue(diagnostics []ontology.NodeLinkDiagnostic) []any {
	out := make([]any, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, map[string]any{
			"code":     diagnostic.Code,
			"notePath": emptyNil(diagnostic.NotePath),
			"ref":      optionalNodeRefGraphQLValue(diagnostic.Ref),
			"blockId":  emptyNil(diagnostic.BlockID),
			"message":  diagnostic.Message,
		})
	}
	return out
}

func locatorFixActionsGraphQLValue(actions []ontology.NodeLinkFixAction) []any {
	out := make([]any, 0, len(actions))
	for _, action := range actions {
		out = append(out, map[string]any{
			"ref":     nodeRefGraphQLValue(action.Ref),
			"blockId": action.BlockID,
		})
	}
	return out
}

func optionalNodeRefGraphQLValue(ref ontology.NodeRef) any {
	if ref.IsZero() {
		return nil
	}
	return nodeRefGraphQLValue(ref)
}

func queryLocatorKey(ref ontology.NodeRef) string {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	if ref.Kind == ontology.NodeKindNote {
		return ref.NotePath
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if fragment := queryFragmentFromSectionID(ref.NotePath, ref.NodeID); fragment != "" {
			return ref.NotePath + "#" + fragment
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

func queryFragmentFromSectionID(notePath, id string) string {
	id = strings.TrimSpace(id)
	prefix := strings.TrimSpace(notePath) + "#"
	if strings.HasPrefix(id, prefix) {
		return strings.TrimPrefix(id, prefix)
	}
	if idx := strings.LastIndex(id, "#"); idx >= 0 && idx+1 < len(id) {
		return id[idx+1:]
	}
	return ""
}
