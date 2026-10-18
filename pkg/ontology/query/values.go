package query

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/vektah/gqlparser/v2/ast"
)

func responseKey(field *ast.Field) string {
	if strings.TrimSpace(field.Alias) != "" {
		return field.Alias
	}
	return field.Name
}

func fieldValue(field *ontoschema.Field, values []string) any {
	if field.List {
		out := make([]any, 0, len(values))
		for _, value := range values {
			out = append(out, scalarValue(field.TypeName, value))
		}
		return out
	}
	if len(values) == 0 {
		return nil
	}
	return scalarValue(field.TypeName, values[0])
}

func scalarValue(typeName, raw string) any {
	switch typeName {
	case "Int":
		if value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			return value
		}
	case "Float":
		if value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
			return value
		}
	case "Boolean":
		if value, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(raw))); err == nil {
			return value
		}
	}
	return raw
}

func builtinProvenance(name string) map[string]struct{} {
	switch name {
	case "linked":
		return map[string]struct{}{"body_link": {}}
	case "backlinked":
		return map[string]struct{}{"backlink": {}}
	default:
		return map[string]struct{}{"body_link": {}, "backlink": {}}
	}
}

func directionProvenance(direction ontoschema.NeighborDirection) map[string]struct{} {
	switch direction {
	case ontoschema.NeighborDirectionOutbound:
		return map[string]struct{}{"body_link": {}}
	case ontoschema.NeighborDirectionInbound:
		return map[string]struct{}{"backlink": {}}
	default:
		return map[string]struct{}{"body_link": {}, "backlink": {}}
	}
}

func scoredPaths[T interface{ getPath() string }](matches []T) []string {
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.getPath())
	}
	return out
}

func intValue(v any, fallback int) int {
	switch current := v.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return fallback
	}
}

func boolValue(v any) bool {
	current, _ := v.(bool)
	return current
}

func boundedInt(v any, fallback int, maxValue int) int {
	value := intValue(v, fallback)
	if value < 1 {
		return fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func traversalDirectionValue(raw any) noderead.TraversalDirection {
	switch strings.ToUpper(strings.TrimSpace(stringValue(raw))) {
	case "INBOUND":
		return noderead.TraversalDirectionInbound
	case "BOTH":
		return noderead.TraversalDirectionBoth
	default:
		return noderead.TraversalDirectionOutbound
	}
}

func traversalDirectionGraphQLValue(direction noderead.TraversalDirection) string {
	switch direction {
	case noderead.TraversalDirectionInbound:
		return "INBOUND"
	case noderead.TraversalDirectionBoth:
		return "BOTH"
	default:
		return "OUTBOUND"
	}
}

func graphProfileValue(raw any) noderead.GraphProfile {
	switch strings.ToUpper(strings.TrimSpace(stringValue(raw))) {
	case "NOTES_ONLY":
		return noderead.GraphProfileNotesOnly
	case "CODE_AWARE":
		return noderead.GraphProfileCodeAware
	default:
		return noderead.GraphProfileOntologyNative
	}
}

func normalizeNotePath(vaultPaths paths.VaultPaths, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if rel, _, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, raw); err == nil {
		return rel.String(), nil
	}
	return string(paths.NormalizeNotePath(raw)), nil
}

func stringListValue(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s := strings.TrimSpace(stringValue(value)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e *executor) resolvedNodeRef(node *resolvedNode) ontology.NodeRef {
	switch {
	case node == nil:
		return ontology.NodeRef{}
	case node.Note != nil:
		return ontology.NodeRef{NotePath: node.Note.Path, Kind: ontology.NodeKindNote, TypeName: node.Note.TypeName}
	case node.Section != nil:
		kind := ontology.NodeKindSection
		if noteType := e.schema.Types[node.Section.TypeName]; noteType != nil && noteType.Role == ontoschema.TypeRoleEmbeddedNode {
			kind = ontology.NodeKindEmbedded
		}
		return ontology.NodeRef{NotePath: node.Section.Node.NotePath, Kind: kind, NodeID: node.Section.Node.ID, TypeName: node.Section.TypeName, Fragment: queryFragmentFromSectionID(node.Section.Node.NotePath, node.Section.Node.ID)}
	case node.Code != nil:
		return codeNodeRef(node.Code)
	default:
		return ontology.NodeRef{}
	}
}

func nodeRefFromString(raw string) ontology.NodeRef {
	raw = strings.TrimSpace(raw)
	ref := ontology.NodeRef{Kind: ontology.NodeKindNote}
	if strings.HasPrefix(raw, "code:") {
		ref.Kind = "CODE_FILE"
		raw = strings.TrimPrefix(raw, "code:")
	}
	if idx := strings.Index(raw, "#"); idx >= 0 {
		ref.NotePath = strings.TrimSpace(raw[:idx])
		ref.Fragment = strings.TrimPrefix(strings.TrimSpace(raw[idx+1:]), "#")
		if strings.HasPrefix(ref.Fragment, "struct:") {
			ref.Structural = strings.TrimSpace(strings.TrimPrefix(ref.Fragment, "struct:"))
			ref.Fragment = ""
			ref.Kind = ontology.NodeKindSection
		} else if strings.HasPrefix(ref.Fragment, "symbol:") {
			ref.Kind = "CODE_SYMBOL"
			ref.TypeName = "CodeSymbol"
		} else if strings.HasPrefix(ref.Fragment, "^") || strings.HasPrefix(ref.Fragment, "section-") || strings.HasPrefix(ref.Fragment, "item-") {
			ref.Kind = ontology.NodeKindEmbedded
		} else if ref.Kind == ontology.NodeKindNote {
			// Resolve a regular fragment through the catalog/projection first. If
			// it is not a note section, rootNode may still confirm an indexed code
			// symbol. A filename extension cannot choose between them.
			ref.Kind = ontology.NodeKindSection
		}
		return ref
	}
	ref.NotePath = raw
	return ref
}

func explicitNodePathRefFromString(raw string) (ontology.NodeRef, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "[[") || strings.Contains(raw, "](") || strings.Contains(raw, "://") {
		return ontology.NodeRef{}, false
	}
	ref := nodeRefFromString(raw)
	notePath := strings.TrimSpace(ref.NotePath)
	if notePath == "" {
		return ontology.NodeRef{}, false
	}
	if strings.HasPrefix(raw, "code:") || strings.Contains(notePath, "/") || filepath.IsAbs(notePath) {
		return ref, true
	}
	return ontology.NodeRef{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func queryRootTypeName(schema *ontoschema.Schema, root string) string {
	for typeName, noteType := range schema.Types {
		if noteType == nil || (noteType.Role != ontoschema.TypeRoleNote && noteType.Role != ontoschema.TypeRoleEmbeddedNode) {
			continue
		}
		if queryRootFieldName(typeName) == root {
			return typeName
		}
	}
	return ""
}
