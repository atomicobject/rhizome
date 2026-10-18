package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strconv"
)

func nodeReadRootRefs(roots []NodeReadRoot) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(roots))
	for _, root := range roots {
		ref := NormalizeRef(root.Ref)
		if ref.IsZero() {
			continue
		}
		if root.TypeName != "" {
			ref.TypeName = root.TypeName
		}
		out = append(out, ref)
	}
	return out
}

func nodeViewsFromRecords(records []NodeRecord) []NodeView {
	out := make([]NodeView, len(records))
	for i, record := range records {
		out[i] = cloneNodeRecord(record)
	}
	return out
}

func (s *Scope) attachViewLocators(ctx context.Context, views []NodeView, refs []ontology.NodeRef) error {
	locators, err := s.Locators(ctx, refs)
	if err != nil {
		return err
	}
	for i := range views {
		if locator, ok := locators[nodereadLocatorKey(views[i].Ref)]; ok {
			views[i].NodeLocator = &locator
			views[i].LinkTarget = locator.LinkTarget
		}
	}
	return nil
}

func nodeReadDiagnosticsDelta(before, after Diagnostics) NodeReadDiagnostics {
	return NodeReadDiagnostics{
		RecordLoads:         after.RecordLoads - before.RecordLoads,
		RecordHits:          after.RecordHits - before.RecordHits,
		ContentLoads:        after.ContentLoads - before.ContentLoads,
		ContentHits:         after.ContentHits - before.ContentHits,
		TypeLoads:           after.TypeLoads - before.TypeLoads,
		TypeHits:            after.TypeHits - before.TypeHits,
		AssessmentLoads:     after.AssessmentLoads - before.AssessmentLoads,
		AssessmentHits:      after.AssessmentHits - before.AssessmentHits,
		EdgeLoads:           after.EdgeLoads - before.EdgeLoads,
		EdgeHits:            after.EdgeHits - before.EdgeHits,
		ProjectionFallbacks: after.ProjectionFallbacks - before.ProjectionFallbacks,
	}
}

func (d NodeReadDiagnostics) add(other NodeReadDiagnostics) NodeReadDiagnostics {
	d.RecordLoads += other.RecordLoads
	d.RecordHits += other.RecordHits
	d.ContentLoads += other.ContentLoads
	d.ContentHits += other.ContentHits
	d.TypeLoads += other.TypeLoads
	d.TypeHits += other.TypeHits
	d.AssessmentLoads += other.AssessmentLoads
	d.AssessmentHits += other.AssessmentHits
	d.EdgeLoads += other.EdgeLoads
	d.EdgeHits += other.EdgeHits
	d.ProjectionFallbacks += other.ProjectionFallbacks
	d.Truncations += other.Truncations
	return d
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func intString(v int) string {
	return strconv.Itoa(v)
}
