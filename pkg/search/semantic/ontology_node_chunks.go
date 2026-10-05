package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

const (
	// OntologyNodeOwnerType is the intel chunk owner type for ontology node
	// semantic chunks.
	OntologyNodeOwnerType = "ontology_node"
	// GranularityOntologyNodeBody marks source-preserving node body chunks.
	GranularityOntologyNodeBody = "node_body"
	// GranularityOntologyNodeVisible marks provider-projected visible root evidence.
	GranularityOntologyNodeVisible = "node_body_visible"
	// GranularityOntologyNodeSupplemental marks bounded inline script/data evidence.
	// RootEvidenceChunkPlan assigns it only to ontology_node owners; visible
	// doc-section retrieval relies on that generated ownership invariant.
	GranularityOntologyNodeSupplemental = "node_body_supplemental"
	// FallbackNoteTypeName is the internal semantic type for untyped notes.
	FallbackNoteTypeName              = ontology.FallbackNoteTypeName
	ontologyPrimaryChunkFormatVersion = 2
)

// OntologyNodeChunkSet is the semantic embedding plan for projected ontology
// notes. Nodes/field values are identity sidecars for chunk construction and
// tests; ontology catalog sync owns durable node/field persistence.
type OntologyNodeChunkSet struct {
	NotePaths   []string
	Nodes       []codeanchor.IntelOntologyNode
	FieldValues []codeanchor.IntelOntologyNodeFieldValue
	Chunks      []codeanchor.IntelChunk
	States      []codeanchor.IntelOntologyNodeEmbeddingState
	Texts       map[string]string
}

type ontologyNodeChunker struct {
	Schema       *ontology.Schema
	ProviderInfo embeddings.ProviderConfig
	Now          int64
	MaxBodyBytes int
}

type ontologyAncestorFrame struct {
	Type  string
	Title string
	Field string
}

// OntologyNodeBodyParts returns the same body slices used for ontology-node embeddings.
func OntologyNodeBodyParts(projection *ontology.NodeProjection, maxBytes int) []string {
	if maxBytes <= 0 {
		maxBytes = defaultSectionMaxBytes
	}
	return nodeOwnMarkdownParts(projection, maxBytes)
}

// BuildOntologyNodeChunks projects a typed note into first-class semantic node chunks.
func BuildOntologyNodeChunks(schema *ontology.Schema, projection *ontology.NodeProjection, providerInfo embeddings.ProviderConfig, now int64) (OntologyNodeChunkSet, error) {
	if now == 0 {
		now = time.Now().Unix()
	}
	c := ontologyNodeChunker{
		Schema:       schema,
		ProviderInfo: providerInfo,
		Now:          now,
		MaxBodyBytes: defaultSectionMaxBytes,
	}
	out := OntologyNodeChunkSet{Texts: map[string]string{}}
	if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
		projection = fallback
	}
	if projection == nil || projection.Snapshot == nil || schema == nil || strings.TrimSpace(projection.ResolvedType) == "" {
		return out, nil
	}
	if err := c.collect(&out, projection, nil, "", nil); err != nil {
		return out, err
	}
	if ontology.IsFallbackNoteProjection(projection) {
		rootFrame := ontologyAncestorFrame{Type: projection.ResolvedType, Title: nodeProjectionTitleForEmbedding(projection), Field: "section"}
		framesBySectionID := make(map[string][]ontologyAncestorFrame)
		for _, section := range ontology.FallbackSectionProjections(projection) {
			ancestors := []ontologyAncestorFrame{rootFrame}
			if parentFrames, ok := framesBySectionID[section.Ref.ParentID]; ok {
				ancestors = parentFrames
			}
			if err := c.collect(&out, section, projection, "section", ancestors); err != nil {
				return out, err
			}
			framesBySectionID[section.Ref.NodeID] = append(append([]ontologyAncestorFrame(nil), ancestors...), ontologyAncestorFrame{
				Type:  section.ResolvedType,
				Title: nodeProjectionTitleForEmbedding(section),
				Field: "section",
			})
		}
	}
	out.NotePaths = uniqueSortedNonEmpty(out.NotePaths)
	return out, nil
}

