package validate

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

type identifierBlockIDMigration struct {
	notePath   string
	nodeID     string
	structural string
	oldID      string
	newID      string
	rawValue   string
}

// RunIdentifierBlockIDMigration finds embedded nodes whose semantic identifier
// can become the canonical block-safe anchor. It is wired into ontology
// validation so the migration is visible and fixable without restoring the old
// eager "every embedded node needs a generated anchor" policy.
func RunIdentifierBlockIDMigration(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime) ([]Issue, []FixAction, error) {
	if runtime == nil || runtime.Schema == nil {
		return nil, nil, nil
	}
	sources, err := markdownValidationSources(ctx, runCtx)
	if err != nil {
		return nil, nil, err
	}
	allNotes := make([]string, 0, len(sources))
	for _, source := range sources {
		allNotes = append(allNotes, source.Path.String())
	}
	contentByPath := markdownSourceContentByPath(sources)
	getContent := func(notePath string) string {
		if c, ok := contentByPath[notePath]; ok {
			return c
		}
		return ""
	}

	migrations := make([]identifierBlockIDMigration, 0)
	for _, notePath := range allNotes {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if !runCtx.inPostcheckScope(notePath) {
			continue
		}
		content := getContent(notePath)
		if strings.TrimSpace(content) == "" {
			continue
		}
		snapshot, err := ontology.BuildDocumentSnapshot(notePath, content, time.Time{})
		if err != nil || snapshot == nil {
			continue
		}
		var walk func([]*ontology.SectionNode)
		walk = func(nodes []*ontology.SectionNode) {
			for _, node := range nodes {
				if ctx.Err() != nil {
					return
				}
				if node == nil {
					continue
				}
				ref := ontology.NodeRef{NotePath: notePath, NodeID: node.ID, Kind: ontology.NodeKindEmbedded}
				projection, err := ontology.ProjectBoundNodeFromSnapshot(snapshot, runtime.Schema, ref)
				if err != nil || projection == nil || projection.Ref.Kind != ontology.NodeKindEmbedded {
					walk(node.Children)
					continue
				}
				rawValue := firstIdentifierValue(projection)
				if rawValue == "" {
					walk(node.Children)
					continue
				}
				canonical := ontology.BlockSafeIdentifier(rawValue)
				if canonical == "" || canonical == "node" {
					walk(node.Children)
					continue
				}
				current := strings.TrimSpace(strings.TrimPrefix(node.BlockID, "^"))
				fieldName := identifierFieldName(projection)
				hasIdentifierBlockID := projectionHasIdentifierBlockID(projection, fieldName, canonical) || sectionHasIdentifierBlockID(node, fieldName, canonical)
				if hasIdentifierBlockID && current == "" {
					walk(node.Children)
					continue
				}
				if current == canonical && hasIdentifierBlockID {
					walk(node.Children)
					continue
				}
				migrations = append(migrations, identifierBlockIDMigration{
					notePath:   notePath,
					nodeID:     node.ID,
					structural: projection.Ref.Structural,
					oldID:      current,
					newID:      canonical,
					rawValue:   rawValue,
				})
				walk(node.Children)
			}
		}
		walk(snapshot.Sections)
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	sort.SliceStable(migrations, func(i, j int) bool {
		if migrations[i].notePath != migrations[j].notePath {
			return migrations[i].notePath < migrations[j].notePath
		}
		return migrations[i].nodeID < migrations[j].nodeID
	})

	issues := make([]Issue, 0, len(migrations))
	fixes := make([]FixAction, 0, len(migrations))
	var referenceInventory *validationSourceLinks
	for _, m := range migrations {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		issues = append(issues, Issue{
			Code:    "identifier_block_id_migration",
			Path:    m.notePath,
			Source:  m.oldID,
			Target:  m.newID,
			Message: fmt.Sprintf("embedded identifier %q should be authored as block-safe id:: ^%s", m.rawValue, m.newID),
		})
		edits := []FixEdit{}
		if m.oldID != "" && m.oldID != m.newID {
			if referenceInventory == nil {
				fullCtx := runCtx
				if len(runCtx.postcheckPaths) > 0 {
					fullCtx.postcheckPaths = nil
					fullCtx.sourceSnapshot = nil
					if reader, ok := fullCtx.NoteReader.(*validationSnapshotNoteReader); ok {
						fullCtx.NoteReader = reader.base
					}
				}
				var err error
				referenceInventory, err = validationLinksFor(ctx, fullCtx)
				if err != nil {
					return nil, nil, fmt.Errorf("load identifier block-id references: %w", err)
				}
			}
			edits = append(edits, blockIDReferenceEdits(referenceInventory, m.notePath, m.oldID, m.newID)...)
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
		}
		edits = append(edits, FixEdit{
			Kind:       FixKindEnsureBlockID,
			NotePath:   m.notePath,
			NodeID:     m.nodeID,
			Structural: m.structural,
			BlockID:    m.newID,
		})
		if m.oldID != "" && m.oldID != m.newID {
			edits = append(edits, FixEdit{
				Kind:       FixKindRemoveBlockID,
				NotePath:   m.notePath,
				NodeID:     m.nodeID,
				Structural: m.structural,
				BlockID:    m.oldID,
			})
		}
		fixes = append(fixes, FixAction{
			ID:            fmt.Sprintf("identifier-block-id:%s:%s", m.notePath, m.newID),
			Check:         CheckOntology,
			IssueCode:     "identifier_block_id_migration",
			Kind:          FixKindEnsureBlockID,
			Safety:        FixSafetySafe,
			Title:         fmt.Sprintf("Make identifier block-safe in %s", m.notePath),
			Summary:       fmt.Sprintf("rewrite identifier to id:: ^%s and update stale block links", m.newID),
			InstanceCount: 1,
			AffectedPaths: []string{m.notePath},
			Edits:         edits,
		})
	}
	return issues, fixes, nil
}

// firstIdentifierValue returns the first authored identifier value on the
// projection. Derived values (synthesized by the projection layer for
// derivable-identity embedded fields per SPEC-0023.US8) are skipped — the
// migration check should not flag a derived id as "needs to be authored as
// block-safe" because there's no authored line to migrate.
func firstIdentifierValue(projection *ontology.NodeProjection) string {
	if projection == nil {
		return ""
	}
	if projection.Type == nil {
		return ""
	}
	for _, field := range projection.Type.Fields {
		if field == nil || !field.IsPreferredIdentifier {
			continue
		}
		binding, ok := projection.Fields[field.Name]
		if ok && len(binding.Values) > 0 && !binding.Derived {
			return strings.TrimSpace(binding.Values[0])
		}
	}
	for _, field := range projection.Type.Fields {
		if field == nil || !field.IsIdentifier {
			continue
		}
		binding, ok := projection.Fields[field.Name]
		if ok && len(binding.Values) > 0 && !binding.Derived {
			return strings.TrimSpace(binding.Values[0])
		}
	}
	return ""
}

func identifierFieldName(projection *ontology.NodeProjection) string {
	return identifierFieldNameOf(preferredIdentifierFieldFor(projection))
}

// preferredIdentifierFieldFor returns the schema field that owns the
// projection's preferred identifier (or any identifier as a fallback). Callers
// that need the per-field metadata (`IsDerivableIdentifier`, `DerivedSuffix`)
// should consult this helper rather than re-walking `projection.Type.Fields`.
func preferredIdentifierFieldFor(projection *ontology.NodeProjection) *ontoschema.Field {
	if projection == nil || projection.Type == nil {
		return nil
	}
	if field := projection.Type.PreferredIdentifierField(); field != nil {
		return field
	}
	for _, field := range projection.Type.Fields {
		if field != nil && field.IsIdentifier {
			return field
		}
	}
	return nil
}

func identifierFieldNameOf(field *ontoschema.Field) string {
	if field == nil {
		return ""
	}
	return field.Name
}

func sectionHasIdentifierBlockID(node *ontology.SectionNode, fieldName, blockID string) bool {
	if node == nil || strings.TrimSpace(fieldName) == "" {
		return false
	}
	target := "^" + strings.TrimSpace(strings.TrimPrefix(blockID, "^"))
	for _, line := range strings.Split(ontology.SectionOwnContent(node), "\n") {
		trimmed := strings.TrimSpace(line)
		if itemStart, ok := unorderedListItemContentStart(trimmed); ok {
			trimmed = strings.TrimSpace(trimmed[itemStart:])
		}
		idx := strings.Index(trimmed, "::")
		if idx <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(trimmed[:idx]), fieldName) && strings.TrimSpace(trimmed[idx+2:]) == target {
			return true
		}
	}
	return false
}

