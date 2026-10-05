package presentation

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

const ontologyNodeContextBudget = 2200

func renderOntologyNodeContext(ctx context.Context, p *DefaultPacker, scope *noderead.Scope, r search.RankedResult, budget int) string {
	if p == nil || scope == nil || strings.TrimSpace(r.NodeRefJSON) == "" {
		return ""
	}
	if budget <= 0 || budget > ontologyNodeContextBudget {
		budget = ontologyNodeContextBudget
	}
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(r.NodeRefJSON), &ref); err != nil || ref.IsZero() {
		return ""
	}
	projection, err := scope.Projection(ctx, ref)
	if err != nil || projection == nil || strings.TrimSpace(projection.ResolvedType) == "" {
		return ""
	}
	lines := []string{"Ontology node context:"}
	lines = append(lines, ontologyNodeSummaryLines(projection)...)
	if parent := ontologyParentLine(ctx, scope, projection); parent != "" {
		lines = append(lines, parent)
	}

	if matchedBody := ontologyMatchedNodeBodyMarkdown(projection, r.ChunkIndex); matchedBody != "" {
		lines = append(lines, "", "Matched body:", contextpack.TrimToBudget(matchedBody, minPositive(900, budget/2)))
	}

	if contextLines := ontologyContextIncludeLines(ctx, p, scope, projection); len(contextLines) > 0 {
		lines = append(lines, "", "Schema context:", strings.Join(contextLines, "\n"))
	}
	return strings.TrimSpace(contextpack.TrimToBudget(strings.Join(lines, "\n"), budget))
}

func ontologyNodeSummaryLines(projection *ontology.NodeProjection) []string {
	title := ontologyProjectionTitle(projection)
	lines := []string{
		fmt.Sprintf("- type: %s", ontologyDisplayType(projection.ResolvedType)),
		fmt.Sprintf("- title: %s", title),
		fmt.Sprintf("- ref: %s", projection.Ref.String()),
	}
	fieldLines := ontologyScalarFieldLines(projection)
	if len(fieldLines) > 0 {
		lines = append(lines, "- fields: "+strings.Join(fieldLines, "; "))
	}
	return lines
}

func ontologyParentLine(ctx context.Context, scope *noderead.Scope, projection *ontology.NodeProjection) string {
	parentID := strings.TrimSpace(projection.Ref.ParentID)
	if parentID == "" {
		return ""
	}
	parentRef := ontology.NodeRef{
		NotePath: projection.Ref.NotePath,
		NodeID:   parentID,
		Kind:     ontology.NodeKindSection,
	}
	parent, err := scope.Projection(ctx, parentRef)
	if err != nil || parent == nil || strings.TrimSpace(parent.ResolvedType) == "" {
		return "- parent: " + parentID
	}
	return fmt.Sprintf("- parent: %s %s", parent.ResolvedType, ontologyProjectionTitle(parent))
}

func ontologyMatchedNodeBodyMarkdown(projection *ontology.NodeProjection, chunkIndex int) string {
	parts := semantic.OntologyNodeBodyParts(projection, 0)
	if len(parts) == 0 {
		return ""
	}
	partIndex := chunkIndex
	if partIndex < 0 || partIndex >= len(parts) {
		partIndex = 0
	}
	return strings.TrimSpace(parts[partIndex])
}

func ontologyDisplayType(typeName string) string {
	if ontology.IsFallbackNoteTypeName(typeName) {
		return "untyped note"
	}
	return strings.TrimSpace(typeName)
}

