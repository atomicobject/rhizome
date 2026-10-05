package query

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type loaders struct {
	selectorInventory selectorInventory

	deps       Deps
	vaultPaths paths.VaultPaths
	scope      *noderead.Scope

	snapshotOnce sync.Once
	snapshotBy   map[string]*noteRecord
	snapshotList []*noteRecord
	snapshotErr  error

	recordMu     sync.Mutex
	recordByPath map[string]*noteRecord
	recordKnown  map[string]bool

	typeMu     sync.Mutex
	typeByPath map[string]semdb.OntologyNoteTypeRow
}

func newLoaders(deps Deps, schema *ontology.Schema) (*loaders, error) {
	vaultPaths, err := paths.NewVaultPaths(deps.VaultDef.BasePath())
	if err != nil {
		return nil, err
	}
	return &loaders{
		deps:       deps,
		vaultPaths: vaultPaths,
		// One Scope per Execute call keeps relation/type/hydration caches shared
		// across resolver goroutines without leaking request-shaped data into the
		// process. This is the performance boundary for GraphQL execution.
		scope: func() *noderead.Scope {
			service := noderead.NewService(deps.VaultDef, deps.NoteReader, deps.Store, schema).WithNoteFormats(deps.NoteFormats)
			service.ExactNoteMetadataRows = deps.ExactNoteMetadataRows
			service.ExactNotePropertyValues = deps.ExactNotePropertyValues
			service.ExactNoteTags = deps.ExactNoteTags
			return service.NewScope(context.Background(), noderead.ScopeOptions{ReadOverlay: deps.ReadOverlay})
		}(),
		recordByPath: make(map[string]*noteRecord),
		recordKnown:  make(map[string]bool),
		typeByPath:   make(map[string]semdb.OntologyNoteTypeRow),
	}, nil
}

func (l *loaders) snapshot(ctx context.Context) (map[string]*noteRecord, []*noteRecord, error) {
	l.snapshotOnce.Do(func() {
		if l.deps.Store == nil {
			l.snapshotBy = map[string]*noteRecord{}
			l.snapshotList = []*noteRecord{}
			return
		}
		metadataRows, err := l.deps.Store.CurrentNoteMetadataRows(ctx)
		if err != nil {
			l.snapshotErr = err
			return
		}
		markdownPaths := make(map[string]struct{}, len(metadataRows))
		for _, row := range metadataRows {
			if row.FormatID == string(noteformat.FormatID("markdown")) && row.Projection.Status == semdb.NoteProjectionStatusCurrent {
				markdownPaths[row.Path] = struct{}{}
			}
		}
		if provider, ok := l.deps.NoteReader.(obsidian.NoteEntriesProvider); ok {
			entries, err := provider.NoteEntriesSnapshot(ctx)
			if err == nil {
				l.snapshotBy = make(map[string]*noteRecord, len(entries))
				l.snapshotList = make([]*noteRecord, 0, len(entries))
				for _, entry := range entries {
					if _, markdown := markdownPaths[entry.Path]; !markdown {
						continue
					}
					record := noteRecordFromEntry(entry)
					l.snapshotBy[record.Path] = record
					l.snapshotList = append(l.snapshotList, record)
				}
				sort.Slice(l.snapshotList, func(i, j int) bool {
					return l.snapshotList[i].Path < l.snapshotList[j].Path
				})
				return
			}
			l.snapshotErr = err
			return
		}

		pathsList, err := l.deps.NoteReader.GetNotesList(l.deps.VaultDef)
		if err != nil {
			l.snapshotErr = err
			return
		}
		sort.Strings(pathsList)
		l.snapshotBy = make(map[string]*noteRecord, len(pathsList))
		l.snapshotList = make([]*noteRecord, 0, len(pathsList))
		for _, notePath := range pathsList {
			if _, markdown := markdownPaths[notePath]; !markdown {
				continue
			}
			content, err := l.deps.NoteReader.GetContents(l.deps.VaultDef, notePath)
			if err != nil {
				continue
			}
			snapshot, err := ontology.BuildDocumentSnapshot(notePath, content, time.Time{})
			if err != nil {
				continue
			}
			fm := snapshot.Frontmatter
			record := &noteRecord{
				Path:        notePath,
				Title:       noteTitle(notePath, fm),
				Content:     content,
				Frontmatter: cloneMap(fm),
				InlineProps: ontology.SectionInlineProperties(content),
				Tags:        extractTags(fm),
				TypeName:    stringValue(fm[ontology.TypeFieldName()]),
			}
			l.snapshotBy[notePath] = record
			l.snapshotList = append(l.snapshotList, record)
		}
	})
	return l.snapshotBy, l.snapshotList, l.snapshotErr
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
	l.recordMu.Lock()
	missing := make([]string, 0, len(pathsList))
	for _, notePath := range pathsList {
		if !l.recordKnown[notePath] {
			missing = append(missing, notePath)
		}
	}
	l.recordMu.Unlock()

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
		l.recordMu.Lock()
		for _, notePath := range missing {
			l.recordKnown[notePath] = true
			if record := loadedByPath[notePath]; record != nil {
				l.recordByPath[notePath] = record
			}
		}
		l.recordMu.Unlock()
	}

	l.recordMu.Lock()
	defer l.recordMu.Unlock()
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
	l.recordMu.Lock()
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
	l.recordMu.Unlock()
	return record, nil
}

