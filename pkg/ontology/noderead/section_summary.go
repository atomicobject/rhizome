package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

// hydrateCatalogFields adds fields absent from scalar metadata rows, using
// the existing batched catalog seam without source reads.
func (s *Scope) hydrateCatalogFields(ctx context.Context, records []NodeRecord) error {
	if s.service.Schema == nil {
		return nil
	}
	selected := make([]NodeRecord, 0)
	fields := make(map[string]map[string]bool)
	for _, record := range records {
		noteType := s.service.Schema.Types[record.TypeName]
		if noteType == nil {
			continue
		}
		names := map[string]bool{}
		for _, field := range noteType.Fields {
			if field.SourcePath != "" || ontoschema.IsSectionSummary(field) {
				names[strings.ToLower(field.Name)] = field.SourcePath != ""
			}
		}
		if len(names) > 0 {
			selected = append(selected, record)
			fields[record.Path] = names
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
		names, ok := fields[record.Path]
		if !ok {
			continue
		}
		for name := range names {
			delete(records[i].InlineProps, name)
		}
		for _, row := range indexed[RefIdentityKey(record.Ref)] {
			nested, selected := names[row.FieldName]
			if !selected {
				continue
			}
			if nested {
				if records[i].FieldValues == nil {
					records[i].FieldValues = map[string][]readmodel.FieldValueRow{}
				}
				records[i].FieldValues[row.FieldName] = append(records[i].FieldValues[row.FieldName], row)
			} else {
				records[i].InlineProps[row.FieldName] = append(records[i].InlineProps[row.FieldName], row.ValueText)
			}
		}
	}
	return nil
}