func (c ontologyNodeChunker) collect(out *OntologyNodeChunkSet, projection *ontology.NodeProjection, parent *ontology.NodeProjection, parentField string, ancestors []ontologyAncestorFrame) error {
	if projection == nil {
		return nil
	}
	node, err := ontology.IntelOntologyNodeForProjection(c.Schema, projection, parent, c.Now)
	if err != nil {
		return err
	}
	title := nodeProjectionTitleForEmbedding(projection)
	parentType := ""
	if parent != nil {
		parentType = strings.TrimSpace(parent.ResolvedType)
	}
	node.Title = title
	out.NotePaths = append(out.NotePaths, node.NotePath)
	out.Nodes = append(out.Nodes, node)
	out.FieldValues = append(out.FieldValues, ontology.IntelOntologyNodeFieldValuesForProjection(c.Schema, projection, node, c.Now)...)

	sig := EmbeddingSchemaSignature(c.Schema, node.TypeName, parentType, parentField)
	sourceParts := nodeOwnMarkdownParts(projection, c.MaxBodyBytes)
	sourceHash := hashString(strings.Join(sourceParts, "\n\n---\n\n"))
	if len(sourceParts) == 0 {
		sourceParts = []string{""}
	}
	for i, body := range sourceParts {
		text := c.nodeBodyText(projection, node, ancestors, body, i == 0)
		c.appendChunk(out, node, i, GranularityOntologyNodeBody, text, sig, sourceHash)
	}

	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field := projection.Fields[name]
		for _, ref := range field.SectionNodes {
			child, err := ontology.ProjectBoundNodeFromSnapshot(projection.Snapshot, c.Schema, ref)
			if err != nil {
				return err
			}
			childAncestors := append(append([]ontologyAncestorFrame(nil), ancestors...), ontologyAncestorFrame{
				Type:  projection.ResolvedType,
				Title: title,
				Field: name,
			})
			if err := c.collect(out, child, projection, name, childAncestors); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c ontologyNodeChunker) appendChunk(out *OntologyNodeChunkSet, node codeanchor.IntelOntologyNode, ord int, granularity string, text string, schemaSig string, sourceHash string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	chunkID := codeanchor.IntelChunkID(node.NodeID, ord, granularity)
	textHash := embeddings.HashText(text)
	out.Chunks = append(out.Chunks, codeanchor.IntelChunk{
		ChunkID:     chunkID,
		OwnerID:     node.NodeID,
		OwnerType:   OntologyNodeOwnerType,
		Ord:         ord,
		Granularity: granularity,
		Breadcrumb:  ontologyNodeBreadcrumb(node),
		Heading:     node.Title,
		ContentHash: textHash,
		StartByte:   node.StartByte,
		EndByte:     node.EndByte,
		UpdatedAt:   c.Now,
	})
	out.States = append(out.States, codeanchor.IntelOntologyNodeEmbeddingState{
		ChunkID:                  chunkID,
		NodeID:                   node.NodeID,
		NotePath:                 node.NotePath,
		TypeName:                 node.TypeName,
		NodeKind:                 node.NodeKind,
		EmbeddingSchemaSignature: schemaSig,
		NodeStructureFingerprint: firstNonEmpty(node.StructuralFingerprint, hashString(node.NodeRefJSON)),
		SourceContentHash:        sourceHash,
		ChunkTextHash:            textHash,
		ChunkGranularity:         granularity,
		Provider:                 c.ProviderInfo.Provider,
		Model:                    c.ProviderInfo.Model,
		UpdatedAt:                c.Now,
	})
	out.Texts[chunkID] = text
}

func (c ontologyNodeChunker) nodeBodyText(projection *ontology.NodeProjection, node codeanchor.IntelOntologyNode, ancestors []ontologyAncestorFrame, body string, includeFields bool) string {
	lines := []string{
		"Kind: ontology_node",
		"NodeKind: " + node.NodeKind,
		"Type: " + node.TypeName,
		"Title: " + node.Title,
		"Path: " + node.NotePath,
		"Ref: " + projection.Ref.String(),
		"Breadcrumb: " + ontologyNodeBreadcrumb(node),
	}
	if breadcrumb := renderOntologyAncestors(ancestors); breadcrumb != "" {
		lines = append(lines, "Ancestors: "+breadcrumb)
	}
	if includeFields {
		if fieldLines := selectedOntologyFieldSignalLines(c.Schema, projection, node); len(fieldLines) > 0 {
			lines = append(lines, "Fields:")
			lines = append(lines, fieldLines...)
		}
	}
	if body = strings.TrimSpace(body); body != "" {
		lines = append(lines, "", body)
	}
	return strings.Join(lines, "\n")
}

func EmbeddingSchemaSignature(schema *ontology.Schema, typeName, parentType, parentField string) string {
	if schema == nil || strings.TrimSpace(typeName) == "" {
		return ""
	}
	payload := map[string]any{
		"formatVersion": ontologyPrimaryChunkFormatVersion,
		"type":          schemaTypeSignature(schema, typeName),
		"parentType":    strings.TrimSpace(parentType),
		"parentField":   strings.TrimSpace(parentField),
	}
	data, _ := json.Marshal(payload)
	return hashString(string(data))
}

func schemaTypeSignature(schema *ontology.Schema, typeName string) any {
	if schema == nil {
		return nil
	}
	if ontology.IsFallbackNoteTypeName(typeName) {
		return map[string]any{
			"name":  FallbackNoteTypeName,
			"role":  "NOTE",
			"label": "untyped note",
		}
	}
	if ontology.IsFallbackSectionTypeName(typeName) {
		return map[string]any{
			"name":  ontology.FallbackSectionTypeName,
			"role":  "SECTION",
			"label": "untyped note section",
		}
	}
	if t := schema.Types[typeName]; t != nil {
		fields := make([]map[string]any, 0, len(t.Fields))
		for _, f := range t.Fields {
			if f == nil {
				continue
			}
			fields = append(fields, fieldSignature(f, schema))
		}
		sort.Slice(fields, func(i, j int) bool { return fmt.Sprint(fields[i]["name"]) < fmt.Sprint(fields[j]["name"]) })
		return map[string]any{
			"name":      t.Name,
			"role":      t.Role,
			"label":     t.Label,
			"semantics": t.Semantics,
			"guidance":  guidanceSummary(t.Guidance),
			"fields":    fields,
		}
	}
	return nil
}

func fieldSignature(f *ontology.Field, schema *ontology.Schema) map[string]any {
	out := map[string]any{
		"name":           f.Name,
		"kind":           f.Kind,
		"type":           f.TypeName,
		"list":           f.List,
		"required":       f.Required,
		"source":         f.Source,
		"sourceAliases":  append([]string(nil), f.SourceAliases...),
		"sourceKind":     f.SourceKind,
		"contextInclude": f.ContextInclude,
		"semantics":      f.Semantics,
		"guidance":       guidanceSummary(f.Guidance),
		"sectionHeading": f.SectionHeading,
		"sectionLevel":   f.SectionLevel,
		"direction":      f.Direction,
		"scope":          f.Scope,
	}
	if values := ontology.EnumValuesSet(schema, f.TypeName); len(values) > 0 {
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		out["enumValues"] = names
	}
	return out
}

func guidanceSummary(g *ontology.Guidance) string {
	if g == nil {
		return ""
	}
	return strings.Join(nonEmptyStrings(g.Summary, g.Meaning, g.AgentImplications), " ")
}

func nodeProjectionTitleForEmbedding(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	if projection.Ref.Kind == ontology.NodeKindNote {
		segment := lastPathSegment(projection.Ref.NotePath)
		return strings.TrimSuffix(segment, filepath.Ext(segment))
	}
	if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
		return strings.TrimSpace(node.Title)
	}
	return projection.Ref.String()
}

func ontologyNodeBreadcrumb(node codeanchor.IntelOntologyNode) string {
	return strings.Join(nonEmptyStrings(node.NotePath, node.TypeName, node.Title), " > ")
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func uniqueSortedNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
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

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func lastPathSegment(path string) string {
	path = strings.Trim(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
