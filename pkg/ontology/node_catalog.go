package ontology

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// BuildIntelOntologyNodes projects a typed note into persisted ontology-node
// catalog rows. The catalog is intentionally independent from semantic
// embeddings so locator resolution works even when embeddings are disabled.
// Docs: [[ontology-indexed-read-model-contract]]
// and [[ontology-indexed-read-model-contract#^spec-0040-noderef-json]] define
// this catalog as the indexed source of NodeRefJSON/source_locator identity.
func BuildIntelOntologyNodes(schema *Schema, projection *NodeProjection, now int64) ([]string, []codeanchor.IntelOntologyNode, error) {
	readModel, err := BuildIntelOntologyNodeReadModel(schema, projection, now)
	if err != nil {
		return nil, nil, err
	}
	return readModel.NotePaths, readModel.Nodes, nil
}

type OntologyLinkTarget struct {
	NotePath      string
	TypeName      string
	NodeID        string
	RefJSON       string
	SourceLocator string
}

type OntologyLinkTargetResolver interface {
	ResolveOntologyLinkTarget(field *Field, targetInput string) (OntologyLinkTarget, bool)
}

type OntologyLinkTargetResolverFunc func(field *Field, targetInput string) (OntologyLinkTarget, bool)

func (fn OntologyLinkTargetResolverFunc) ResolveOntologyLinkTarget(field *Field, targetInput string) (OntologyLinkTarget, bool) {
	if fn == nil {
		return OntologyLinkTarget{}, false
	}
	return fn(field, targetInput)
}

type BuildIntelOntologyNodeReadModelOptions struct {
	LinkResolver OntologyLinkTargetResolver
}

func BuildIntelOntologyNodeReadModel(schema *Schema, projection *NodeProjection, now int64) (codeanchor.IntelOntologyNodeReadModel, error) {
	return BuildIntelOntologyNodeReadModelWithOptions(schema, projection, now, BuildIntelOntologyNodeReadModelOptions{})
}

