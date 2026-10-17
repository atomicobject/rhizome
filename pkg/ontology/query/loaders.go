package query

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

type loaders struct {
	deps         Deps
	vaultPaths   paths.VaultPaths
	scope        *noderead.Scope
	recordByPath map[string]*noteRecord
	recordKnown  map[string]bool
	typeByPath   map[string]semdb.OntologyNoteTypeRow
}

func newLoaders(deps Deps, schema *ontoschema.Schema) (*loaders, error) {
	vaultPaths, err := paths.NewVaultPaths(deps.VaultDef.BasePath())
	if err != nil {
		return nil, err
	}
	return &loaders{
		deps:       deps,
		vaultPaths: vaultPaths,
		// One Scope per Execute call keeps relation/type/hydration caches shared
		// across field selections without leaking request-shaped data into the
		// process. This is the performance boundary for GraphQL execution.
		scope: func() *noderead.Scope {
			service := noderead.NewService(deps.VaultDef, deps.NoteReader, deps.Store, schema).WithNoteFormats(deps.NoteFormats)
			service.ExactNoteMetadataRows = deps.ExactNoteMetadataRows
			service.ExactNotePropertyValues = deps.ExactNotePropertyValues
			service.ExactNoteTags = deps.ExactNoteTags
			return service.NewScope(noderead.ScopeOptions{ReadOverlay: deps.ReadOverlay})
		}(),
		recordByPath: make(map[string]*noteRecord),
		recordKnown:  make(map[string]bool),
		typeByPath:   make(map[string]semdb.OntologyNoteTypeRow),
	}, nil
}

func (l *loaders) noteByPath(ctx context.Context, notePath string) (*noteRecord, error) {
	records, err := l.recordsForPaths(ctx, []string{notePath})
	if err != nil || len(records) == 0 {
		return nil, err
	}
	return records[0], nil
}

func (l *loaders) noteRowsByType(ctx context.Context, typeName string, limit int) ([]*noteRecord, error) {
	pathsList, err := l.deps.Store.OntologyPathsByType(ctx, typeName, limit)
	if err != nil {
		return nil, err
	}
	return l.recordsForPaths(ctx, pathsList)
}

func (l *loaders) recordsForPaths(ctx context.Context, pathsList []string) ([]*noteRecord, error) {
	pathsList = normalizeStrings(pathsList)
	if len(pathsList) == 0 {
		return []*noteRecord{}, nil
	}
	missing := make([]string, 0, len(pathsList))
	for _, notePath := range pathsList {
		if !l.recordKnown[notePath] {
			missing = append(missing, notePath)
		}
	}

	if len(missing) > 0 {
		scopeRecords, err := l.scope.Hydrate(ctx, noteRefsForPaths(missing), noderead.HydrateOptions{Profile: noderead.HydrateSummary})
		if err != nil {
			return nil, err
		}
		loaded := noteRecordsFromNodeRecords(scopeRecords)
		loadedByPath := make(map[string]*noteRecord, len(loaded))
		for _, record := range loaded {
			if record != nil {
				loadedByPath[record.Path] = record
			}
		}
		for _, notePath := range missing {
			l.recordKnown[notePath] = true
			if record := loadedByPath[notePath]; record != nil {
				l.recordByPath[notePath] = record
			}
		}
	}
	out := make([]*noteRecord, 0, len(pathsList))
	for _, notePath := range pathsList {
		if record := l.recordByPath[notePath]; record != nil {
			out = append(out, record)
		}
	}
	return out, nil
}

func (l *loaders) hydrateContentForPath(ctx context.Context, notePath string) (*noteRecord, error) {
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return nil, nil
	}
	scopeRecords, err := l.scope.Hydrate(ctx, []ontology.NodeRef{{NotePath: notePath, Kind: ontology.NodeKindNote}}, noderead.HydrateOptions{Profile: noderead.HydrateContent})
	if err != nil || len(scopeRecords) == 0 {
		return nil, err
	}
	record := noteRecordsFromNodeRecords(scopeRecords)[0]
	if existing := l.recordByPath[notePath]; existing != nil {
		existing.Content = record.Content
		existing.InlineProps = cloneInlineProps(record.InlineProps)
		existing.Frontmatter = cloneMap(record.Frontmatter)
		existing.Tags = cloneStrings(record.Tags)
		existing.Title = record.Title
		if record.TypeName != "" {
			existing.TypeName = record.TypeName
		}
		record = existing
	} else {
		l.recordByPath[notePath] = record
	}
	l.recordKnown[notePath] = true
	return record, nil
}

