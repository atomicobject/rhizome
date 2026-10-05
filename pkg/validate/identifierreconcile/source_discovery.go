package identifierreconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type identifierRepairSource struct {
	vaultDef    obsidian.VaultDefinition
	schema      *ontology.Schema
	sources     []notemeta.NoteSourceSnapshot
	projections []*ontology.NodeProjection
}

func discoverIdentifierRepairSource(ctx context.Context, vaultDef obsidian.VaultDefinition, roots []reference.IdentifierRewrite) (*identifierRepairSource, []reference.IdentifierRewrite, error) {
	basePath := strings.TrimSpace(vaultDef.BasePath())
	if basePath == "" {
		return nil, nil, fmt.Errorf("identifier discovery requires a vault root")
	}
	schema, err := ontology.LoadSchema(basePath)
	if err != nil {
		return nil, nil, fmt.Errorf("load compiled ontology schema: %w", err)
	}
	if schema == nil || strings.TrimSpace(schema.Hash) == "" {
		return nil, nil, fmt.Errorf("load compiled ontology schema: compiled schema is unavailable")
	}
	canonicalRoots, err := canonicalRepairRewriteSet(roots)
	if err != nil {
		return nil, nil, err
	}
	for _, rewrite := range canonicalRoots {
		if !rewrite.DerivedFrom.IsZero() {
			return nil, nil, fmt.Errorf("identifier discovery accepts root rewrites only; descendants are derived from source")
		}
	}
	sources, err := notemeta.BuildNoteSourceFacts(ctx, vaultDef)
	if err != nil {
		return nil, nil, fmt.Errorf("build complete note source inventory: %w", err)
	}
	projections := make([]*ontology.NodeProjection, 0, len(sources))
	rootTypesByPath := make(map[string]string)
	for _, rewrite := range canonicalRoots {
		if rewrite.OldRef.Kind != ontology.NodeKindNote {
			continue
		}
		if existing := rootTypesByPath[rewrite.OldRef.NotePath]; existing != "" && existing != rewrite.OldRef.TypeName {
			return nil, nil, fmt.Errorf("identifier roots assign multiple types to %s", rewrite.OldRef.NotePath)
		}
		rootTypesByPath[rewrite.OldRef.NotePath] = rewrite.OldRef.TypeName
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if source.Format != noteformat.FormatID("markdown") {
			continue
		}
		snapshot, buildErr := ontology.BuildDocumentSnapshot(source.Path.String(), source.Content, time.Unix(source.Mtime, 0))
		if buildErr != nil {
			return nil, nil, fmt.Errorf("project identifier source %s: %w", source.Path, buildErr)
		}
		nodes, projectErr := ontology.ProjectDocumentNodesFromSnapshot(snapshot, schema)
		if projectErr != nil {
			return nil, nil, fmt.Errorf("project identifier source %s: %w", source.Path, projectErr)
		}
		if typeName := rootTypesByPath[source.Path.String()]; typeName != "" && (len(nodes) == 0 || nodes[0].Type == nil) {
			nodes, projectErr = ontology.ProjectDocumentNodesFromSnapshotAsSelectorType(snapshot, schema, typeName)
			if projectErr != nil {
				return nil, nil, fmt.Errorf("project identifier migration source %s: %w", source.Path, projectErr)
			}
		}
		projections = append(projections, nodes...)
	}
	rewrites, err := deriveCompleteIdentifierRewrites(canonicalRoots, projections)
	if err != nil {
		return nil, nil, err
	}
	return &identifierRepairSource{vaultDef: vaultDef, schema: schema, sources: sources, projections: projections}, rewrites, nil
}

