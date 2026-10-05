package ontology

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/notemeta"
)

// projectRootNodeForDoc projects one provider-owned file root. Markdown keeps
// its structural projector; root-only formats bind ontology fields directly to
// provider metadata and never acquire a synthetic Markdown snapshot.
func projectRootNodeForDoc(doc *noteDoc, schema *Schema) (*NodeProjection, error) {
	if doc == nil || doc.Root == nil || schema == nil {
		return nil, nil
	}
	if doc.Snapshot != nil {
		projection, err := ProjectNodeFromSnapshot(doc.Snapshot, schema, NodeRef{
			NotePath: doc.Path, Kind: NodeKindNote, TypeName: doc.TypeName,
		})
		if projection != nil {
			projection.RootSnapshot = doc.Root
		}
		return projection, err
	}
	assessment := resolveNoteAssessment(doc, schema)
	typeName := ""
	var noteType *NoteType
	if assessment != nil {
		typeName = strings.TrimSpace(assessment.ResolvedType)
		noteType = schema.Types[typeName]
	}
	projection := &NodeProjection{
		Ref: NodeRef{
			NotePath: doc.Path, Kind: NodeKindNote, TypeName: typeName,
			EndByte: len(doc.Root.RawSource),
		},
		RootSnapshot: doc.Root,
		ResolvedType: typeName,
		Type:         noteType,
		Assessment:   assessment,
		PropertyCase: propertyCaseOrDefault(noteType),
		Fields:       map[string]FieldBinding{},
		Collections:  map[string]CollectionBinding{},
	}
	if noteType == nil {
		return projection, nil
	}
	for _, field := range noteType.Fields {
		if field == nil {
			continue
		}
		switch field.Kind {
		case FieldKindScalar, FieldKindEnum, FieldKindLink:
			projection.Fields[field.Name] = rootMetadataFieldBinding(doc, field)
		case FieldKindSection:
			projection.Fields[field.Name] = FieldBinding{
				FieldName: field.Name, Kind: BindingKindSectionList,
				SourceKind: field.SourceKind,
			}
		}
	}
	return projection, nil
}

// ProjectRootDocumentSnapshot projects a provider-current root snapshot into
// the ontology without assuming a structural source language.
func ProjectRootDocumentSnapshot(root *RootDocumentSnapshot, schema *Schema) (*NodeProjection, error) {
	if root == nil {
		return nil, nil
	}
	doc := &noteDoc{
		Path: root.NotePath.String(), Title: root.Title,
		Content: string(root.RawSource), Frontmatter: cloneAnyMap(root.Metadata),
		Inline: cloneNoteInline(root.InlineProperties), Tags: append([]string(nil), root.Tags...),
		Links:      append([]notemeta.ResolvedNoteLink(nil), root.Links...),
		Projection: root.Projection.Copy(), TypeName: stringValue(root.Metadata[typeFieldName]),
		Root: root, Snapshot: root.MarkdownStructure,
	}
	return projectRootNodeForDoc(doc, schema)
}

func rootMetadataFieldBinding(doc *noteDoc, field *Field) FieldBinding {
	binding := FieldBinding{
		FieldName: field.Name, Kind: BindingKindFrontmatterField,
		SourceKind: field.SourceKind,
		Present:    fieldPresent(doc, field),
		Values:     normalizeFieldValues(field, extractFieldValues(doc, field)),
	}
	if doc == nil || doc.Root == nil || field.SourceKind == FieldSourceInline {
		return binding
	}
	names := FieldSourceNames(field)
	for _, fact := range doc.Root.RootMetadata {
		matched := false
		for _, name := range names {
			if strings.EqualFold(strings.TrimSpace(fact.Key), strings.TrimSpace(name)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if fact.ValueRange.Present {
			r := fact.ValueRange.Range
			binding.ValueRanges = append(binding.ValueRanges, ByteRange{Start: r.StartByte, End: r.EndByte})
		}
		if fact.Range.Present {
			r := fact.Range.Range
			if binding.Range == (ByteRange{}) {
				binding.Range = ByteRange{Start: r.StartByte, End: r.EndByte}
			} else {
				binding.Range.Start = min(binding.Range.Start, r.StartByte)
				binding.Range.End = max(binding.Range.End, r.EndByte)
			}
		}
	}
	binding.ValueRangesExact = metadataRangesExactlyMatch(doc.Root.RawSource, binding.Values, binding.ValueRanges)
	return binding
}

func metadataRangesExactlyMatch(source []byte, values []string, ranges []ByteRange) bool {
	if len(values) == 0 || len(values) != len(ranges) {
		return false
	}
	for index, sourceRange := range ranges {
		if !sourceRange.Valid(len(source)) || strings.TrimSpace(string(source[sourceRange.Start:sourceRange.End])) != strings.TrimSpace(values[index]) {
			return false
		}
	}
	return true
}

// ProjectNoteSourceSnapshot uses the same provider-current source assembly as
// structural ontology indexing. Root-only formats retain provider facts, and
// structural capabilities attach their genuine syntax snapshot inside ontology.
func ProjectNoteSourceSnapshot(source notemeta.NoteSourceSnapshot, schema *Schema) (*NodeProjection, error) {
	doc, err := projectNoteSource(source)
	if err != nil {
		return nil, err
	}
	return ProjectRootDocumentSnapshot(doc.Root, schema)
}
