package query

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func (e *executor) sectionNodes(ctx context.Context, note *noteRecord) ([]*ontology.SectionNode, error) {
	if note == nil {
		return nil, nil
	}
	nodes, ok := e.sectionsByPath[note.Path]
	if ok {
		return nodes, nil
	}
	projection, err := e.projectNoteRecord(ctx, note)
	if err != nil {
		return nil, err
	}
	if projection == nil || projection.Snapshot == nil {
		return nil, nil
	}
	nodes = projection.Snapshot.Sections
	e.sectionsByPath[note.Path] = nodes
	return nodes, nil
}

func (e *executor) headingSectionRecordsForNote(ctx context.Context, note *noteRecord) ([]*sectionRecord, error) {
	nodes, err := e.sectionNodes(ctx, note)
	if err != nil {
		return nil, err
	}
	propertyCase := e.propertyCaseForType(note.TypeName)
	out := make([]*sectionRecord, 0)
	var walk func(parentTypeName string, children []*ontology.SectionNode, allowDocumentTitle bool)
	walk = func(parentTypeName string, children []*ontology.SectionNode, allowDocumentTitle bool) {
		for _, child := range children {
			if child == nil {
				continue
			}
			typeName := e.childSectionType(&sectionRecord{TypeName: parentTypeName}, child)
			record := &sectionRecord{
				Node:         child,
				Note:         note,
				TypeName:     typeName,
				PropertyCase: propertyCase,
			}
			out = append(out, record)
			nestedParentType := typeName
			if allowDocumentTitle && len(children) == 1 && child.Level == ontoschema.SectionLevelH1 && nestedParentType == "Section" {
				// WHY: A lone unmatched H1 is the Markdown document title, not an
				// ontology field. Other unmatched wrappers remain opaque so their
				// descendants cannot leak into level-only section lists.
				nestedParentType = parentTypeName
			}
			walk(nestedParentType, child.Children, false)
		}
	}
	walk(note.TypeName, nodes, true)
	return out, nil
}

func (e *executor) matchSectionsForField(ctx context.Context, node *noteRecord, field *ontoschema.Field) ([]*sectionRecord, error) {
	if field != nil && field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != ontoschema.EmbeddedSourceShapeSection {
		projection, err := e.projectNoteRecord(ctx, node)
		if err != nil {
			return nil, err
		}
		return e.sectionRecordsFromProjectionField(projection, node, field), nil
	}
	nodes, err := e.sectionNodes(ctx, node)
	if err != nil {
		return nil, err
	}
	matches := ontology.MatchSectionNodesForField(nodes, field)
	propertyCase := e.propertyCaseForType(node.TypeName)
	out := make([]*sectionRecord, 0, len(matches))
	for _, match := range matches {
		out = append(out, &sectionRecord{
			Node:         match,
			Note:         node,
			TypeName:     field.TypeName,
			PropertyCase: propertyCase,
		})
	}
	return out, nil
}

func (e *executor) matchNestedSections(ctx context.Context, node *sectionRecord, field *ontoschema.Field) ([]*sectionRecord, error) {
	if node == nil {
		return nil, nil
	}
	if field != nil && field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != ontoschema.EmbeddedSourceShapeSection {
		projection := node.Projection
		if projection == nil {
			var err error
			projection, err = e.projectSectionRecord(ctx, node)
			if err != nil {
				return nil, err
			}
		}
		return e.sectionRecordsFromProjectionField(projection, node.Note, field), nil
	}
	if node.Node == nil {
		return nil, nil
	}
	matches := ontology.MatchSectionNodesForField(node.Node.Children, field)
	out := make([]*sectionRecord, 0, len(matches))
	for _, match := range matches {
		out = append(out, &sectionRecord{
			Node:         match,
			Note:         node.Note,
			TypeName:     field.TypeName,
			PropertyCase: node.PropertyCase,
		})
	}
	return out, nil
}