func deriveCompleteIdentifierRewrites(roots []reference.IdentifierRewrite, projections []*ontology.NodeProjection) ([]reference.IdentifierRewrite, error) {
	rewrites := cloneIdentifierRewrites(roots)
	aliasFieldsByType := identifierAliasFieldsByType(projections, roots)
	byRef := make(map[string]reference.IdentifierRewrite, len(rewrites))
	for _, rewrite := range rewrites {
		if rewrite.Mode == reference.IdentifierRewritePreferredRekey {
			byRef[repairRefKey(rewrite.OldRef)] = rewrite
		}
	}
	byNodeID := make(map[string]*ontology.NodeProjection, len(projections))
	for _, projection := range projections {
		if projection != nil && strings.TrimSpace(projection.Ref.NodeID) != "" {
			byNodeID[projection.Ref.NotePath+"\x00"+projection.Ref.NodeID] = projection
		}
	}

	for iteration := 0; ; iteration++ {
		if iteration > len(projections)+1 {
			return nil, fmt.Errorf("derived identifier discovery did not converge after %d projections (%d rewrites)", len(projections), len(rewrites))
		}
		changed := false
		for _, projection := range projections {
			field, binding, oldIdentifier, ok := projectedPreferredIdentifier(projection)
			locatorOnly := false
			if !ok {
				oldIdentifier, _, locatorOnly = parserOwnedBlockLocator(projection)
				if locatorOnly {
					field = &ontology.Field{Name: reference.StructuredFieldBlockLocator, IsDerivableIdentifier: true, DerivedSuffix: "locator"}
				}
			}
			if !ok && !locatorOnly || !field.IsDerivableIdentifier || field.DerivedSuffix == "" {
				continue
			}
			oldRef := semanticRepairRef(projection.Ref)
			if _, exists := byRef[repairRefKey(oldRef)]; exists {
				continue
			}
			parent, found := nearestIdentifierRewrite(projection, oldIdentifier, byNodeID, byRef, rewrites)
			if !found {
				if locatorOnly {
					continue
				}
				if hasPreferredNoteRootRewrite(rewrites, projection.Ref.NotePath) {
					return nil, fmt.Errorf("derived identifier %s in %s has no structurally valid rewritten ancestor", oldIdentifier, oldRef.NotePath)
				}
				continue
			}
			newIdentifier, replaced := replaceBoundedRepairIdentifier(oldIdentifier, parent.OldIdentifier, parent.NewIdentifier)
			if !replaced {
				if locatorOnly {
					continue
				}
				return nil, fmt.Errorf("derived identifier %s in %s does not preserve the %s suffix below its rewritten ancestor", oldIdentifier, oldRef.NotePath, field.DerivedSuffix)
			}
			if !locatorOnly && !derivedFieldSuffixMatches(oldIdentifier, parent.OldIdentifier, field.DerivedSuffix) {
				return nil, fmt.Errorf("derived identifier %s in %s does not preserve the %s suffix below its rewritten ancestor", oldIdentifier, oldRef.NotePath, field.DerivedSuffix)
			}
			newRef := derivedRepairRef(oldRef, parent, oldIdentifier, newIdentifier)
			rewrite := reference.IdentifierRewrite{
				Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef,
				OldIdentifier: oldIdentifier, NewIdentifier: newIdentifier, PreferredField: field.Name,
				AliasesField: parent.AliasesField, DerivedFrom: parent.OldRef,
			}
			for _, aliasField := range aliasFieldsByType[oldRef.TypeName] {
				if aliasField != rewrite.AliasesField {
					rewrite.AdditionalAliasesFields = append(rewrite.AdditionalAliasesFields, aliasField)
				}
			}
			if !locatorOnly && !binding.Derived {
				valueRange := ontology.ByteRange{}
				if len(binding.ValueRanges) == 1 {
					valueRange = binding.ValueRanges[0]
				}
				if _, exact := exactProjectedFieldValue(projection.Snapshot.Content, field, binding, 0, oldIdentifier, valueRange); !exact || len(binding.ValueRanges) != len(binding.Values) {
					return nil, fmt.Errorf("derived identifier %s in %s is authored but lacks an exact source range", oldIdentifier, oldRef.NotePath)
				}
			}
			byRef[repairRefKey(oldRef)] = rewrite
			rewrites = append(rewrites, rewrite)
			changed = true
		}
		if !changed {
			break
		}
	}
	return canonicalRepairRewriteSet(rewrites)
}

func parserOwnedBlockLocator(projection *ontology.NodeProjection) (string, ontology.ByteRange, bool) {
	if projection == nil || projection.Snapshot == nil || projection.Ref.Kind != ontology.NodeKindEmbedded || !strings.HasPrefix(strings.TrimSpace(projection.Ref.Fragment), "^") {
		return "", ontology.ByteRange{}, false
	}
	span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]
	if span == nil || span.BlockID == "" || !span.Range.Valid(len(projection.Snapshot.Content)) {
		return "", ontology.ByteRange{}, false
	}
	identifier := strings.TrimPrefix(strings.TrimSpace(projection.Ref.Fragment), "^")
	if identifier != span.BlockID {
		return "", ontology.ByteRange{}, false
	}
	lineEnd := strings.IndexByte(projection.Snapshot.Content[span.Range.Start:span.Range.End], '\n')
	if lineEnd < 0 {
		lineEnd = span.Range.End - span.Range.Start
	}
	line := strings.TrimRight(projection.Snapshot.Content[span.Range.Start:span.Range.Start+lineEnd], " \t\r")
	expected := "^" + span.BlockID
	if !strings.HasSuffix(line, expected) {
		return "", ontology.ByteRange{}, false
	}
	start := span.Range.Start + len(line) - len(expected)
	return identifier, ontology.ByteRange{Start: start, End: start + len(expected)}, true
}