func BuildIntelOntologyNodeReadModelWithOptions(schema *Schema, projection *NodeProjection, now int64, opts BuildIntelOntologyNodeReadModelOptions) (codeanchor.IntelOntologyNodeReadModel, error) {
	if now == 0 {
		now = time.Now().Unix()
	}
	if projection == nil || (projection.Snapshot == nil && projection.RootSnapshot == nil) || schema == nil {
		return codeanchor.IntelOntologyNodeReadModel{}, nil
	}
	if projection.Snapshot == nil {
		rootProjection := projection
		if strings.TrimSpace(rootProjection.ResolvedType) == "" {
			if fallback, ok := AsFallbackNoteProjection(rootProjection); ok {
				rootProjection = fallback
			}
		}
		if strings.TrimSpace(rootProjection.ResolvedType) == "" {
			return codeanchor.IntelOntologyNodeReadModel{}, nil
		}
		node, err := IntelOntologyNodeForProjection(schema, rootProjection, nil, now)
		if err != nil {
			return codeanchor.IntelOntologyNodeReadModel{}, err
		}
		fields, dependencies := IntelOntologyNodeFieldReadModelForProjection(schema, rootProjection, node, now, opts)
		return codeanchor.IntelOntologyNodeReadModel{
			NotePaths:        []string{rootProjection.Ref.NotePath},
			Nodes:            []codeanchor.IntelOntologyNode{node},
			FieldValues:      fields,
			LinkDependencies: dependencies,
		}, nil
	}
	resolver, err := newProjectionResolver(projection.Snapshot, schema)
	if err != nil {
		return codeanchor.IntelOntologyNodeReadModel{}, err
	}
	var nodes []codeanchor.IntelOntologyNode
	var fields []codeanchor.IntelOntologyNodeFieldValue
	var dependencies []codeanchor.IntelOntologyNodeLinkDependency
	seen := map[string]struct{}{}
	rootProjection := projection
	// WHY: untyped notes that match a global @source (e.g. notes containing
	// `#action-item` checkbox items) need a FallbackNote NOTE row in the
	// catalog so downstream writers do not have to repeat the row themselves
	// with destructive Replace semantics. Without this row the only writer
	// that knew to emit it (the semantic syncer) would wipe the embedded
	// child rows along with re-inserting the FallbackNote.
	if strings.TrimSpace(rootProjection.ResolvedType) == "" {
		if fallback, ok := AsFallbackNoteProjection(rootProjection); ok {
			rootProjection = fallback
		}
	}
	if strings.TrimSpace(rootProjection.ResolvedType) != "" {
		if err := collectIntelOntologyNodeReadModel(resolver, rootProjection, nil, now, opts, &nodes, &fields, &dependencies, seen); err != nil {
			return codeanchor.IntelOntologyNodeReadModel{}, err
		}
		if IsFallbackNoteProjection(rootProjection) {
			for _, section := range FallbackSectionProjections(rootProjection) {
				if err := collectIntelOntologyNodeReadModel(resolver, section, rootProjection, now, opts, &nodes, &fields, &dependencies, seen); err != nil {
					return codeanchor.IntelOntologyNodeReadModel{}, err
				}
			}
		}
	}
	globalRefs := resolver.globalSourceNodeRefs()
	for _, ref := range globalRefs {
		child, err := resolver.projectCurrent(ref)
		if err != nil {
			return codeanchor.IntelOntologyNodeReadModel{}, err
		}
		if err := collectIntelOntologyNodeReadModel(resolver, child, rootProjection, now, opts, &nodes, &fields, &dependencies, seen); err != nil {
			return codeanchor.IntelOntologyNodeReadModel{}, err
		}
	}
	paths := make([]string, 0, len(nodes))
	for _, node := range nodes {
		paths = append(paths, node.NotePath)
	}
	return codeanchor.IntelOntologyNodeReadModel{
		NotePaths:        uniqueSortedNodeCatalogStrings(paths),
		Nodes:            nodes,
		FieldValues:      fields,
		LinkDependencies: dependencies,
	}, nil
}

