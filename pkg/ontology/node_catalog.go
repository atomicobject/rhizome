package ontology

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BuildIntelOntologyNodes projects a typed note into persisted ontology-node
// catalog rows. The catalog is intentionally independent from semantic
// embeddings so locator resolution works even when embeddings are disabled.
// Docs: [[ontology-indexed-read-model-contract]]
// and [[ontology-indexed-read-model-contract#^spec-0040-noderef-json]] define
// this catalog as the indexed source of NodeRefJSON/source_locator identity.
func BuildIntelOntologyNodes(schema *ontoschema.Schema, projection *NodeProjection, now int64) ([]string, []readmodel.NodeRow, error) {
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
	ResolveOntologyLinkTarget(field *ontoschema.Field, targetInput string) (OntologyLinkTarget, bool)
}

type OntologyLinkTargetResolverFunc func(field *ontoschema.Field, targetInput string) (OntologyLinkTarget, bool)

func (fn OntologyLinkTargetResolverFunc) ResolveOntologyLinkTarget(field *ontoschema.Field, targetInput string) (OntologyLinkTarget, bool) {
	if fn == nil {
		return OntologyLinkTarget{}, false
	}
	return fn(field, targetInput)
}

type BuildIntelOntologyNodeReadModelOptions struct {
	LinkResolver OntologyLinkTargetResolver
}

func BuildIntelOntologyNodeReadModel(schema *ontoschema.Schema, projection *NodeProjection, now int64) (readmodel.NodeReadModel, error) {
	return BuildIntelOntologyNodeReadModelWithOptions(schema, projection, now, BuildIntelOntologyNodeReadModelOptions{})
}

func BuildIntelOntologyNodeReadModelWithOptions(schema *ontoschema.Schema, projection *NodeProjection, now int64, opts BuildIntelOntologyNodeReadModelOptions) (readmodel.NodeReadModel, error) {
	return buildIntelOntologyNodeReadModel(schema, projection, nil, now, opts)
}

func buildIntelOntologyNodeReadModel(schema *ontoschema.Schema, projection *NodeProjection, resolver *projectionResolver, now int64, opts BuildIntelOntologyNodeReadModelOptions) (readmodel.NodeReadModel, error) {
	if now == 0 {
		now = time.Now().Unix()
	}
	if projection == nil || (projection.Snapshot == nil && projection.RootSnapshot == nil) || schema == nil {
		return readmodel.NodeReadModel{}, nil
	}
	if projection.Snapshot == nil {
		rootProjection := projection
		if strings.TrimSpace(rootProjection.ResolvedType) == "" {
			if fallback, ok := AsFallbackNoteProjection(rootProjection); ok {
				rootProjection = fallback
			}
		}
		if strings.TrimSpace(rootProjection.ResolvedType) == "" {
			return readmodel.NodeReadModel{}, nil
		}
		node, err := IntelOntologyNodeForProjection(schema, rootProjection, nil, now)
		if err != nil {
			return readmodel.NodeReadModel{}, err
		}
		fields, dependencies := IntelOntologyNodeFieldReadModelForProjection(schema, rootProjection, node, now, opts)
		return readmodel.NodeReadModel{
			NotePaths:        []string{rootProjection.Ref.NotePath},
			Nodes:            []readmodel.NodeRow{node},
			FieldValues:      fields,
			LinkDependencies: dependencies,
		}, nil
	}
	if resolver == nil {
		var err error
		resolver, err = newProjectionResolver(projection.Snapshot, schema)
		if err != nil {
			return readmodel.NodeReadModel{}, err
		}
	}
	var nodes []readmodel.NodeRow
	var fields []readmodel.FieldValueRow
	var dependencies []readmodel.LinkDependencyRow
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
			return readmodel.NodeReadModel{}, err
		}
		if IsFallbackNoteProjection(rootProjection) {
			for _, section := range FallbackSectionProjections(rootProjection) {
				if err := collectIntelOntologyNodeReadModel(resolver, section, rootProjection, now, opts, &nodes, &fields, &dependencies, seen); err != nil {
					return readmodel.NodeReadModel{}, err
				}
			}
		}
	}
	globalRefs := resolver.globalSourceNodeRefs()
	for _, ref := range globalRefs {
		child, err := resolver.projectCurrent(ref)
		if err != nil {
			return readmodel.NodeReadModel{}, err
		}
		if err := collectIntelOntologyNodeReadModel(resolver, child, rootProjection, now, opts, &nodes, &fields, &dependencies, seen); err != nil {
			return readmodel.NodeReadModel{}, err
		}
	}
	paths := make([]string, 0, len(nodes))
	for _, node := range nodes {
		paths = append(paths, node.NotePath)
	}
	return readmodel.NodeReadModel{
		NotePaths:        uniqueSortedNodeCatalogStrings(paths),
		Nodes:            nodes,
		FieldValues:      fields,
		LinkDependencies: dependencies,
	}, nil
}