func hasPreferredNoteRootRewrite(rewrites []reference.IdentifierRewrite, notePath string) bool {
	for _, rewrite := range rewrites {
		if rewrite.Mode == reference.IdentifierRewritePreferredRekey && rewrite.DerivedFrom.IsZero() && rewrite.OldRef.NotePath == notePath && rewrite.OldRef.Kind == ontology.NodeKindNote {
			return true
		}
	}
	return false
}

func projectedPreferredIdentifier(projection *ontology.NodeProjection) (*ontology.Field, ontology.FieldBinding, string, bool) {
	if projection == nil || projection.Type == nil {
		return nil, ontology.FieldBinding{}, "", false
	}
	for _, field := range projection.Type.Fields {
		if field == nil || !field.IsPreferredIdentifier {
			continue
		}
		binding, found := projection.Fields[field.Name]
		if !found || len(binding.Values) != 1 || strings.TrimSpace(binding.Values[0]) == "" {
			return nil, ontology.FieldBinding{}, "", false
		}
		return field, binding, strings.TrimSpace(binding.Values[0]), true
	}
	return nil, ontology.FieldBinding{}, "", false
}

func nearestIdentifierRewrite(projection *ontology.NodeProjection, oldIdentifier string, byNodeID map[string]*ontology.NodeProjection, byRef map[string]reference.IdentifierRewrite, rewrites []reference.IdentifierRewrite) (reference.IdentifierRewrite, bool) {
	visited := make(map[string]struct{})
	for parentID := strings.TrimSpace(projection.Ref.ParentID); parentID != ""; {
		if _, cycle := visited[parentID]; cycle {
			break
		}
		visited[parentID] = struct{}{}
		parentProjection := byNodeID[projection.Ref.NotePath+"\x00"+parentID]
		if parentProjection == nil {
			break
		}
		if rewrite, found := byRef[repairRefKey(semanticRepairRef(parentProjection.Ref))]; found {
			return rewrite, true
		}
		parentID = strings.TrimSpace(parentProjection.Ref.ParentID)
	}
	var candidates []reference.IdentifierRewrite
	for _, rewrite := range rewrites {
		if rewrite.Mode != reference.IdentifierRewritePreferredRekey || rewrite.OldRef.NotePath != projection.Ref.NotePath {
			continue
		}
		if _, ok := derivedSuffix(oldIdentifier, rewrite.OldIdentifier); ok {
			candidates = append(candidates, rewrite)
		}
	}
	if len(candidates) == 0 {
		return reference.IdentifierRewrite{}, false
	}
	for i := 1; i < len(candidates); i++ {
		if len(candidates[i].OldIdentifier) > len(candidates[0].OldIdentifier) {
			candidates[0], candidates[i] = candidates[i], candidates[0]
		}
	}
	return candidates[0], true
}

func derivedFieldSuffixMatches(child, parent, suffix string) bool {
	remainder, ok := derivedSuffix(child, parent)
	if !ok {
		return false
	}
	remainder = strings.TrimLeft(remainder, "-_./:")
	return strings.HasPrefix(IdentifierComparisonKey(remainder), IdentifierComparisonKey(suffix))
}

func semanticRepairRef(ref ontology.NodeRef) ontology.NodeRef {
	return ontology.NodeRef{
		NotePath: strings.TrimSpace(ref.NotePath), Fragment: strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#"),
		TypeName: strings.TrimSpace(ref.TypeName), Kind: ref.Kind,
	}
}

func derivedRepairRef(oldRef ontology.NodeRef, parent reference.IdentifierRewrite, oldIdentifier, newIdentifier string) ontology.NodeRef {
	newRef := oldRef
	newRef.NotePath = parent.NewRef.NotePath
	if replacement, changed := replaceBoundedRepairIdentifier(oldRef.Fragment, oldIdentifier, newIdentifier); changed {
		newRef.Fragment = replacement
	}
	return newRef
}
