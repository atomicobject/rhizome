package ontology

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func (p *PreparedDocument) recordTitle(record *recordProjection) string {
	value, _ := p.value(record.selection.value)
	for _, candidate := range record.binding.TitleSelectors {
		title, ok := "", false
		switch candidate.Text {
		case "$key":
			title = record.identity.LogicalKey
			ok = title != ""
		case "$value":
			if value.Scalar != nil {
				title = sourceScalarText(*value.Scalar)
				ok = title != ""
			}
		default:
			title, ok = p.scalarAt(record, candidate)
		}
		if ok && strings.TrimSpace(title) != "" {
			return title
		}
	}
	return record.identity.LogicalKey
}

// ApplyIndexedRecordContext restores composition-dependent logical IDs for a
// captured indexed generation. The caller verifies provider/source freshness.
func (p *PreparedDocument) ApplyIndexedRecordContext(rows []readmodel.NodeRow) error {
	for _, row := range rows {
		if row.RecordJSON == "" || row.SchemaHash != p.schema.Hash || row.NotePath != p.root.NotePath.String() {
			continue
		}
		var ref NodeRef
		if err := json.Unmarshal([]byte(row.NodeRefJSON), &ref); err != nil {
			return fmt.Errorf("record_context_invalid: %w", err)
		}
		records := p.recordByID[ref.NodeID]
		if len(records) != 1 {
			continue
		}
		record := records[0]
		if !record.valid || record.projection.Ref.Fragment != ref.Fragment {
			continue
		}
		var context RecordIdentity
		if err := json.Unmarshal([]byte(row.RecordJSON), &context); err != nil {
			return fmt.Errorf("record_context_invalid: %w", err)
		}
		current := record.identity
		if context.Binding != current.Binding || context.Space != current.Space || context.LogicalKind != current.LogicalKind || context.Positional != current.Positional || context.LogicalKey == "" {
			return fmt.Errorf("record_context_invalid: binding identity differs")
		}
		record.identity = context
		record.projection.sourceTitle = p.recordTitle(record)
	}
	var candidates []recordReferenceCandidate
	for _, record := range p.records {
		if record.valid {
			candidates = append(candidates, recordReferenceCandidate{record, p})
		}
	}
	p.rebindRecordReferences(buildRecordReferenceIndex(candidates))
	return nil
}
