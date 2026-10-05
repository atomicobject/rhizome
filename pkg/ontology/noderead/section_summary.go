package noderead

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// hydrateSectionSummaries adds only the declared display summary to metadata
// roots, using the existing batched catalog seam and no source reads.
func (s *Scope) hydrateSectionSummaries(ctx context.Context, records []NodeRecord) error {
	if s.service.Schema == nil {
		return nil
	}
	selected := make([]NodeRecord, 0)
	fields := make(map[string]string)
	for _, record := range records {
		noteType := s.service.Schema.Types[record.TypeName]
		if noteType == nil {
			continue
		}
		field := ontology.SummaryField(noteType.Fields)
		if ontology.IsSectionSummary(field) {
			selected = append(selected, record)
			fields[record.Path] = strings.ToLower(field.Name)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	indexed, err := s.IndexedRecordFieldValues(ctx, selected)
	if err != nil {
		return err
	}
	for i, record := range records {
		name, ok := fields[record.Path]
		if !ok {
			continue
		}
		delete(records[i].InlineProps, name)
		for _, row := range indexed[RefIdentityKey(record.Ref)] {
			if row.FieldName == name {
				records[i].InlineProps[name] = append(records[i].InlineProps[name], row.ValueText)
			}
		}
	}
	return nil
}