func projectionHasIdentifierBlockID(projection *ontology.NodeProjection, fieldName, blockID string) bool {
	if projection == nil || strings.TrimSpace(fieldName) == "" {
		return false
	}
	binding, ok := projection.Fields[fieldName]
	if !ok {
		return false
	}
	target := "^" + strings.TrimSpace(strings.TrimPrefix(blockID, "^"))
	for _, span := range binding.InlineSpans {
		if strings.TrimSpace(span.Value) == target {
			return true
		}
	}
	return false
}

func unorderedListItemContentStart(trimmed string) (int, bool) {
	if len(trimmed) < 2 || trimmed[1] != ' ' && trimmed[1] != '\t' {
		return 0, false
	}
	switch trimmed[0] {
	case '-', '*', '+':
		return 2, true
	default:
		return 0, false
	}
}

func blockIDReferenceEdits(inventory *validationSourceLinks, targetNote, oldID, newID string) []FixEdit {
	oldFragment := "^" + strings.TrimPrefix(oldID, "^")
	newFragment := "^" + strings.TrimPrefix(newID, "^")
	var edits []FixEdit
	seen := map[string]bool{}
	for _, link := range inventory.links {
		if !link.Exists || link.Target != targetNote || link.Fact.Fragment != oldFragment || seen[link.Source] || !validationBlockFragmentAuthored(inventory.sources[link.Source], link.Fact) {
			continue
		}
		seen[link.Source] = true
		edits = append(edits, FixEdit{Kind: FixKindRewriteLinkGroup, NotePath: link.Source, OldTarget: targetNote + "#" + oldFragment, NewTarget: targetNote + "#" + newFragment})
	}
	return edits
}