func (e *executor) itemProjectionRecordsForNote(ctx context.Context, note *noteRecord) ([]*sectionRecord, error) {
	projection, err := e.projectNoteRecord(ctx, note)
	if err != nil {
		return nil, err
	}
	out := make([]*sectionRecord, 0)
	seen := map[string]struct{}{}
	appendItem := func(child *ontology.NodeProjection) {
		if child == nil {
			return
		}
		if child.Record == nil {
			if child.Snapshot == nil {
				return
			}
			sourceSpan := child.Snapshot.SourceSpansByID[child.Ref.NodeID]
			if sourceSpan == nil || sourceSpan.Shape == ontoschema.EmbeddedSourceShapeSection {
				return
			}
		}
		key := child.Ref.String() + "\x00" + child.Ref.NodeID + "\x00" + child.Ref.TypeName
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, e.sectionRecordFromProjection(note, child))
	}
	var walk func(*ontology.NodeProjection) error
	walk = func(parent *ontology.NodeProjection) error {
		if parent == nil {
			return nil
		}
		names := make([]string, 0, len(parent.Fields))
		for name := range parent.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, ref := range parent.Fields[name].ChildRefs {
				child, err := parent.Prepared.ProjectBound(ref)
				if err != nil {
					return err
				}
				appendItem(child)
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if projection == nil || projection.Prepared == nil {
		return out, nil
	}
	if err := walk(projection); err != nil {
		return nil, err
	}
	globalRefs, err := projection.Prepared.GlobalSourceRefs()
	if err != nil {
		return nil, err
	}
	for _, ref := range globalRefs {
		child, err := projection.Prepared.ProjectBound(ref)
		if err != nil {
			return nil, err
		}
		appendItem(child)
	}
	return out, nil
}

func (e *executor) projectNoteRecord(ctx context.Context, note *noteRecord) (*ontology.NodeProjection, error) {
	if note == nil {
		return nil, nil
	}
	if e.loaders == nil || e.loaders.scope == nil {
		return nil, nil
	}
	return e.loaders.scope.Projection(ctx, ontology.NodeRef{NotePath: note.Path, Kind: ontology.NodeKindNote, TypeName: note.TypeName})
}

func (e *executor) projectSectionRecord(ctx context.Context, node *sectionRecord) (*ontology.NodeProjection, error) {
	if node == nil {
		return nil, nil
	}
	if node.Projection != nil {
		return node.Projection, nil
	}
	return e.loaders.scope.Projection(ctx, e.sectionNodeRef(node))
}

func (e *executor) sectionRecordsFromProjectionField(projection *ontology.NodeProjection, note *noteRecord, field *ontoschema.Field) []*sectionRecord {
	if projection == nil || field == nil {
		return nil
	}
	binding := projection.Fields[field.Name]
	out := make([]*sectionRecord, 0, len(binding.ChildRefs))
	for _, ref := range binding.ChildRefs {
		child, err := projection.Prepared.ProjectBound(ref)
		if err != nil {
			continue
		}
		out = append(out, e.sectionRecordFromProjection(note, child))
	}
	return out
}

func (e *executor) sectionRecordFromProjection(note *noteRecord, projection *ontology.NodeProjection) *sectionRecord {
	if projection == nil {
		return nil
	}
	return &sectionRecord{
		Node:         sectionNodeFromProjection(projection),
		Projection:   projection,
		Note:         note,
		TypeName:     projection.ResolvedType,
		PropertyCase: projection.PropertyCase,
	}
}

func sectionNodeFromProjection(projection *ontology.NodeProjection) *ontology.SectionNode {
	if projection == nil || projection.Snapshot == nil {
		return nil
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return section
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return &ontology.SectionNode{
			ID:               span.ID,
			NotePath:         span.NotePath,
			Title:            span.Title,
			BlockID:          span.BlockID,
			MalformedBlockID: span.MalformedBlockID,
			StartByte:        span.Range.Start,
			EndByte:          span.Range.End,
			Content:          span.Content,
		}
	}
	return nil
}

// propertyCaseForType returns the PropertyCase for a note type, or the kebab
// default when the type is absent (e.g., ambient ImplicitNote).
func (e *executor) propertyCaseForType(typeName string) ontoschema.PropertyCase {
	if e == nil || e.schema == nil {
		return ontoschema.PropertyCaseKebab
	}
	if typeName == "" {
		return ontoschema.PropertyCaseKebab
	}
	if noteType := e.schema.Types[typeName]; noteType != nil && noteType.PropertyCase != "" {
		return noteType.PropertyCase
	}
	return ontoschema.PropertyCaseKebab
}

func (e *executor) childSectionType(parent *sectionRecord, child *ontology.SectionNode) string {
	if parent == nil || child == nil || strings.TrimSpace(parent.TypeName) == "" || parent.TypeName == "Section" {
		return "Section"
	}
	parentType := e.schema.Types[parent.TypeName]
	if parentType == nil {
		return "Section"
	}
	// Prefer heading-matched fields because they're strictly more specific
	// than list-no-heading catch-alls. Walk in two passes so a heading match
	// always wins over an overlapping level-only match.
	headingMatchCount := 0
	headingMatchType := ""
	levelMatchCount := 0
	levelMatchType := ""
	for _, field := range parentType.Fields {
		if field == nil || field.Kind != ontoschema.FieldKindSection {
			continue
		}
		if field.SectionLevel != child.Level {
			continue
		}
		if strings.TrimSpace(field.SectionHeading) != "" {
			if strings.TrimSpace(field.SectionHeading) == strings.TrimSpace(child.Title) {
				headingMatchCount++
				headingMatchType = field.TypeName
			}
			continue
		}
		if field.List {
			levelMatchCount++
			levelMatchType = field.TypeName
		}
	}
	switch {
	case headingMatchCount == 1 && strings.TrimSpace(headingMatchType) != "":
		return headingMatchType
	case headingMatchCount > 1:
		return "Section"
	case levelMatchCount == 1 && strings.TrimSpace(levelMatchType) != "":
		return levelMatchType
	}
	return "Section"
}

func (e *executor) notePathCacheForSections(ctx context.Context) (*obsidian.NotePathCache, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.notePathCache != nil {
		return e.notePathCache, nil
	}
	cache, err := buildCurrentNotePathCache(ctx, e.loaders.deps)
	if err == nil {
		e.notePathCache = cache
	}
	return cache, err
}

func relationAssessmentHasStrictIssue(ok bool, relation ontology.RelationAssessment) bool {
	return ok && len(relation.Issues) > 0
}

func unwrapSectionLinkValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[[") && strings.HasSuffix(raw, "]]") {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, "]]"), "[[")
		if idx := strings.Index(raw, "|"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

func sectionLinkTypeMatchesOrImplements(schema *ontoschema.Schema, actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual == "" || expected == "" {
		return false
	}
	if actual == expected || expected == "Note" {
		return true
	}
	if schema == nil {
		return false
	}
	if noteType := schema.Types[actual]; noteType != nil {
		for _, iface := range noteType.Implements {
			if iface == expected {
				return true
			}
		}
	}
	return false
}