func (l *loaders) rememberNodeViews(views []noderead.NodeView) {
	if len(views) == 0 {
		return
	}
	records := noteRecordsFromNodeRecords(views)
	for _, record := range records {
		if record == nil || record.Path == "" {
			continue
		}
		l.recordKnown[record.Path] = true
		if existing := l.recordByPath[record.Path]; existing != nil {
			if record.Content != "" {
				existing.Content = record.Content
			}
			if len(record.InlineProps) > 0 {
				existing.InlineProps = cloneInlineProps(record.InlineProps)
			}
			if len(record.Frontmatter) > 0 {
				existing.Frontmatter = cloneMap(record.Frontmatter)
			}
			if len(record.Tags) > 0 {
				existing.Tags = cloneStrings(record.Tags)
			}
			if record.Title != "" {
				existing.Title = record.Title
			}
			if record.TypeName != "" {
				existing.TypeName = record.TypeName
			}
			continue
		}
		l.recordByPath[record.Path] = record
	}
}

func (l *loaders) refreshRecords(records []*noteRecord) []*noteRecord {
	if len(records) == 0 {
		return records
	}
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		if record == nil || record.Path == "" {
			out = append(out, record)
			continue
		}
		cached := l.recordByPath[record.Path]
		if cached == nil || cached == record {
			out = append(out, record)
			continue
		}
		score := record.Score
		refreshed := *cached
		refreshed.Score = score
		out = append(out, &refreshed)
	}
	return out
}

func (l *loaders) ontologyTypesByPaths(ctx context.Context, pathsList []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	pathsList = normalizeStrings(pathsList)
	if len(pathsList) == 0 {
		return map[string]semdb.OntologyNoteTypeRow{}, nil
	}
	missing := make([]string, 0, len(pathsList))
	out := make(map[string]semdb.OntologyNoteTypeRow, len(pathsList))
	for _, notePath := range pathsList {
		if row, ok := l.typeByPath[notePath]; ok {
			out[notePath] = row
			continue
		}
		missing = append(missing, notePath)
	}

	if len(missing) > 0 {
		rows, err := l.scope.TypesByPaths(ctx, missing)
		if err != nil {
			return nil, err
		}
		for notePath, row := range rows {
			l.typeByPath[notePath] = row
			out[notePath] = row
		}
	}
	return out, nil
}

func (l *loaders) assessmentsByPaths(ctx context.Context, pathsList []string) (map[string]*ontology.NoteAssessment, error) {
	return l.scope.AssessmentsByPaths(ctx, pathsList)
}

func (l *loaders) structuralEdges(ctx context.Context, sources []string, relation string) (map[string][]semdb.OntologyEdgeRow, error) {
	result, err := l.scope.Edges(ctx, noderead.NeighborhoodRequest{
		Sources: noteRefsForPaths(sources), Direction: noderead.TraversalDirectionOutbound,
		RelationNames: optionalSingle(relation), IncludeStructural: true,
	})
	return edgeRowsBySource(result), err
}

func (l *loaders) ambientEdges(ctx context.Context, sources []string, relation string, provenance map[string]struct{}, dstType string, limitPerSource int) (map[string][]semdb.OntologyEdgeRow, error) {
	result, err := l.scope.Edges(ctx, noderead.NeighborhoodRequest{
		Sources: noteRefsForPaths(sources), Direction: noderead.TraversalDirectionOutbound,
		RelationNames: optionalSingle(relation), IncludeAmbient: true, Provenance: provenance,
		TargetTypes: optionalSingle(dstType), FirstPerSource: limitPerSource,
	})
	return edgeRowsBySource(result), err
}

func edgeRowsBySource(result noderead.NeighborhoodResult) map[string][]semdb.OntologyEdgeRow {
	rows := make(map[string][]semdb.OntologyEdgeRow, len(result.Sources))
	for _, source := range result.Sources {
		rows[source.Source.NotePath] = nil
		for _, edge := range source.Edges {
			rows[source.Source.NotePath] = append(rows[source.Source.NotePath], edge.Edge)
		}
	}
	return rows
}

func noteRefsForPaths(pathsList []string) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(pathsList))
	for _, notePath := range pathsList {
		if notePath = strings.TrimSpace(notePath); notePath != "" {
			out = append(out, ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindNote})
		}
	}
	return out
}

func noteRecordsFromNodeRecords(records []noderead.NodeRecord) []*noteRecord {
	out := make([]*noteRecord, 0, len(records))
	for _, record := range records {
		out = append(out, &noteRecord{
			Path:                   record.Path,
			Title:                  record.Title,
			Content:                record.Content,
			Frontmatter:            cloneMap(record.Frontmatter),
			InlineProps:            cloneInlineProps(record.InlineProps),
			Tags:                   cloneStrings(record.Tags),
			TypeName:               record.TypeName,
			Format:                 record.Format,
			SourceRepresentation:   record.SourceRepresentation,
			EvidenceRepresentation: record.EvidenceRepresentation,
			Capabilities:           append([]noteformat.Capability(nil), record.Capabilities...),
		})
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneInlineProps(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = cloneStrings(values)
	}
	return out
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
