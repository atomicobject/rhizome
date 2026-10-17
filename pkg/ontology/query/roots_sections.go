package query

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func (e *executor) rootSection(ctx context.Context, ref ontology.NodeRef) (*sectionRecord, error) {
	trace := os.Getenv("RZM_TRACE_GRAPHQL_NODE") != ""
	start := time.Now()
	last := start
	traceStep := func(label string) {
		if !trace {
			return
		}
		now := time.Now()
		fmt.Fprintf(os.Stderr, "graphql rootSection trace %s step=%s delta=%s total=%s\n", ref.String(), label, now.Sub(last), now.Sub(start))
		last = now
	}
	note, err := e.rootNote(ctx, ref.NotePath)
	traceStep("root-note")
	if err != nil || note == nil {
		return nil, err
	}
	if strings.TrimSpace(ref.Structural) != "" && strings.TrimSpace(ref.Fragment) == "" && strings.TrimSpace(ref.NodeID) == "" {
		noteProjection, err := e.projectNoteRecord(ctx, note)
		traceStep("project-note")
		if err != nil {
			return nil, err
		}
		if noteProjection == nil {
			return nil, nil
		}
		projection, err := ontology.ProjectNodeFromSnapshot(noteProjection.Snapshot, e.schema, ref)
		traceStep("project-structural")
		if err != nil {
			return nil, err
		}
		if projection == nil {
			return nil, nil
		}
		record := e.sectionRecordFromProjection(note, projection)
		if record != nil && record.Node != nil {
			return record, nil
		}
		return nil, nil
	}
	sections, err := e.headingSectionRecordsForNote(ctx, note)
	traceStep("heading-sections")
	if err != nil {
		return nil, err
	}
	if section := e.matchRootSection(ref, sections); section != nil {
		traceStep("heading-match")
		return section, nil
	}
	sections, err = e.itemProjectionRecordsForNote(ctx, note)
	traceStep("item-sections")
	if err != nil {
		return nil, err
	}
	section := e.matchRootSection(ref, sections)
	traceStep("all-match")
	return section, nil
}

func (e *executor) matchRootSection(ref ontology.NodeRef, sections []*sectionRecord) *sectionRecord {
	targetFragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	for _, section := range sections {
		if section == nil || section.Node == nil {
			continue
		}
		node := section.Node
		if ref.NodeID != "" && node.ID != ref.NodeID {
			continue
		}
		if targetFragment != "" && !matchesRootSectionFragment(ref.NotePath, targetFragment, node) {
			continue
		}
		kind := ontology.NodeKindSection
		if noteType := e.schema.Types[section.TypeName]; noteType != nil && noteType.Role == ontoschema.TypeRoleEmbeddedNode {
			kind = ontology.NodeKindEmbedded
		}
		if ref.Kind != "" && ref.Kind != kind {
			continue
		}
		return section
	}
	return nil
}

func matchesRootSectionFragment(notePath, targetFragment string, node *ontology.SectionNode) bool {
	if node == nil {
		return false
	}
	if strings.HasPrefix(targetFragment, "^") {
		return strings.TrimPrefix(targetFragment, "^") == strings.TrimPrefix(strings.TrimSpace(node.BlockID), "^") && strings.TrimSpace(node.BlockID) != ""
	}
	return targetFragment == queryFragmentFromSectionID(notePath, node.ID)
}
