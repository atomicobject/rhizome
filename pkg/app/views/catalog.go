package views

import (
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

func catalogEntries(defs []viewconfig.ViewDefinition, issues []viewconfig.Issue, includeHidden bool) []CatalogEntry {
	byView := map[string][]viewconfig.Issue{}
	byPath := map[string][]viewconfig.Issue{}
	for _, issue := range issues {
		if issue.View != "" {
			byView[issue.View] = append(byView[issue.View], issue)
		} else {
			byPath[issue.Path] = append(byPath[issue.Path], issue)
		}
	}
	out := make([]CatalogEntry, 0, len(defs))
	for _, def := range defs {
		if def.Mount.Hidden && !includeHidden {
			continue
		}
		if _, err := json.Marshal(def.Configuration); err != nil {
			def.Configuration = nil
		}
		entryIssues := byView[def.ID]
		if def.ID == "" {
			entryIssues = append(entryIssues, byPath[def.Source.Path]...)
		}
		if def.Generated {
			entryIssues = nil
		}
		out = append(out, CatalogEntry{
			Configuration:     def.Configuration,
			ID:                def.ID,
			Name:              def.Name,
			Description:       def.Description,
			Generated:         def.Generated,
			Origin:            def.Origin,
			Source:            def.SourceSpec,
			Mount:             def.Mount,
			Defaults:          def.Defaults,
			Variants:          def.Variants,
			AvailableVariants: availableVariants(def),
			Definition:        def,
			Issues:            entryIssues,
		})
	}
	return out
}