func (l *loaders) rememberNodeViews(views []noderead.NodeView) {
	if len(views) == 0 {
		return
	}
	records := noteRecordsFromNodeRecords(nodeRecordsFromViews(views))
	l.recordMu.Lock()
	defer l.recordMu.Unlock()
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
	l.recordMu.Lock()
	defer l.recordMu.Unlock()
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

	l.typeMu.Lock()
	missing := make([]string, 0, len(pathsList))
	out := make(map[string]semdb.OntologyNoteTypeRow, len(pathsList))
	for _, notePath := range pathsList {
		if row, ok := l.typeByPath[notePath]; ok {
			out[notePath] = row
			continue
		}
		missing = append(missing, notePath)
	}
	l.typeMu.Unlock()

	if len(missing) > 0 {
		rows, err := l.scope.TypesByPaths(ctx, missing)
		if err != nil {
			return nil, err
		}
		l.typeMu.Lock()
		for notePath, row := range rows {
			l.typeByPath[notePath] = row
			out[notePath] = row
		}
		l.typeMu.Unlock()
	}
	return out, nil
}

func (l *loaders) assessmentsByPaths(ctx context.Context, pathsList []string) (map[string]*ontology.NoteAssessment, error) {
	return l.scope.AssessmentsByPaths(ctx, pathsList)
}

func (l *loaders) structuralEdges(ctx context.Context, sources []string, relation string) (map[string][]semdb.OntologyEdgeRow, error) {
	result, err := l.scope.Traverse(ctx, noderead.TraverseRequest{
		Sources:    noteRefsForPaths(sources),
		Relation:   relation,
		Structural: true,
	})
	if err != nil {
		return nil, err
	}
	return result.EdgesBySource, nil
}

func (l *loaders) ambientEdges(ctx context.Context, sources []string, relation string, provenance map[string]struct{}, dstType string, limitPerSource int) (map[string][]semdb.OntologyEdgeRow, error) {
	result, err := l.scope.Traverse(ctx, noderead.TraverseRequest{
		Sources:        noteRefsForPaths(sources),
		Relation:       relation,
		Provenance:     provenance,
		DstType:        dstType,
		LimitPerSource: limitPerSource,
	})
	if err != nil {
		return nil, err
	}
	return result.EdgesBySource, nil
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

func nodeRecordsFromViews(views []noderead.NodeView) []noderead.NodeRecord {
	out := make([]noderead.NodeRecord, 0, len(views))
	for _, view := range views {
		out = append(out, view.Record)
	}
	return out
}

func noteRecordFromEntry(entry obsidian.NoteEntry) *noteRecord {
	fm := cloneMap(entry.Frontmatter)
	return &noteRecord{
		Path:        entry.Path,
		Title:       noteTitle(entry.Path, fm),
		Content:     entry.Content,
		Frontmatter: fm,
		InlineProps: ontology.SectionInlineProperties(entry.Content),
		Tags:        cloneStrings(entry.Tags),
		TypeName:    stringValue(fm[ontology.TypeFieldName()]),
	}
}

func noteTitle(notePath string, fm map[string]any) string {
	if title := stringValue(fm["title"]); title != "" {
		return title
	}
	base := filepath.Base(notePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
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

func flattenValueStrings(v any) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case []string:
		return normalizeStrings(val)
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, flattenValueStrings(item)...)
		}
		return normalizeStrings(out)
	case string:
		return normalizeStrings([]string{val})
	case bool:
		return []string{strconv.FormatBool(val)}
	case int:
		return []string{strconv.Itoa(val)}
	case int64:
		return []string{strconv.FormatInt(val, 10)}
	case float64:
		return []string{strconv.FormatFloat(val, 'f', -1, 64)}
	case time.Time:
		if val.Hour() == 0 && val.Minute() == 0 && val.Second() == 0 {
			return []string{val.Format("2006-01-02")}
		}
		return []string{val.Format(time.RFC3339)}
	default:
		return []string{strings.TrimSpace(fmt.Sprint(val))}
	}
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

// extractTags keeps persisted root tag values. Provider projection owns inline
// hashtag syntax; neutral GraphQL loading never reparses it.
func extractTags(fm map[string]any) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, value := range flattenValueStrings(fm["tags"]) {
		value = strings.TrimPrefix(value, "#")
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
