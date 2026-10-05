package views

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// boardOpenRecordLimit is the most open or active records a generated
// workflow view shows as its default Board.
const boardOpenRecordLimit = 60

// fieldValueCounter counts indexed records per normalized field value; the
// Intel store implements it with one indexed GROUP BY.
type fieldValueCounter interface {
	OntologyFieldValueCounts(ctx context.Context, typeNames []string, fieldName string) (map[string]int, error)
}

// generatedDefaultVariant applies SPEC-0112's default-variant rule to a
// generated view: Board when its shape is workflow, at least two open or
// active lifecycle values have records, and at most 60 records are open or
// active; otherwise its declared default (Table). Authored views keep theirs.
//
// ponytail: one indexed count query per workflow type on each catalog read,
// uncached; cache by index generation if catalog reads become hot.
func (s *Service) generatedDefaultVariant(ctx context.Context, entry CatalogEntry) string {
	fallback := entry.Defaults.Variant
	def := entry.Definition
	if !entry.Generated || def.Variants.Kanban == nil {
		return fallback
	}
	profile, ok := sourceProfile(s.opts.Schema, def)
	if !ok || profile.Shape != ontology.ShapeWorkflow {
		return fallback
	}
	counter, ok := s.opts.Store.(fieldValueCounter)
	if !ok {
		return fallback
	}
	stages := enumValueStages(lifecycleEnum(s.opts.Schema, subjectFields(s.opts.Schema, sourceSubject(def)), profile))
	typeNames := defaultSourceResolver{opts: s.opts}.sourceTypeNames(def)
	counts, err := counter.OntologyFieldValueCounts(ctx, typeNames, profile.LifecycleField)
	if err != nil || !boardFitsOpenWork(stages, counts) {
		return fallback
	}
	return "kanban"
}

// boardFitsOpenWork reports whether open work spans two or more lifecycle
// values and fits on one board. counts are keyed by lowercased value.
func boardFitsOpenWork(stages map[string]ontology.LifecycleStage, counts map[string]int) bool {
	populated, open := 0, 0
	for value, stage := range stages {
		if stage != ontology.StageOpen && stage != ontology.StageActive {
			continue
		}
		if count := counts[strings.ToLower(value)]; count > 0 {
			populated++
			open += count
		}
	}
	return populated >= 2 && open <= boardOpenRecordLimit
}