func collectIntelOntologyNodeReadModel(resolver *projectionResolver, projection *NodeProjection, parent *NodeProjection, now int64, opts BuildIntelOntologyNodeReadModelOptions, out *[]codeanchor.IntelOntologyNode, fieldOut *[]codeanchor.IntelOntologyNodeFieldValue, dependencyOut *[]codeanchor.IntelOntologyNodeLinkDependency, seen map[string]struct{}) error {
	if projection == nil {
		return nil
	}
	node, err := IntelOntologyNodeForProjection(resolver.schema, projection, parent, now)
	if err != nil {
		return err
	}
	if seen != nil {
		if _, ok := seen[node.NodeID]; ok {
			return nil
		}
		seen[node.NodeID] = struct{}{}
	}
	*out = append(*out, node)
	if fieldOut != nil {
		fieldValues, dependencies := IntelOntologyNodeFieldReadModelForProjection(resolver.schema, projection, node, now, opts)
		*fieldOut = append(*fieldOut, fieldValues...)
		if dependencyOut != nil {
			*dependencyOut = append(*dependencyOut, dependencies...)
		}
	}

	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, ref := range projection.Fields[name].SectionNodes {
			child, err := resolver.projectCurrent(ref)
			if err != nil {
				return err
			}
			if err := collectIntelOntologyNodeReadModel(resolver, child, semanticChildParent(projection, parent), now, opts, out, fieldOut, dependencyOut, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func semanticChildParent(projection *NodeProjection, parent *NodeProjection) *NodeProjection {
	if projection == nil || projection.Type == nil {
		return parent
	}
	switch projection.Type.Role {
	case TypeRoleNote, TypeRoleEmbeddedNode:
		return projection
	default:
		return parent
	}
}

// IntelOntologyNodeForProjection creates the persisted catalog row for one projection.
func IntelOntologyNodeForProjection(schema *Schema, projection *NodeProjection, parent *NodeProjection, now int64) (codeanchor.IntelOntologyNode, error) {
	if now == 0 {
		now = time.Now().Unix()
	}
	fragment, blockID := nodeCatalogFragments(projection)
	ref := projection.Ref
	ref.TypeName = strings.TrimSpace(firstNonEmpty(ref.TypeName, projection.ResolvedType))
	if ref.Fragment == "" && fragment != "" {
		ref.Fragment = fragment
	}
	nodeID := OntologyNodeID(ref)
	nodeRefJSON, err := json.Marshal(ref)
	if err != nil {
		return codeanchor.IntelOntologyNode{}, err
	}
	parentID := ""
	parentType := ""
	if parent != nil {
		parentID = OntologyNodeID(parent.Ref)
		parentType = strings.TrimSpace(parent.ResolvedType)
	}
	sourceLocator := NodeSourceLocator(ref)
	displayLabel := nodeCatalogDisplayLabel(projection)
	return codeanchor.IntelOntologyNode{
		NodeID:                nodeID,
		NotePath:              ref.NotePath,
		NodeRefJSON:           string(nodeRefJSON),
		NodeKind:              string(ref.Kind),
		TypeName:              ref.TypeName,
		ParentNodeID:          parentID,
		ParentTypeName:        parentType,
		Title:                 displayLabel,
		SourceLocator:         sourceLocator,
		Fragment:              fragment,
		BlockID:               blockID,
		DisplayLabel:          displayLabel,
		LocatorStatus:         nodeCatalogLocatorStatus(ref, blockID),
		StartByte:             int64(ref.StartByte),
		EndByte:               int64(ref.EndByte),
		StructuralFingerprint: strings.TrimSpace(ref.Structural),
		SchemaHash:            schemaHash(schema),
		UpdatedAt:             now,
	}, nil
}

func IntelOntologyNodeFieldValuesForProjection(schema *Schema, projection *NodeProjection, node codeanchor.IntelOntologyNode, now int64) []codeanchor.IntelOntologyNodeFieldValue {
	rows, _ := IntelOntologyNodeFieldReadModelForProjection(schema, projection, node, now, BuildIntelOntologyNodeReadModelOptions{})
	return rows
}

func IntelOntologyNodeFieldReadModelForProjection(schema *Schema, projection *NodeProjection, node codeanchor.IntelOntologyNode, now int64, opts BuildIntelOntologyNodeReadModelOptions) ([]codeanchor.IntelOntologyNodeFieldValue, []codeanchor.IntelOntologyNodeLinkDependency) {
	if projection == nil || projection.Type == nil || strings.TrimSpace(node.NodeID) == "" {
		return nil, nil
	}
	var out []codeanchor.IntelOntologyNodeFieldValue
	var dependencies []codeanchor.IntelOntologyNodeLinkDependency
	for _, field := range projection.Type.Fields {
		if field == nil {
			continue
		}
		switch field.Kind {
		case FieldKindScalar, FieldKindEnum, FieldKindLink:
		case FieldKindSection:
			if !IsSectionSummary(field) {
				continue
			}
		default:
			continue
		}
		binding, ok := projection.Fields[field.Name]
		if !ok {
			continue
		}
		values := binding.Values
		if IsSectionSummary(field) {
			values = SectionSummaryValues(projection, field)
		}
		if len(values) == 0 && binding.Present {
			values = []string{""}
		}
		for i, value := range values {
			// Empty-binding Link fields would emit a row with empty value_norm and
			// no target identity, which leaves a stale "present but unresolved"
			// signal in the index. Skip the row instead so reads stay quiet.
			if field.Kind == FieldKindLink && strings.TrimSpace(value) == "" {
				continue
			}
			row, dependency := ontologyNodeFieldValueForValueWithOptions(schema, node, field, binding, value, i, now, opts)
			out = append(out, row)
			if dependency != nil {
				dependencies = append(dependencies, *dependency)
			}
		}
	}
	return out, dependencies
}

func ontologyNodeFieldValueForValue(schema *Schema, node codeanchor.IntelOntologyNode, field *Field, binding FieldBinding, value string, ordinal int, now int64) codeanchor.IntelOntologyNodeFieldValue {
	row, _ := ontologyNodeFieldValueForValueWithOptions(schema, node, field, binding, value, ordinal, now, BuildIntelOntologyNodeReadModelOptions{})
	return row
}

func ontologyNodeFieldValueForValueWithOptions(schema *Schema, node codeanchor.IntelOntologyNode, field *Field, binding FieldBinding, value string, ordinal int, now int64, opts BuildIntelOntologyNodeReadModelOptions) (codeanchor.IntelOntologyNodeFieldValue, *codeanchor.IntelOntologyNodeLinkDependency) {
	text := strings.TrimSpace(value)
	row := codeanchor.IntelOntologyNodeFieldValue{
		NodeID:      node.NodeID,
		NotePath:    node.NotePath,
		TypeName:    node.TypeName,
		FieldName:   strings.ToLower(strings.TrimSpace(field.Name)),
		FieldKind:   string(field.Kind),
		SourceKind:  string(binding.SourceKind),
		ValueText:   text,
		ValueNorm:   normalizeOntologyFieldValue(text),
		ListOrdinal: ordinal,
		SchemaHash:  schemaHash(schema),
		UpdatedAt:   now,
	}
	if field.Kind == FieldKindEnum {
		row.ValueKind = "enum"
		return row, nil
	}
	if field.Kind == FieldKindLink {
		row.ValueKind = "link"
		targetInput := normalizeOntologyLinkTarget(text)
		if opts.LinkResolver == nil {
			row.TargetNotePath = targetInput
			row.TargetSourceLocator = row.TargetNotePath
		} else if target, ok := opts.LinkResolver.ResolveOntologyLinkTarget(field, targetInput); ok {
			row.TargetNotePath = strings.TrimSpace(target.NotePath)
			row.TargetTypeName = strings.TrimSpace(target.TypeName)
			row.TargetNodeID = strings.TrimSpace(target.NodeID)
			row.TargetRefJSON = strings.TrimSpace(target.RefJSON)
			row.TargetSourceLocator = strings.TrimSpace(target.SourceLocator)
		}
		dependency := &codeanchor.IntelOntologyNodeLinkDependency{
			SourceNotePath:         node.NotePath,
			NodeID:                 node.NodeID,
			TypeName:               node.TypeName,
			FieldName:              row.FieldName,
			TargetInput:            targetInput,
			TargetInputNorm:        normalizeOntologyFieldValue(targetInput),
			ResolvedTargetNotePath: row.TargetNotePath,
			ResolvedTargetTypeName: row.TargetTypeName,
			SchemaHash:             schemaHash(schema),
			UpdatedAt:              now,
		}
		return row, dependency
	}
	switch strings.ToLower(strings.TrimSpace(field.TypeName)) {
	case "boolean", "bool":
		if b, err := strconv.ParseBool(strings.ToLower(text)); err == nil {
			row.ValueKind = "bool"
			row.ValueBool = &b
		}
	case "int", "integer":
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			row.ValueKind = "int"
			row.ValueInt = &n
		}
	case "float", "number":
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			row.ValueKind = "real"
			row.ValueReal = &f
		}
	case "date":
		if isISODate(text) {
			row.ValueKind = "date"
			row.ValueDate = &text
		}
	case "datetime":
		if t, err := time.Parse(time.RFC3339, text); err == nil {
			canonical := t.Format(time.RFC3339)
			row.ValueKind = "datetime"
			row.ValueDateTime = &canonical
			row.ValueNorm = normalizeOntologyFieldValue(canonical)
		}
	case "url":
		if _, err := url.ParseRequestURI(text); err == nil {
			row.ValueKind = "url"
		}
	case "id":
		row.ValueKind = "id"
	}
	if row.ValueKind == "" {
		row.ValueKind = "string"
	}
	return row, nil
}

func normalizeOntologyFieldValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "[[")
	value = strings.TrimSuffix(value, "]]")
	if idx := strings.Index(value, "|"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}