func ontologyContextIncludeLines(ctx context.Context, p *DefaultPacker, scope *noderead.Scope, projection *ontology.NodeProjection) []string {
	if projection == nil || projection.Type == nil {
		return nil
	}
	var out []string
	for _, field := range projection.Type.Fields {
		if field == nil || !field.ContextInclude {
			continue
		}
		values := ontologyContextIncludeValues(ctx, p, scope, projection, field)
		values = uniqueStrings(values)
		if len(values) == 0 {
			continue
		}
		labels := make([]string, 0, len(values))
		for _, value := range values {
			labels = append(labels, ontologyContextTargetLabel(p, value))
		}
		out = append(out, fmt.Sprintf("- %s: %s", field.Name, strings.Join(labels, ", ")))
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func ontologyContextIncludeValues(ctx context.Context, p *DefaultPacker, scope *noderead.Scope, projection *ontology.NodeProjection, field *ontology.Field) []string {
	if projection == nil || field == nil {
		return nil
	}
	var values []string
	if binding, ok := projection.Fields[field.Name]; ok {
		values = append(values, binding.Values...)
		for _, span := range binding.InlineSpans {
			values = append(values, span.Value)
		}
		for _, ref := range binding.SectionNodes {
			values = append(values, ref.String())
		}
	}
	if field.Kind != ontology.FieldKindNeighbor {
		return values
	}
	if scope == nil || strings.TrimSpace(projection.Ref.NotePath) == "" {
		return values
	}
	var schema *ontology.Schema
	if p != nil {
		schema = p.OntologySchema
	}
	targetTypes, targetInterfaces := noderead.TargetFiltersForType(schema, field.TypeName)
	result, err := scope.Neighborhood(ctx, noderead.NeighborhoodRequest{
		Sources:          []ontology.NodeRef{projection.Ref},
		Direction:        noderead.TraversalDirectionBoth,
		RelationNames:    []string{field.Name},
		IncludeAmbient:   true,
		TargetTypes:      targetTypes,
		TargetInterfaces: targetInterfaces,
		FirstPerSource:   8,
	})
	if err != nil {
		return values
	}
	for _, edge := range result.BySource[projection.Ref.String()].Edges {
		values = append(values, edge.Target.NotePath)
	}
	return values
}

// ontologyContextTargetLabel renders an authored context target without
// inferring generic note or code ownership. Explicit Markdown wikilinks retain
// their historic extensionless .md compatibility; all other targets preserve
// their authored path and extension.
func ontologyContextTargetLabel(p *DefaultPacker, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	markdownWikilink := strings.HasPrefix(value, "[[") && strings.HasSuffix(value, "]]")
	path := strings.TrimPrefix(value, "[[")
	path = strings.TrimSuffix(path, "]]")
	path = strings.TrimSpace(strings.Split(path, "|")[0])
	if markdownWikilink && filepath.Ext(path) == "" {
		path += ".md"
	}
	if p != nil && p.NoteReader != nil {
		if title, ok := p.NoteReader.Title(path); ok && strings.TrimSpace(title) != "" {
			return fmt.Sprintf("%s (%s)", title, path)
		}
	}
	return fmt.Sprintf("%s (%s)", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), path)
}

func ontologyScalarFieldLines(projection *ontology.NodeProjection) []string {
	if projection == nil || projection.Type == nil {
		return nil
	}
	var out []string
	for _, field := range projection.Type.Fields {
		if field == nil {
			continue
		}
		if field.Kind == ontology.FieldKindSection || field.Kind == ontology.FieldKindNeighbor {
			continue
		}
		binding, ok := projection.Fields[field.Name]
		if !ok {
			continue
		}
		values := append([]string(nil), binding.Values...)
		for _, span := range binding.InlineSpans {
			values = append(values, span.Value)
		}
		values = uniqueStrings(values)
		if len(values) == 0 {
			continue
		}
		if len(values) > 3 {
			values = values[:3]
		}
		out = append(out, fmt.Sprintf("%s=%s", field.Name, strings.Join(values, ", ")))
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func ontologyProjectionTitle(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	for _, name := range []string{"title", "name", "id"} {
		if binding, ok := projection.Fields[name]; ok {
			values := uniqueStrings(binding.Values)
			if len(values) > 0 {
				return values[0]
			}
		}
	}
	if projection.Snapshot != nil {
		if node, ok := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; ok && strings.TrimSpace(node.Title) != "" {
			return strings.TrimSpace(node.Title)
		}
	}
	return projection.Ref.String()
}

func minPositive(a, b int) int {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