func collectIntelOntologyNodeReadModel(resolver *projectionResolver, projection *NodeProjection, parent *NodeProjection, now int64, opts BuildIntelOntologyNodeReadModelOptions, out *[]readmodel.NodeRow, fieldOut *[]readmodel.FieldValueRow, dependencyOut *[]readmodel.LinkDependencyRow, seen map[string]struct{}) error {
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
	case ontoschema.TypeRoleNote, ontoschema.TypeRoleEmbeddedNode:
		return projection
	default:
		return parent
	}
}

// IntelOntologyNodeForProjection creates the persisted catalog row for one projection.
func IntelOntologyNodeForProjection(schema *ontoschema.Schema, projection *NodeProjection, parent *NodeProjection, now int64) (readmodel.NodeRow, error) {
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
		return readmodel.NodeRow{}, err
	}
	parentID := ""
	parentType := ""
	if parent != nil {
		parentID = OntologyNodeID(parent.Ref)
		parentType = strings.TrimSpace(parent.ResolvedType)
	}
	sourceLocator := NodeSourceLocator(ref)
	displayLabel := nodeCatalogDisplayLabel(projection)
	return readmodel.NodeRow{
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
		RelocationFingerprint: projection.relocationFingerprint,
		SchemaHash:            schemaHash(schema),
		UpdatedAt:             now,
	}, nil
}

func IntelOntologyNodeFieldValuesForProjection(schema *ontoschema.Schema, projection *NodeProjection, node readmodel.NodeRow, now int64) []readmodel.FieldValueRow {
	rows, _ := IntelOntologyNodeFieldReadModelForProjection(schema, projection, node, now, BuildIntelOntologyNodeReadModelOptions{})
	return rows
}

func IntelOntologyNodeFieldReadModelForProjection(schema *ontoschema.Schema, projection *NodeProjection, node readmodel.NodeRow, now int64, opts BuildIntelOntologyNodeReadModelOptions) ([]readmodel.FieldValueRow, []readmodel.LinkDependencyRow) {
	if projection == nil || projection.Type == nil || strings.TrimSpace(node.NodeID) == "" {
		return nil, nil
	}
	var out []readmodel.FieldValueRow
	var dependencies []readmodel.LinkDependencyRow
	for _, field := range projection.Type.Fields {
		if field == nil {
			continue
		}
		switch field.Kind {
		case ontoschema.FieldKindScalar, ontoschema.FieldKindEnum, ontoschema.FieldKindLink:
		case ontoschema.FieldKindSection:
			if !ontoschema.IsSectionSummary(field) {
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
		if ontoschema.IsSectionSummary(field) {
			values = SectionSummaryValues(projection, field)
		}
		if len(values) == 0 && binding.Present {
			values = []string{""}
		}
		for i, value := range values {
			// Empty-binding Link fields would emit a row with empty value_norm and
			// no target identity, which leaves a stale "present but unresolved"
			// signal in the index. Skip the row instead so reads stay quiet.
			if field.Kind == ontoschema.FieldKindLink && strings.TrimSpace(value) == "" {
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

func ontologyNodeFieldValueForValue(schema *ontoschema.Schema, node readmodel.NodeRow, field *ontoschema.Field, binding FieldBinding, value string, ordinal int, now int64) readmodel.FieldValueRow {
	row, _ := ontologyNodeFieldValueForValueWithOptions(schema, node, field, binding, value, ordinal, now, BuildIntelOntologyNodeReadModelOptions{})
	return row
}

func ontologyNodeFieldValueForValueWithOptions(schema *ontoschema.Schema, node readmodel.NodeRow, field *ontoschema.Field, binding FieldBinding, value string, ordinal int, now int64, opts BuildIntelOntologyNodeReadModelOptions) (readmodel.FieldValueRow, *readmodel.LinkDependencyRow) {
	row := FieldValueRow(field, value)
	row.NodeID = node.NodeID
	row.NotePath = node.NotePath
	row.TypeName = node.TypeName
	row.FieldName = strings.ToLower(strings.TrimSpace(field.Name))
	row.FieldKind = string(field.Kind)
	row.SourceKind = string(binding.SourceKind)
	row.ListOrdinal = ordinal
	row.SchemaHash = schemaHash(schema)
	row.UpdatedAt = now
	if field.Kind == ontoschema.FieldKindLink {
		targetInput := row.TargetNotePath
		row.TargetNotePath = ""
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
		dependency := &readmodel.LinkDependencyRow{
			SourceNotePath:         node.NotePath,
			NodeID:                 node.NodeID,
			TypeName:               node.TypeName,
			FieldName:              row.FieldName,
			TargetInput:            targetInput,
			TargetInputNorm:        readmodel.LinkDependencyKey(targetInput),
			ResolvedTargetNotePath: row.TargetNotePath,
			ResolvedTargetTypeName: row.TargetTypeName,
			SchemaHash:             schemaHash(schema),
			UpdatedAt:              now,
		}
		return row, dependency
	}
	return row, nil
}

func normalizeOntologyFieldValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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

func schemaHash(schema *ontoschema.Schema) string {
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