func normalizeOntologyLinkTarget(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[[")
	value = strings.TrimSuffix(value, "]]")
	if idx := strings.Index(value, "|"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}

func isISODate(value string) bool {
	if len(value) != len("2006-01-02") {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

// OntologyNodeID is the stable owner id used by the ontology node catalog and
// ontology-node semantic chunks.
func OntologyNodeID(ref NodeRef) string {
	return nodeCatalogHash(strings.Join([]string{
		ref.NotePath,
		string(ref.Kind),
		ref.TypeName,
		ref.NodeID,
		ref.Structural,
		ref.String(),
	}, "\x00"))
}

// NodeSourceLocator renders the canonical source locator used for indexed
// ontology-node lookup. Link rendering remains owned by NodeLinkService.
// Docs: [[ontology-indexed-read-model-contract#^spec-0040-source-locator]]
// keeps this as lookup/display identity, not the full graph/cache key.
func NodeSourceLocator(ref NodeRef) string {
	ref = normalizeCatalogNodeRef(ref)
	if ref.Kind == "" || ref.Kind == NodeKindNote {
		return ref.String()
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if fragment := normalizeSectionFragment(ref.NodeID, ref.NotePath); fragment != "" {
			return ref.NotePath + "#" + fragment
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

func nodeCatalogFragments(projection *NodeProjection) (string, string) {
	if projection == nil {
		return "", ""
	}
	fragment := strings.TrimSpace(projection.Ref.Fragment)
	if fragment == "" && projection.Snapshot != nil {
		if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
			fragment = strings.TrimSpace(sectionFragment(node))
		}
	}
	blockID := ""
	if strings.HasPrefix(fragment, "^") {
		blockID = strings.TrimPrefix(fragment, "^")
	}
	return fragment, blockID
}

func nodeCatalogDisplayLabel(projection *NodeProjection) string {
	if projection == nil {
		return ""
	}
	if projection.Ref.Kind == NodeKindNote {
		if projection.Snapshot == nil && projection.RootSnapshot != nil && strings.TrimSpace(projection.RootSnapshot.Title) != "" {
			return strings.TrimSpace(projection.RootSnapshot.Title)
		}
		return fileStemForCatalog(projection.Ref.NotePath)
	}
	if projection.Snapshot != nil {
		if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil && strings.TrimSpace(span.Title) != "" {
			return strings.TrimSpace(span.Title)
		}
		if node := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; node != nil && strings.TrimSpace(node.Title) != "" {
			return strings.TrimSpace(node.Title)
		}
	}
	return fileStemForCatalog(projection.Ref.NotePath)
}

func fileStemForCatalog(path string) string {
	segment := lastPathSegmentForCatalog(path)
	return strings.TrimSuffix(segment, filepath.Ext(segment))
}

func nodeCatalogLocatorStatus(ref NodeRef, blockID string) string {
	if ref.Kind == "" || ref.Kind == NodeKindNote {
		return string(NodeLocatorLinkable)
	}
	if ref.Kind == NodeKindEmbedded {
		if strings.TrimSpace(blockID) != "" {
			return string(NodeLocatorLinkable)
		}
		return string(NodeLocatorRequiresFix)
	}
	return string(NodeLocatorUnsupported)
}

func normalizeCatalogNodeRef(ref NodeRef) NodeRef {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.TypeName = strings.TrimSpace(ref.TypeName)
	ref.ParentID = strings.TrimSpace(ref.ParentID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	return ref
}

func schemaHash(schema *Schema) string {
	if schema == nil {
		return ""
	}
	return schema.Hash
}

func nodeCatalogHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func uniqueSortedNodeCatalogStrings(values []string) []string {
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

func lastPathSegmentForCatalog(path string) string {
	path = strings.Trim(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
