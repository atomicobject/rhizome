package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/guide"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type ontologyDefinitions struct {
	schema  *ontology.Schema
	exec    *ontologyquery.ExecutableSchema
	execErr error
}

func (s *Server) ontologyDefinitions() (*ontologyDefinitions, error) {
	if s == nil {
		return nil, nil
	}
	currentHash, err := ontology.SchemaSourceHash(s.cfg.VaultPath)
	if errors.Is(err, ontology.ErrNoOntologyFiles) {
		currentHash, err = "", nil
	}
	if err != nil {
		return nil, err
	}

	s.ontologyMu.RLock()
	cached := s.ontologyDefs
	s.ontologyMu.RUnlock()
	if cached != nil && cached.schema != nil && cached.schema.Hash == currentHash {
		return cached, nil
	}

	s.ontologyMu.Lock()
	defer s.ontologyMu.Unlock()
	if s.ontologyDefs != nil && s.ontologyDefs.schema != nil && s.ontologyDefs.schema.Hash == currentHash {
		return s.ontologyDefs, nil
	}

	schema, err := ontology.LoadSchema(s.cfg.VaultPath)
	if err != nil {
		if errors.Is(err, ontology.ErrNoOntologyFiles) {
			s.ontologyDefs = nil
			return nil, nil
		}
		return nil, err
	}
	if schema.Hash != currentHash {
		return nil, fmt.Errorf("%w: ontology schema changed during reload", errIndexInitializing)
	}
	exec, execErr := ontologyquery.BuildExecutableSchema(schema)
	s.ontologyDefs = &ontologyDefinitions{schema: schema, exec: exec, execErr: execErr}
	return s.ontologyDefs, nil
}

func (s *Server) ontologyContext() (*ontology.Service, *ontologyDefinitions, error) {
	defs, err := s.ontologyDefinitions()
	if err != nil || defs == nil {
		return nil, defs, err
	}
	store := s.runtime.Intel()
	if store == nil {
		return nil, defs, nil
	}
	return ontology.NewService(s.cfg.VaultDef, s.defaultOntologyNoteReader(), store, defs.schema), defs, nil
}

func (s *Server) ontologyQueryDeps(service *ontology.Service, noteReader obsidian.NoteReader) ontologyquery.Deps {
	return s.ontologyQueryDepsForExactPaths(service, noteReader, nil)
}

func (s *Server) ontologyQueryDepsForExactPaths(service *ontology.Service, noteReader obsidian.NoteReader, exactSnapshot *exactNoteReadSnapshot) ontologyquery.Deps {
	if noteReader == nil {
		noteReader = s.defaultOntologyNoteReader()
	}
	store := s.runtime.Intel()
	if service != nil && service.Store != nil {
		store = service.Store
	}
	deps := ontologyquery.Deps{
		VaultDef:   s.cfg.VaultDef,
		NoteReader: noteReader,
		Store:      store,
		Service:    service,
		PathOwner:  s.ontologyQueryPathOwner,
	}
	if formats, err := s.noteMetadata.FormatRuntime(); err == nil {
		deps.NoteFormats = formats
	}
	if store != nil {
		if exactSnapshot == nil {
			deps.ExactNoteMetadataRows = func(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
				return s.exactNoteMetadataRowsFromStore(ctx, store, paths)
			}
			deps.ExactNotePropertyValues = store.DurableNotePropertyValuesByPaths
			deps.ExactNoteTags = store.DurableNoteTagsByPaths
		} else {
			deps.ExactNoteMetadataRows = exactSnapshot.metadataRows
			deps.ExactNotePropertyValues = exactSnapshot.propertyValues
			deps.ExactNoteTags = exactSnapshot.tags
		}
	}
	_, noteProvider := s.runtime.NoteEmbeddings()
	if store != nil && noteProvider != nil {
		deps.SemanticSearcher = &semantic.Searcher{
			NoteProvider: noteProvider,
			IntelStore:   store,
		}
	}
	if store != nil {
		_, codeProvider := s.runtime.CodeEmbeddings()
		deps.NoteSearcher = unifiedsearch.NoteSearcher{Runtime: unifiedsearch.Options{
			VaultPath:    s.cfg.VaultPath,
			VaultDef:     s.cfg.VaultDef,
			IntelStore:   store,
			NoteProvider: noteProvider,
			CodeProvider: codeProvider,
		}}
	}
	return deps
}

func (s *Server) exactNoteMetadataRowsFromStore(ctx context.Context, store *semdb.Store, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	if store == nil {
		return map[string]semdb.NoteMetadataRow{}, nil
	}
	rows, err := s.noteMetadata.UsableNoteMetadataRowsByPaths(ctx, store, paths)
	if err != nil {
		return nil, err
	}
	for path := range rows {
		if !s.exactNotePathOwned(path) {
			delete(rows, path)
		}
	}
	return rows, nil
}

func (s *Server) exactNotePathOwned(path string) bool {
	if s != nil && s.cfg.NotePathOwned != nil {
		return s.cfg.NotePathOwned(path)
	}
	return s.ontologyQueryPathOwner(path) == ontologyquery.PathOwnerNote
}

// noteCacheRefresher is a note reader backed by a watcher-fed cache, which a
// write must mark and refresh before later reads can see it.
type noteCacheRefresher interface {
	MarkDirty(relPath string, kind cache.DirtyKind)
	Refresh(ctx context.Context) error
}

// defaultOntologyNoteReader prefers the runtime's live note cache. `rzm serve`
// builds the server before the runtime publishes that cache, so Config.Cache
// can be nil while reads already go through it.
func (s *Server) defaultOntologyNoteReader() obsidian.NoteReader {
	if s.runtime != nil && s.runtime.Live != nil {
		if reader := s.runtime.Live.Snapshot().NoteReader; reader != nil {
			return reader
		}
	}
	if s.noteReader != nil {
		return s.noteReader
	}
	return &obsidian.Note{}
}

func (s *Server) ontologyQueryPathOwner(path string) ontologyquery.PathOwner {
	kind, _ := s.classifyFile(path)
	switch kind {
	case "note":
		return ontologyquery.PathOwnerNote
	case "code":
		return ontologyquery.PathOwnerCode
	default:
		return ontologyquery.PathOwnerUnknown
	}
}

func (s *Server) ontologySummary(ctx context.Context) (OntologySummaryResponse, error) {
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return OntologySummaryResponse{}, err
	}
	resp := OntologySummaryResponse{}
	if defs != nil {
		resp.SchemaPresent = true
		resp.SchemaHash = defs.schema.Hash
		resp.QuerySchemaPresent = defs.exec != nil
		if defs.exec != nil {
			resp.QuerySchemaSDL = defs.exec.SDL
		}
		if defs.execErr != nil {
			resp.QuerySchemaError = defs.execErr.Error()
		}
	}

	store := s.runtime.Intel()
	if store == nil {
		return resp, nil
	}
	// Ownership transitions clear the published metadata state until their
	// batch republishes it; gated reads return nothing in that window.
	if ready, err := notemeta.MetadataStateReady(ctx, store); err != nil {
		return resp, err
	} else if !ready {
		return s.rebuildingSummary(resp, defs), nil
	}
	counted, err := s.countOntologySummary(ctx, store, defs, resp)
	if err != nil {
		return counted, err
	}
	if ready, err := notemeta.MetadataStateReady(ctx, store); err != nil || !ready {
		return s.rebuildingSummary(resp, defs), err
	}
	s.summaryMu.Lock()
	s.lastSummary = &counted
	s.summaryMu.Unlock()
	return counted, nil
}

// rebuildingSummary keeps the fresh schema fields of resp and fills counts and
// types from the last published summary. Before the first one, as right after
// a start, it lists the schema's types without counts, so the rail, type
// routes, and type colors work while counts stay unknown (zero totals).
func (s *Server) rebuildingSummary(resp OntologySummaryResponse, defs *ontologyDefinitions) OntologySummaryResponse {
	s.summaryMu.Lock()
	last := s.lastSummary
	s.summaryMu.Unlock()
	if last != nil {
		resp.TotalNotes = last.TotalNotes
		resp.TypedNotes = last.TypedNotes
		resp.UntypedNotes = last.UntypedNotes
		resp.AmbiguousNotes = last.AmbiguousNotes
		resp.IssueNotes = last.IssueNotes
		resp.Types = last.Types
		resp.Interfaces = last.Interfaces
	} else if defs != nil && defs.schema != nil {
		if types, interfaces, err := schemaSummaryEntries(defs); err == nil {
			resp.Types = types
			resp.Interfaces = interfaces
		}
	}
	resp.Rebuilding = true
	return resp
}

func (s *Server) countOntologySummary(ctx context.Context, store *semdb.Store, defs *ontologyDefinitions, resp OntologySummaryResponse) (OntologySummaryResponse, error) {
	if defs == nil || defs.schema == nil {
		rows, err := store.CurrentNoteMetadataRows(ctx)
		if err != nil {
			return resp, err
		}
		resp.TotalNotes = len(rows)
		resp.UntypedNotes = resp.TotalNotes
		return resp, nil
	}

	nodeScope := noderead.NewService(s.cfg.VaultDef, &obsidian.Note{}, store, defs.schema).NewScope(ctx, noderead.ScopeOptions{})
	all, err := nodeScope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: ontology.TypeScopeAll})
	if err != nil {
		return resp, err
	}
	pathsList := make([]string, 0, len(all.Items))
	for _, item := range all.Items {
		pathsList = append(pathsList, item.NotePath)
	}
	resp.TotalNotes = all.Count

	typeRows, err := nodeScope.TypesByPaths(ctx, pathsList)
	if err != nil {
		return resp, err
	}
	resp.TypedNotes = len(typeRows)
	resp.UntypedNotes = resp.TotalNotes - resp.TypedNotes

	flags, err := nodeScope.AssessmentFlags(ctx)
	if err != nil {
		return resp, err
	}
	ambiguousCount := 0
	issueCount := 0
	for _, entry := range flags {
		if entry.TypeAmbiguous {
			ambiguousCount++
		}
		if entry.HasIssues {
			issueCount++
		}
	}

	resp.AmbiguousNotes = ambiguousCount
	resp.IssueNotes = issueCount
	types, interfaces, err := schemaSummaryEntries(defs)
	if err != nil {
		return resp, err
	}
	for i := range types {
		result, err := nodeScope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: types[i].Name})
		if err != nil {
			return resp, err
		}
		types[i].Count = result.Count
		types[i].IssueCount = result.IssueCount
		types[i].StartingNotes = ontologyItemStartingRefs(result.Items, 5)
	}
	for i := range interfaces {
		result, err := nodeScope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: interfaces[i].Name})
		if err != nil {
			return resp, err
		}
		interfaces[i].Count = result.Count
		interfaces[i].IssueCount = result.IssueCount
	}
	sortByCountThenName(types, func(t OntologyTypeSummary) (int, string) { return t.Count, t.Name })
	sortByCountThenName(interfaces, func(i OntologyInterfaceSummary) (int, string) { return i.Count, i.Name })
	resp.Types = types
	resp.Interfaces = interfaces
	return resp, nil
}

// sortByCountThenName orders summary entries by count, largest first, then name.
func sortByCountThenName[T any](items []T, key func(T) (int, string)) {
	sort.SliceStable(items, func(i, j int) bool {
		ci, ni := key(items[i])
		cj, nj := key(items[j])
		if ci != cj {
			return ci > cj
		}
		return ni < nj
	})
}

// schemaSummaryEntries lists the rail's types and interfaces from the schema
// alone, without counts, sorted by name.
func schemaSummaryEntries(defs *ontologyDefinitions) ([]OntologyTypeSummary, []OntologyInterfaceSummary, error) {
	typeDocs, err := ontology.SchemaDocs(defs.schema, "")
	if err != nil {
		return nil, nil, err
	}
	typeDocByName := make(map[string]ontology.TypeDoc, len(typeDocs))
	for _, doc := range typeDocs {
		typeDocByName[doc.Name] = doc
	}

	// Shared with viewconfig.DisplayGroups so group mounts see the rail's members.
	typeNames, interfaceMembers := viewconfig.NavigationMembers(defs.schema)

	types := make([]OntologyTypeSummary, 0, len(typeNames))
	for _, typeName := range typeNames {
		roleLabel := "note"
		if defs.schema.Types[typeName].Role == ontology.TypeRoleEmbeddedNode {
			roleLabel = "embedded"
		}
		doc := typeDocByName[typeName]
		types = append(types, OntologyTypeSummary{
			Name:          typeName,
			Label:         doc.Label,
			PluralLabel:   doc.PluralLabel,
			DisplayGroup:  doc.DisplayGroup,
			DisplayParent: doc.DisplayParent,
			Color:         doc.Color,
			Description:   doc.Description,
			Role:          roleLabel,
		})
	}
	sort.SliceStable(types, func(i, j int) bool { return types[i].Name < types[j].Name })

	interfaceNames := make([]string, 0, len(interfaceMembers))
	for name := range interfaceMembers {
		interfaceNames = append(interfaceNames, name)
	}
	sort.Strings(interfaceNames)
	interfaces := make([]OntologyInterfaceSummary, 0, len(interfaceNames))
	for _, ifaceName := range interfaceNames {
		doc := typeDocByName[ifaceName]
		interfaces = append(interfaces, OntologyInterfaceSummary{
			Name:          ifaceName,
			Label:         doc.Label,
			PluralLabel:   doc.PluralLabel,
			DisplayGroup:  doc.DisplayGroup,
			DisplayParent: doc.DisplayParent,
			Description:   doc.Description,
			Implementors:  interfaceMembers[ifaceName],
		})
	}
	return types, interfaces, nil
}

// ontologyAtlas builds the single-fetch atlas payload used by the web UI's
// ontology overview: every note- or embedded-role TypeDoc with vault-grounded
// counts plus interface TypeDocs so the client can render the schema graph +
// type cards without an N+1 round-trip per type.
func (s *Server) ontologyAtlas(ctx context.Context) (OntologyAtlasResponse, error) {
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return OntologyAtlasResponse{}, err
	}
	resp := OntologyAtlasResponse{}
	if s != nil {
		resp.VaultName = s.cfg.VaultDef.Name
	}
	if defs == nil || defs.schema == nil {
		return resp, nil
	}
	resp.SchemaPresent = true
	resp.SchemaHash = defs.schema.Hash
	nodeScope := noderead.NewService(s.cfg.VaultDef, &obsidian.Note{}, s.runtime.Intel(), defs.schema).NewScope(ctx, noderead.ScopeOptions{})

	docs, err := ontology.SchemaDocs(defs.schema, "")
	if err != nil {
		return resp, err
	}
	docsByName := make(map[string]ontology.TypeDoc, len(docs))
	for _, doc := range docs {
		docsByName[doc.Name] = doc
	}

	atlasTypeNames := make([]string, 0, len(defs.schema.Types))
	for name, noteType := range defs.schema.Types {
		if noteType == nil {
			continue
		}
		if noteType.Role != ontology.TypeRoleNote && noteType.Role != ontology.TypeRoleEmbeddedNode {
			continue
		}
		atlasTypeNames = append(atlasTypeNames, name)
	}
	sort.Strings(atlasTypeNames)

	entries := make([]OntologyAtlasTypeEntry, 0, len(atlasTypeNames))
	for _, name := range atlasTypeNames {
		doc := docsByName[name]
		result, err := nodeScope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: name})
		if err != nil {
			return resp, err
		}
		typeDoc := doc
		entries = append(entries, OntologyAtlasTypeEntry{
			Type:          &typeDoc,
			Count:         result.Count,
			IssueCount:    result.IssueCount,
			StartingNotes: ontologyItemStartingRefs(result.Items, 5),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Type.Name < entries[j].Type.Name
	})
	resp.Types = entries

	interfaceNames := make([]string, 0, len(defs.schema.Interfaces))
	for name := range defs.schema.Interfaces {
		interfaceNames = append(interfaceNames, name)
	}
	sort.Strings(interfaceNames)
	for _, name := range interfaceNames {
		if doc, ok := docsByName[name]; ok {
			resp.Interfaces = append(resp.Interfaces, doc)
		}
	}

	sectionNames := make([]string, 0, len(defs.schema.Types))
	for name, noteType := range defs.schema.Types {
		if noteType == nil || noteType.Role != ontology.TypeRoleSection {
			continue
		}
		sectionNames = append(sectionNames, name)
	}
	sort.Strings(sectionNames)
	for _, name := range sectionNames {
		if doc, ok := docsByName[name]; ok {
			resp.Sections = append(resp.Sections, doc)
		}
	}
	return resp, nil
}

// Pseudo-type sentinels accepted by ontologyType. They produce synthetic views
// across all notes rather than a single schema-defined type: "__all__" shows
// every indexed note, "__issues__" shows only notes with validation issues.
// "__modified__" is surfaced in the ontology workspace rail while an edit
// session has uncommitted changes; its content is fetched separately via the
// session diff endpoint rather than through ontologyType.
const (
	pseudoTypeAll    = "__all__"
	pseudoTypeIssues = "__issues__"
)

// ontologyType lists a type's instances; a non-nil overlay reflects an edit session.
func (s *Server) ontologyType(ctx context.Context, typeName string, overlay *noderead.ReadOverlay) (OntologyTypeResponse, error) {
	schema, nodeScope, result, err := s.ontologyTypeInstances(ctx, typeName, overlay)
	if err != nil || nodeScope == nil {
		return OntologyTypeResponse{}, err
	}
	return s.enrichOntologyType(ctx, typeName, overlay, schema, nodeScope, result)
}

func (s *Server) enrichOntologyType(ctx context.Context, typeName string, overlay *noderead.ReadOverlay, schema *ontology.Schema, nodeScope *noderead.Scope, result ontology.TypeListResult) (OntologyTypeResponse, error) {
	pathsList := uniqueOntologyItemNotePaths(result.Items)
	tagsByPath := map[string][]string{}
	issuesByPath := map[string]bool{}
	issueItemsByPath := map[string][]OntologyNoteIssueItem{}
	relationCountByPath := map[string]int{}
	identityStatusByItemRef := map[string]string{}
	issueCount := result.IssueCount
	identityStatusField := pickIdentityStatusField(result.TypeDoc)
	var fetchErr error
	var fetchMu sync.Mutex
	var wg sync.WaitGroup
	setErr := func(err error) {
		if err == nil {
			return
		}
		fetchMu.Lock()
		if fetchErr == nil {
			fetchErr = err
		}
		fetchMu.Unlock()
	}

	wg.Add(4)
	go func() {
		defer wg.Done()
		tagRows, err := s.noteTagsByPaths(ctx, pathsList)
		if err != nil {
			setErr(err)
			return
		}
		fetchMu.Lock()
		tagsByPath = tagRows
		fetchMu.Unlock()
	}()
	go func() {
		defer wg.Done()
		assessments, err := nodeScope.AssessmentsByPaths(ctx, pathsList)
		if err != nil {
			setErr(err)
			return
		}
		issues, items, _, totalIssues, err := assessmentIssueDetails(ctx, schema, assessments, nodeScope.AssessmentsByPaths)
		if err != nil {
			setErr(err)
			return
		}
		fetchMu.Lock()
		issuesByPath = issues
		issueItemsByPath = items
		issueCount = totalIssues
		fetchMu.Unlock()
	}()
	go func() {
		defer wg.Done()
		refs := make([]ontology.NodeRef, 0, len(pathsList))
		for _, path := range pathsList {
			refs = append(refs, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
		}
		countResult, err := nodeScope.RelationCounts(ctx, noderead.RelationCountsRequest{
			Sources:           refs,
			Direction:         noderead.TraversalDirectionBoth,
			IncludeStructural: true,
			IncludeAmbient:    true,
		})
		if err != nil {
			setErr(err)
			return
		}
		counts := make(map[string]int, len(pathsList))
		for _, item := range countResult.Sources {
			counts[item.Source.NotePath] = item.Count
		}
		fetchMu.Lock()
		relationCountByPath = counts
		fetchMu.Unlock()
	}()
	go func() {
		defer wg.Done()
		statusByRef, err := s.identityStatusByItemRefs(ctx, result.Items, identityStatusField)
		if err != nil {
			setErr(err)
			return
		}
		fetchMu.Lock()
		identityStatusByItemRef = statusByRef
		fetchMu.Unlock()
	}()
	wg.Wait()
	if fetchErr != nil {
		return OntologyTypeResponse{}, fetchErr
	}
	if err := overlayIdentityStatuses(ctx, nodeScope, overlay, result.Items, identityStatusField, identityStatusByItemRef); err != nil {
		return OntologyTypeResponse{}, err
	}
	s.overlayNoteTags(overlay, pathsList, tagsByPath)

	items := make([]OntologyNoteListItem, 0, len(result.Items))
	for _, item := range result.Items {
		hasIssues := item.HasIssues || issuesByPath[item.NotePath]
		itemRef := item.Ref.String()
		if itemRef == "" {
			itemRef = item.NotePath
		}
		items = append(items, OntologyNoteListItem{
			Ref:            item.Ref,
			Path:           itemRef,
			Title:          item.Title,
			ResolvedType:   item.ResolvedType,
			HasIssues:      hasIssues,
			RelationCount:  relationCountByPath[item.NotePath],
			Tags:           tagsByPath[item.NotePath],
			UpdatedAt:      item.UpdatedAt,
			Issues:         issueItemsByPath[item.NotePath],
			IdentityStatus: identityStatusByItemRef[itemRef],
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].HasIssues != items[j].HasIssues {
			return items[i].HasIssues
		}
		return items[i].Title < items[j].Title
	})
	resp := OntologyTypeResponse{
		Type:       result.TypeDoc,
		Count:      result.Count,
		IssueCount: issueCount,
		Notes:      items,
	}
	if result.TypeDoc != nil && typeName != pseudoTypeAll && typeName != pseudoTypeIssues {
		if schema != nil {
			if text, err := guide.RenderMarkdownWithResolver(schema, []string{typeName}, s.ontologyCompanionResolver()); err == nil {
				resp.AuthoringGuide = text
			}
		}
	}
	return resp, nil
}

// ontologyTypeDoc answers `?notes=none`: the type documentation and indexed
// counts without per-note enrichment or the rendered authoring guide.
func (s *Server) ontologyTypeDoc(ctx context.Context, typeName string, overlay *noderead.ReadOverlay) (OntologyTypeResponse, error) {
	_, _, result, err := s.ontologyTypeInstances(ctx, typeName, overlay)
	if err != nil {
		return OntologyTypeResponse{}, err
	}
	return OntologyTypeResponse{Type: result.TypeDoc, Count: result.Count, IssueCount: result.IssueCount}, nil
}

// ontologyTypeInstances reads a type's documentation and instances from the
// indexed read model. A nil scope means the index store is unavailable.
func (s *Server) ontologyTypeInstances(ctx context.Context, typeName string, overlay *noderead.ReadOverlay) (*ontology.Schema, *noderead.Scope, ontology.TypeListResult, error) {
	_, defs, err := s.ontologyContext()
	if err != nil {
		return nil, nil, ontology.TypeListResult{}, err
	}
	store := s.runtime.Intel()
	if store == nil {
		return nil, nil, ontology.TypeListResult{}, nil
	}
	var schema *ontology.Schema
	if defs != nil {
		schema = defs.schema
	}
	nodeScope := noderead.NewService(s.cfg.VaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, noderead.ScopeOptions{ReadOverlay: overlay})
	result, err := nodeScope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName})
	if err != nil {
		return nil, nil, ontology.TypeListResult{}, err
	}
	if result.TypeDoc == nil && typeName != pseudoTypeAll && typeName != pseudoTypeIssues {
		return nil, nil, ontology.TypeListResult{}, fmt.Errorf("unknown ontology type %q", typeName)
	}
	return schema, nodeScope, result, nil
}

// ontologyCompanionResolver returns a guide.CompanionDocResolver backed by the
// server's vault config, mirroring the CLI authoring-guide behavior.
func (s *Server) ontologyCompanionResolver() guide.CompanionDocResolver {
	note := &obsidian.Note{}
	vaultDef := s.cfg.VaultDef
	return func(path string) (guide.CompanionDocMeta, bool) {
		content, err := note.GetContents(vaultDef, path)
		if err != nil {
			return guide.CompanionDocMeta{}, false
		}
		meta := guide.CompanionDocMeta{}
		if title, ok := note.Title(path); ok {
			meta.Title = strings.TrimSpace(title)
		}
		frontmatter := s.companionFrontmatter(path, content)
		if len(frontmatter) > 0 {
			for _, key := range []string{"summary", "synopsis", "about", "description", "desc", "doc"} {
				for current, value := range frontmatter {
					if !strings.EqualFold(current, key) {
						continue
					}
					if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
						meta.Summary = text
						break
					}
				}
				if meta.Summary != "" {
					break
				}
			}
		}
		return meta, true
	}
}

func (s *Server) companionFrontmatter(path, content string) map[string]interface{} {
	if s.catalog != nil {
		projection, isNote, err := s.catalog.projectNote(path, []byte(content), 0)
		if err == nil && isNote && projection.Status == noteformat.ProjectionStatusCurrent {
			return frontmatterFromProjection(projection)
		}
	}
	snapshot, markdown, err := s.markdownDocumentSnapshotCompat(path, content, time.Time{})
	if err != nil || !markdown || snapshot == nil {
		return nil
	}
	return snapshot.Frontmatter
}

func ontologyItemStartingRefs(items []ontology.NodeListItem, limit int) []string {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	out := make([]string, 0, min(limit, len(items)))
	for _, item := range items {
		out = append(out, item.Ref.String())
		if len(out) == limit {
			break
		}
	}
	return out
}

func uniqueOntologyItemNotePaths(items []ontology.NodeListItem) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.NotePath]; ok {
			continue
		}
		seen[item.NotePath] = struct{}{}
		out = append(out, item.NotePath)
	}
	return out
}

func (s *Server) ontologyInspect(ctx context.Context, notePath string) (OntologyInspectResponse, error) {
	service, defs, err := s.ontologyContext()
	if err != nil {
		return OntologyInspectResponse{}, err
	}
	resp := OntologyInspectResponse{
		OntologyAvailable: defs != nil,
	}
	if defs != nil {
		resp.SchemaHash = defs.schema.Hash
	}
	if service == nil {
		return resp, nil
	}
	note, err := service.Inspect(ctx, notePath)
	if err != nil {
		return resp, err
	}
	resp.Note = &note
	return resp, nil
}

func (s *Server) executeOntologyQuery(ctx context.Context, rawQuery string) (ontologyquery.Result, error) {
	return s.executeOntologyQueryOperation(ctx, rawQuery, nil, "", nil)
}

func (s *Server) executeOntologyQueryOperation(ctx context.Context, rawQuery string, variables map[string]any, operationName string, overlay *ontologyquery.ReadOverlay) (ontologyquery.Result, error) {
	trace := os.Getenv("RZM_TRACE_GRAPHQL") != ""
	start := time.Now()
	last := start
	traceStep := func(label string) {
		if !trace {
			return
		}
		now := time.Now()
		log.Printf("graphql execute trace step=%s delta=%s total=%s", label, now.Sub(last), now.Sub(start))
		last = now
	}
	defs, err := s.ontologyDefinitions()
	traceStep("definitions")
	if err != nil {
		return ontologyquery.Result{}, err
	}
	if defs == nil {
		return ontologyquery.Result{}, fmt.Errorf("ontology is unavailable")
	}
	if defs.exec == nil {
		if defs.execErr != nil {
			return ontologyquery.Result{}, fmt.Errorf("ontology query schema is unavailable: %w", defs.execErr)
		}
		return ontologyquery.Result{}, fmt.Errorf("ontology query schema is unavailable")
	}
	prepared, errs := ontologyquery.PrepareWithOptions(defs.exec, rawQuery, ontologyquery.PrepareOptions{
		Variables:     variables,
		OperationName: strings.TrimSpace(operationName),
	})
	traceStep("prepare")
	if len(errs) > 0 {
		return ontologyquery.Result{Errors: errs}, nil
	}
	deps, err := s.ontologyQueryDepsForPrepared(ctx, defs, prepared)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	traceStep("context")
	deps.ReadOverlay = overlay
	traceStep("deps")
	result := ontologyquery.ExecutePrepared(ctx, deps, defs.schema, defs.exec, prepared)
	traceStep("execute-prepared")
	return result, nil
}

func (s *Server) ontologyQueryDepsForPrepared(ctx context.Context, defs *ontologyDefinitions, prepared *ontologyquery.PreparedQuery) (ontologyquery.Deps, error) {
	store, exactSnapshot, err := s.waitForPreparedQuery(ctx, prepared)
	if err != nil {
		return ontologyquery.Deps{}, err
	}
	if store == nil {
		return ontologyquery.Deps{}, fmt.Errorf("ontology is unavailable")
	}
	service := ontology.NewService(s.cfg.VaultDef, s.defaultOntologyNoteReader(), store, defs.schema)
	return s.ontologyQueryDepsForExactPaths(service, nil, exactSnapshot), nil
}

func (s *Server) waitForPreparedQuery(ctx context.Context, prepared *ontologyquery.PreparedQuery) (*semdb.Store, *exactNoteReadSnapshot, error) {
	if s == nil || s.runtime == nil {
		return nil, nil, nil
	}
	gateCtx, cancel := context.WithTimeout(ctx, indexGateTimeout)
	defer cancel()
	notePaths, exactNotes := prepared.ExactNotePaths()
	rawOnly := exactNotes
	if !rawOnly {
		if err := s.runtime.WaitIndexReady(gateCtx, indexGateTimeout); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", errIndexInitializing, err)
		}
		notePaths, exactNotes = prepared.SelectedNotePaths()
	}
	if exactNotes {
		for _, notePath := range notePaths {
			if !s.exactNotePathOwned(notePath) {
				exactNotes = false
				break
			}
		}
	}
	if exactNotes {
		if err := s.runtime.WaitNoteReadReady(gateCtx, indexGateTimeout); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", errIndexInitializing, err)
		}
		ticker := time.NewTicker(exactNoteAvailabilityPollInterval)
		defer ticker.Stop()
		for exactNotes {
			for _, notePath := range notePaths {
				if !s.exactNotePathOwned(notePath) {
					exactNotes = false
					break
				}
			}
			if !exactNotes {
				break
			}
			store := s.runtime.Intel()
			if store == nil {
				select {
				case <-gateCtx.Done():
					return nil, nil, fmt.Errorf("%w: %v", errIndexInitializing, gateCtx.Err())
				case <-ticker.C:
					continue
				}
			}
			facts, loadErr := s.exactNoteFactsFromStore(gateCtx, store, notePaths)
			if loadErr != nil {
				return nil, nil, fmt.Errorf("%w: load exact note facts: %v", errIndexInitializing, loadErr)
			}
			allAccountedFor := true
			for _, notePath := range notePaths {
				if _, available := facts.MetadataRows[notePath]; available {
					continue
				}
				if !s.runtime.IndexReady() || !s.exactNoteSourceMissing(notePath) {
					allAccountedFor = false
					break
				}
			}
			if allAccountedFor && !rawOnly {
				defs, err := s.ontologyDefinitions()
				if err != nil || defs == nil || defs.schema == nil {
					return nil, nil, fmt.Errorf("%w: selected node schema is unavailable", errIndexInitializing)
				}
				catalogPaths, err := ontology.PublishedCatalogPaths(gateCtx, store, facts.MetadataRows, defs.schema.Hash)
				if err != nil {
					return nil, nil, fmt.Errorf("%w: load selected nodes: %v", errIndexInitializing, err)
				}
				for _, notePath := range notePaths {
					if !catalogPaths[notePath] && !s.exactNoteSourceMissing(notePath) {
						allAccountedFor = false
						break
					}
				}
				if allAccountedFor {
					return store, nil, nil
				}
			}
			if allAccountedFor {
				admitted := make(map[string]struct{}, len(notePaths))
				for _, notePath := range notePaths {
					admitted[notePath] = struct{}{}
				}
				return store, &exactNoteReadSnapshot{admittedPaths: admitted, facts: facts}, nil
			}
			select {
			case <-gateCtx.Done():
				return nil, nil, fmt.Errorf("%w: %v", errIndexInitializing, gateCtx.Err())
			case <-ticker.C:
			}
		}
	}
	err := s.runtime.WaitIndexReady(gateCtx, indexGateTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errIndexInitializing, err)
	}
	return s.runtime.Intel(), nil, nil
}

const exactNoteAvailabilityPollInterval = 250 * time.Millisecond

func (s *Server) exactNoteSourceMissing(notePath string) bool {
	reader := s.defaultOntologyNoteReader()
	if reader == nil {
		return false
	}
	_, err := reader.GetModTime(s.cfg.VaultDef, notePath)
	return err != nil && (errors.Is(err, os.ErrNotExist) || err.Error() == obsidian.NoteDoesNotExistError)
}

func (s *Server) requireOntologyDefinitions() (*ontologyDefinitions, error) {
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return nil, err
	}
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology is unavailable")
	}
	return defs, nil
}

func (s *Server) createOntologyEditSessionResponse(ctx context.Context, req OntologyEditSessionCreateRequest) (OntologyEditSessionResponse, error) {
	defs, err := s.requireOntologyDefinitions()
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	now := time.Now()
	canonicalOps, err := s.canonicalizeOntologyEditOps(ctx, defs, req.Ops)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	canonicalOps, err = s.reduceOntologyEditOps(ctx, defs, canonicalOps)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	editor, err := s.replayOntologyEditOps(defs, canonicalOps)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	session, err := s.createOntologyEditSession(now, req.SessionID, canonicalOps, editor)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	return s.buildOntologyEditSessionResponse(ctx, session, defs, false, false)
}

func (s *Server) getOntologyEditSessionResponse(ctx context.Context, sessionID string) (OntologyEditSessionResponse, error) {
	defs, err := s.requireOntologyDefinitions()
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	session, ok := s.ontologyEditSession(sessionID)
	if !ok {
		return OntologyEditSessionResponse{}, fmt.Errorf("edit session %q not found", sessionID)
	}
	resp, err := s.buildOntologyEditSessionResponse(ctx, session, defs, true, false)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	session.mu.Lock()
	session.UpdatedAt = time.Now()
	session.mu.Unlock()
	return resp, nil
}

func (s *Server) stageOntologyEditSessionResponse(ctx context.Context, sessionID string, req OntologyEditSessionStageRequest) (OntologyEditSessionResponse, error) {
	defs, err := s.requireOntologyDefinitions()
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	if len(req.Ops) == 0 {
		return OntologyEditSessionResponse{}, fmt.Errorf("ops are required")
	}
	session, err := s.ensureOntologyEditSession(ctx, defs, sessionID, req.Snapshot)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	if err := session.lockLive(); err != nil {
		return OntologyEditSessionResponse{}, err
	}
	defer session.mu.Unlock()
	receiptKey := ontologyEditRequestReceiptKey("stage", req.RequestID)
	requestHash := ontologyEditOpsHash(req.Ops) + fmt.Sprintf("|replace=%t", req.Replace)
	if receiptKey != "" {
		if receipt, ok := session.Receipts[receiptKey]; ok {
			if receipt.RequestHash != requestHash {
				return OntologyEditSessionResponse{}, fmt.Errorf("stage request id %q was reused for different operations", req.RequestID)
			}
			return receipt.Response, nil
		}
	}
	if req.ExpectedRevision != 0 && req.ExpectedRevision != session.Revision {
		return OntologyEditSessionResponse{}, fmt.Errorf("edit session revision changed: expected %d, current %d", req.ExpectedRevision, session.Revision)
	}
	baseDocuments := session.Editor.BaseDocuments()
	canonicalOps, err := s.canonicalizeOntologyEditOpsForSessionLocked(ctx, defs, session, req.Ops)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	candidateOps := canonicalOps
	if !req.Replace {
		candidateOps = append(cloneOntologyEditOps(session.Ops), canonicalOps...)
	}
	effectiveOps, err := s.reduceOntologyEditOps(ctx, defs, candidateOps)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	editor, err := s.replayOntologyEditOpsWithBases(defs, effectiveOps, baseDocuments)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	if coordinator := s.runtime.RepairPathCoordinator(); coordinator != nil {
		if err := coordinator.SetManualPaths(s.validationVaultIdentity(), session.ID, touchedPathsFromOps(effectiveOps)); err != nil {
			return OntologyEditSessionResponse{}, err
		}
	}
	session.Ops = cloneOntologyEditOps(effectiveOps)
	session.Editor = editor
	session.Rebased = false
	session.StalePaths = nil
	session.invalidateReadOverlayCacheLocked()
	session.Revision++
	session.UpdatedAt = time.Now()
	resp, err := s.buildOntologyEditSessionResponseLocked(ctx, session, defs, false, false)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	if receiptKey != "" {
		session.Receipts[receiptKey] = ontologyEditRequestReceipt{RequestHash: requestHash, Response: resp}
	}
	return resp, nil
}

func ontologyEditRequestReceiptKey(action, requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	return action + "|" + requestID
}

func (s *Server) previewOntologyEditSessionResponse(ctx context.Context, sessionID string, req OntologyEditSessionPreviewRequest) (OntologyEditSessionResponse, error) {
	defs, err := s.requireOntologyDefinitions()
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	session, err := s.ensureOntologyEditSession(ctx, defs, sessionID, req.Snapshot)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	resp, err := s.buildOntologyEditSessionResponse(ctx, session, defs, true, true)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	session.mu.Lock()
	session.UpdatedAt = time.Now()
	session.mu.Unlock()
	return resp, nil
}

func (s *Server) commitOntologyEditSessionResponse(ctx context.Context, sessionID string, req OntologyEditSessionCommitRequest) (OntologyEditSessionResponse, error) {
	if err := s.recoverPendingOntologyEditRepairs(ctx); err != nil {
		return OntologyEditSessionResponse{}, err
	}
	if receipt, ok, err := s.readOntologyCommitReceipt(sessionID, req.RequestID, req.ExpectedRevision, req.Snapshot); err != nil {
		return OntologyEditSessionResponse{}, err
	} else if ok {
		return receipt, nil
	}
	defs, err := s.requireOntologyDefinitions()
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	session, err := s.ensureOntologyEditSession(ctx, defs, sessionID, req.Snapshot)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	if err := session.lockLive(); err != nil {
		return OntologyEditSessionResponse{}, err
	}
	// An unchanged commit clears its operations before the separate retry
	// receipt is published. If that publication was canceled, a retry must use
	// the original submission hash retained in memory, not hash the now-clean
	// session and misclassify the same request ID as a different submission.
	var cachedUnchanged *ontologyEditRequestReceipt
	if key := ontologyEditRequestReceiptKey("commit", req.RequestID); key != "" {
		if cached, ok := session.Receipts[key]; ok && cached.Response.Outcome == ontology.CommitOutcomeUnchanged {
			if req.Snapshot != nil {
				if ontologyCommitSubmissionHash(sessionID, req.ExpectedRevision, req.Snapshot.Ops, req.Snapshot.BaseDocuments) != cached.RequestHash {
					session.mu.Unlock()
					return OntologyEditSessionResponse{}, fmt.Errorf("commit request id %q was reused for a different submission", req.RequestID)
				}
				cachedUnchanged = &cached
			} else if len(session.Ops) == 0 && session.Revision == cached.Response.Revision &&
				(req.ExpectedRevision == 0 || req.ExpectedRevision+1 == session.Revision) {
				cachedUnchanged = &cached
			}
		}
	}
	commitBases := []ontology.EditBaseDocument(nil)
	if session.Editor != nil {
		commitBases = session.Editor.BaseDocuments()
	}
	if len(commitBases) == 0 {
		commitBases, err = editBaseDocumentsFromOps(session.Ops)
		if err != nil {
			session.mu.Unlock()
			return OntologyEditSessionResponse{}, err
		}
	}
	commitSubmissionHash := ontologyCommitSubmissionHash(session.ID, req.ExpectedRevision, session.Ops, commitBases)
	session.mu.Unlock()
	var resp OntologyEditSessionResponse
	if cachedUnchanged != nil {
		resp = cachedUnchanged.Response
		commitSubmissionHash = cachedUnchanged.RequestHash
	} else {
		resp, err = s.commitTransientOntologyEditSession(ctx, session, defs, req.RequestID, req.ExpectedRevision)
		if err != nil {
			return OntologyEditSessionResponse{}, err
		}
	}
	if resp.Outcome == ontology.CommitOutcomeUnchanged && strings.TrimSpace(req.RequestID) != "" {
		if err := s.writeOntologyCommitReceipt(ctx, sessionID, req.RequestID, commitSubmissionHash, resp); err != nil {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("saved, but could not persist the retry receipt: %v", err))
		}
	}
	return resp, nil
}

func (s *Server) recoverPendingOntologyEditRepairs(ctx context.Context) error {
	vaultPath := strings.TrimSpace(s.cfg.VaultPath)
	if vaultPath == "" {
		vaultPath = s.cfg.VaultDef.BasePath()
	}
	noteReader := obsidian.NoteReader(&obsidian.Note{})
	runCtx := validate.RunContext{
		VaultDef: s.cfg.VaultDef, VaultPath: vaultPath, VaultMgr: s.cfg.Vault,
		NoteReader: noteReader, NoteMetadata: s.noteMetadata, MaxIssues: 20,
	}
	pending, err := validate.DetectPendingRepairJournals(runCtx)
	if err != nil {
		return fmt.Errorf("inspect pending edit repairs: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	planned := validate.Result{
		OK: true, SelectedChecks: []string{validate.CheckOntology},
		Checks:         []validate.CheckResult{{Name: validate.CheckOntology, OK: true}},
		RepairJournals: pending,
	}
	_, _, err = validate.ApplyRepairSession(ctx, planned, runCtx, validate.Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: indexing.ValidationProjectionPostApplyRefresher{
			VaultPath: vaultPath, VaultDef: s.cfg.VaultDef,
			NoteMetadata: s.noteMetadata, NoteReader: noteReader,
		},
	})
	if err != nil {
		return fmt.Errorf("recover pending edit repairs: %w", err)
	}
	s.invalidateNoteCaches()
	return nil
}

func (s *Server) commitTransientOntologyEditSession(ctx context.Context, session *ontologyEditSession, defs *ontologyDefinitions, requestID string, expectedRevision uint64) (OntologyEditSessionResponse, error) {
	if err := session.lockLive(); err != nil {
		return OntologyEditSessionResponse{}, err
	}
	defer session.mu.Unlock()
	bases := []ontology.EditBaseDocument(nil)
	if session.Editor != nil {
		bases = session.Editor.BaseDocuments()
	}
	if len(bases) == 0 {
		var err error
		bases, err = editBaseDocumentsFromOps(session.Ops)
		if err != nil {
			return OntologyEditSessionResponse{}, err
		}
	}
	receiptKey := ontologyEditRequestReceiptKey("commit", requestID)
	requestHash := ontologyCommitSubmissionHash(session.ID, expectedRevision, session.Ops, bases)
	if receiptKey != "" {
		if receipt, ok := session.Receipts[receiptKey]; ok {
			if receipt.RequestHash != requestHash {
				return OntologyEditSessionResponse{}, fmt.Errorf("commit request id %q was reused for a different submission", requestID)
			}
			return receipt.Response, nil
		}
	}
	if expectedRevision != 0 && expectedRevision != session.Revision {
		return OntologyEditSessionResponse{}, fmt.Errorf("edit session revision changed: expected %d, current %d", expectedRevision, session.Revision)
	}
	editor, err := s.replayOntologyEditOpsWithBases(defs, session.Ops, bases)
	if err != nil {
		return OntologyEditSessionResponse{}, fmt.Errorf("revalidate edits against the current ontology: %w", err)
	}
	session.Editor = editor
	ops := cloneOntologyEditOps(session.Ops)
	validationPlan, refLineage, replayConflicts, err := session.Editor.PreviewCurrentWithRefLineage(ctx)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	if len(replayConflicts) == 0 {
		replayConflicts, err = s.validateOntologyRelationOpsAgainstPlan(ctx, defs, ops, validationPlan)
		if err != nil {
			return OntologyEditSessionResponse{}, err
		}
	}
	if len(replayConflicts) > 0 {
		result := ontology.CommitResult{
			Outcome:   ontology.CommitOutcomeConflicted,
			Plan:      validationPlan,
			Conflicts: attachOntologyConflictOperationIDs(replayConflicts, ops),
		}
		resp := ontologyCommitResponse(session, result, ops)
		resp.Status = OntologyEditSessionStatusConflicted
		resp.Ops = ops
		resp.HasUncommittedChanges = true
		if receiptKey != "" {
			session.Receipts[receiptKey] = ontologyEditRequestReceipt{RequestHash: requestHash, Response: resp}
		}
		return resp, nil
	}
	result := ontology.CommitResult{Outcome: ontology.CommitOutcomeUnchanged, Plan: validationPlan}
	for _, file := range validationPlan.Files {
		if file.HasMaterialChange {
			result.Outcome = ontology.CommitOutcomeCommitted
			result.Applied = true
			break
		}
	}
	resp := ontologyCommitResponse(session, result, ops)
	var refreshedProjection *validate.PostApplyRefreshResult
	if result.Applied {
		resp.RefLineage = refLineage
		noteReader := obsidian.NoteReader(&obsidian.Note{})
		vaultPath := strings.TrimSpace(s.cfg.VaultPath)
		if vaultPath == "" {
			vaultPath = s.cfg.VaultDef.BasePath()
		}
		runCtx := validate.RunContext{
			VaultDef: s.cfg.VaultDef, VaultPath: vaultPath, VaultMgr: s.cfg.Vault,
			NoteReader: noteReader, NoteMetadata: s.noteMetadata, MaxIssues: 20,
		}
		planned, buildErr := validate.BuildOntologyEditSessionRepairResult(ctx, runCtx, session.ID, validationPlan)
		if buildErr != nil {
			return ontologyEditApplyConflictResponse(session, resp, ops, validationPlan, buildErr), nil
		}
		applyOptions := validate.Options{
			Fix: true, NonInteractive: true,
			PostApplyRefresher: indexing.ValidationProjectionPostApplyRefresher{
				VaultPath: vaultPath, VaultDef: s.cfg.VaultDef,
				NoteMetadata: s.noteMetadata, NoteReader: noteReader,
			},
		}
		if strings.TrimSpace(requestID) != "" {
			artifact, artifactErr := s.ontologyCommitReceiptArtifact(session.ID, requestID, requestHash, resp)
			if artifactErr != nil {
				return OntologyEditSessionResponse{}, artifactErr
			}
			applyOptions.CompletionArtifact = &validate.RepairCompletionArtifact{Path: artifact.Path, Content: artifact.Content}
		}
		_, execution, applyErr := validate.ApplyRepairSession(ctx, planned, runCtx, applyOptions)
		if execution != nil {
			refreshedProjection = execution.Refresh
		}
		if applyErr != nil {
			if execution == nil || execution.AppliedTransactions == 0 {
				return ontologyEditApplyConflictResponse(session, resp, ops, validationPlan, applyErr), nil
			}
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("saved, but repair completion needs recovery: %v", applyErr))
		}
		if execution == nil || execution.AppliedTransactions == 0 {
			return ontologyEditApplyConflictResponse(session, resp, ops, validationPlan, fmt.Errorf("repair transaction was not applied")), nil
		}
	}
	session.Ops = nil
	session.Editor = s.newOntologyEditSession(defs.schema)
	session.Rebased = false
	session.StalePaths = nil
	session.Revision++
	session.UpdatedAt = time.Now()
	resp.Revision = session.Revision
	if coordinator := s.runtime.RepairPathCoordinator(); coordinator != nil {
		coordinator.ReleaseManualSession(s.validationVaultIdentity(), session.ID)
	}
	if result.Applied {
		resp = s.refreshCommittedOntologyEdit(ctx, resp, refreshedProjection)
	}
	if receiptKey != "" {
		session.Receipts[receiptKey] = ontologyEditRequestReceipt{RequestHash: requestHash, Response: resp}
	}
	return resp, nil
}

func (s *Server) refreshCommittedOntologyEdit(ctx context.Context, resp OntologyEditSessionResponse, projection *validate.PostApplyRefreshResult) OntologyEditSessionResponse {
	// Semantic work has its own watcher input, independent of cache readers.
	if hub := s.runtime.WatchHubRef(); hub != nil {
		defer hub.EmitHintPaths(resp.TouchedPaths)
	}
	s.invalidateNodeProjectionCacheForPaths(resp.TouchedPaths)
	s.invalidateNoteCaches()
	var cacheRefreshErr error
	// Bring the cache that serves reads current before returning. Waiting for
	// the file watcher would let the next read return the pre-save source, and
	// an edit started from it would conflict with the saved file.
	noteCache, ok := s.defaultOntologyNoteReader().(noteCacheRefresher)
	if !ok && s.cfg.Cache != nil {
		noteCache, ok = s.cfg.Cache, true
	}
	if ok {
		for _, notePath := range resp.TouchedPaths {
			noteCache.MarkDirty(notePath, cache.DirtyModified)
		}
		cacheRefreshErr = noteCache.Refresh(ctx)
		if cacheRefreshErr != nil {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("refresh committed note cache: %v", cacheRefreshErr))
		}
	}
	s.runtime.RequestValidationRefresh()
	if cacheRefreshErr != nil {
		return resp
	}
	workspaces, err := s.refreshTouchedWorkspaces(ctx, resp.TouchedNodeRefs)
	if err != nil {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("refresh touched workspaces: %v", err))
		return resp
	}
	applySessionStatusToWorkspaces(workspaces, resp.Status, resp.StalePaths, resp.Rebased)
	resp.Workspaces = workspaces
	if len(resp.Warnings) == 0 && projection != nil {
		s.NotifyGlobalEvent(GlobalEventNodeChanged, NodeChangedEventData{
			Paths: projection.Paths, Domains: projection.Domains,
		})
	}
	return resp
}

func ontologyEditApplyConflictResponse(session *ontologyEditSession, resp OntologyEditSessionResponse, ops []OntologyEditOp, plan ontology.CommitPlan, err error) OntologyEditSessionResponse {
	resp.Outcome = ontology.CommitOutcomeConflicted
	resp.RefLineage = nil
	resp.Status = OntologyEditSessionStatusConflicted
	resp.Ops = ops
	resp.HasUncommittedChanges = len(ops) > 0
	resp.Revision = session.Revision
	for _, file := range plan.Files {
		if !file.HasMaterialChange {
			continue
		}
		resp.Conflicts = append(resp.Conflicts, ontology.ConflictReport{
			Kind: ontology.ConflictKindSourceChanged, NotePath: file.NotePath,
			Message: fmt.Sprintf("source changed before save: %v", err),
		})
	}
	resp.Conflicts = attachOntologyConflictOperationIDs(resp.Conflicts, ops)
	return resp
}

func ontologyCommitResponse(session *ontologyEditSession, result ontology.CommitResult, ops []OntologyEditOp) OntologyEditSessionResponse {
	touchedPaths := touchedPathsFromOps(ops)
	touchedNodeRefs := touchedNodeRefsFromOps(ops)
	revision := session.Revision
	if result.Applied {
		revision++
	}
	return OntologyEditSessionResponse{
		SessionID: session.ID, Revision: revision, Status: OntologyEditSessionStatusClean,
		Outcome: result.Outcome, TouchedPaths: touchedPaths, TouchedNodes: touchedNodesFromOps(ops),
		TouchedNodeRefs: touchedNodeRefs, ChangedFieldsByNode: changedFieldsByNode(ops),
		ChangedFieldsByNodeRef: changedFieldsByNodeRef(ops), CollectionChanges: collectionChangesFromOps(ops),
		BaseFingerprints: baseFingerprintsFromPlan(result.Plan), BaseDocuments: session.Editor.BaseDocuments(),
		StalePaths: mergeStringSets(stalePathsFromPlan(result.Plan), session.StalePaths),
		Rebased:    result.Rebased || session.Rebased, Conflicts: result.Conflicts,
		Warnings: append([]string(nil), result.Warnings...), HasUncommittedChanges: false,
		CreatedAt: session.CreatedAt, UpdatedAt: time.Now(), Plan: &result.Plan,
	}
}

func (s *Server) querySchema() (OntologyQuerySchemaResponse, error) {
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return OntologyQuerySchemaResponse{}, err
	}
	if defs == nil {
		return OntologyQuerySchemaResponse{}, nil
	}
	resp := OntologyQuerySchemaResponse{
		SchemaPresent:      true,
		QuerySchemaPresent: defs.exec != nil,
		SchemaHash:         defs.schema.Hash,
	}
	if defs.exec != nil {
		resp.SDL = defs.exec.SDL
	}
	if defs.execErr != nil {
		resp.Error = defs.execErr.Error()
	}
	return resp, nil
}

func (s *Server) readRenderedNote(ctx context.Context, rel string) (RenderedFileResponse, error) {
	if idx := strings.Index(rel, "#"); idx >= 0 {
		rel = rel[:idx]
	}
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, rel)
	if err != nil {
		return RenderedFileResponse{}, err
	}
	if readPath.rel == "" {
		return RenderedFileResponse{}, errors.New("empty path")
	}
	rel = readPath.rel
	if ignoreMatcher := s.runtime.IgnoreMatcher(); ignoreMatcher != nil {
		if ignoreMatcher.IsIgnored(rel, false) || ignoreMatcher.IsIgnored(readPath.resolvedRel, false) {
			return RenderedFileResponse{}, errors.New("path is ignored")
		}
	}
	info, err := os.Stat(readPath.abs)
	if err != nil {
		return RenderedFileResponse{}, err
	}
	if info.IsDir() {
		return RenderedFileResponse{}, errors.New("path is a directory")
	}
	if s.catalog != nil {
		if provider, unsupported := s.catalog.unsupportedNoteProjection(rel); unsupported {
			return RenderedFileResponse{}, errors.New(unsupportedProjectionMessage(rel, provider, "rendered file view"))
		}
	}
	kind, _ := s.classifyFile(rel)
	if kind != "note" {
		return RenderedFileResponse{}, errors.New("path is not a note")
	}
	contentBytes, err := os.ReadFile(readPath.abs)
	if err != nil {
		return RenderedFileResponse{}, err
	}
	content := string(contentBytes)

	title := s.lookupNoteTitle(ctx, rel)
	if title == "" {
		title = titleFromPath(rel)
	}
	snapshot, markdown, err := s.markdownDocumentSnapshotCompat(rel, content, info.ModTime())
	if err != nil {
		return RenderedFileResponse{}, err
	}
	var (
		resp         RenderedFileResponse
		sectionNodes []*ontology.SectionNode
	)
	if markdown {
		sectionNodes = snapshot.Sections
		resp = RenderedFileResponse{
			Path:        rel,
			Title:       title,
			Frontmatter: snapshot.Frontmatter,
			Content:     content,
			Rendered:    stripProjectedFrontmatter(snapshot),
			Links:       s.resolveMarkdownLinksCompat(rel, content),
			Embeds:      s.renderedEmbeds(ctx, rel, content),
			Sections:    mapRenderedSections(sectionNodes),
		}
	} else {
		projection, isNote, projectErr := s.catalog.projectNote(rel, contentBytes, info.ModTime().Unix())
		if projectErr != nil {
			return RenderedFileResponse{}, projectErr
		}
		if !isNote || projection.Status != noteformat.ProjectionStatusCurrent {
			return RenderedFileResponse{}, notemeta.ErrProjectionNotCurrent
		}
		if projection.Facts.Title != nil && strings.TrimSpace(projection.Facts.Title.Value) != "" {
			title = projection.Facts.Title.Value
		}
		resp = RenderedFileResponse{
			Path:        rel,
			Title:       title,
			Frontmatter: frontmatterFromProjection(projection),
			Content:     content,
			Rendered:    content,
			Links:       s.resolveProjectedLinksWithBase(rel, projection.Facts.DocumentBase, projection.Facts.Links),
			Embeds:      s.renderedProjectedEmbeds(ctx, rel, projection.Facts.Links),
		}
	}

	service, defs, _ := s.ontologyContext()
	if service != nil {
		if row, ok, err := service.ResolvedType(ctx, rel); err == nil && ok {
			resp.ResolvedType = row.TypeName
		}
	}
	if defs != nil && defs.schema != nil && resp.ResolvedType != "" && len(resp.Sections) > 0 {
		if noteType := defs.schema.Types[resp.ResolvedType]; noteType != nil {
			idMap := make(map[string]*RenderedSection)
			indexRenderedSections(resp.Sections, idMap)
			annotateTypedSections(sectionNodes, noteType, noteType.PropertyCase, defs.schema, idMap, "")
		}
	}
	return resp, nil
}

func (s *Server) workspaceGroups(ctx context.Context, nodeScope *noderead.Scope, notePath string, rendered RenderedFileResponse, inspected ontology.InspectNote) ([]NoteWorkspaceGroup, error) {
	seen := map[string]struct{}{}
	relationSeen := map[string]struct{}{}
	addSeen := func(kind, path string) {
		seen[kind+"|"+path] = struct{}{}
	}
	hasSeen := func(kind, path string) bool {
		_, ok := seen[kind+"|"+path]
		return ok
	}
	addRelationItem := func(items *[]NoteWorkspaceLink, relationName, targetPath, targetTitle, kind, resolvedType, provenance, direction string, structural bool) {
		if targetPath == "" || targetPath == notePath {
			return
		}
		key := fmt.Sprintf("%t|%s|%s|%s", structural, relationName, targetPath, direction)
		if _, ok := relationSeen[key]; ok {
			return
		}
		relationSeen[key] = struct{}{}
		*items = append(*items, NoteWorkspaceLink{
			Path:         targetPath,
			Title:        targetTitle,
			Kind:         kind,
			ResolvedType: resolvedType,
			RelationName: relationName,
			Provenance:   provenance,
			Direction:    direction,
			Structural:   structural,
		})
		addSeen("note", targetPath)
	}

	structural := make([]NoteWorkspaceLink, 0)
	ambient := make([]NoteWorkspaceLink, 0)
	persistedRelations, dbRelations, err := s.workspacePersistedRelations(ctx, notePath)
	if err != nil {
		return nil, err
	}
	if (!dbRelations || len(persistedRelations) == 0) && inspected.Assessment != nil {
		for _, relation := range inspected.Assessment.Relations {
			for _, target := range relation.Targets {
				if target.Structural {
					addRelationItem(&structural, relation.Name, target.Path, titleFromPath(target.Path), s.pathKind(target.Path), target.TypeName, target.Provenance, workspaceLinkDirection(false, target.Provenance), true)
				} else {
					addRelationItem(&ambient, relation.Name, target.Path, titleFromPath(target.Path), s.pathKind(target.Path), target.TypeName, target.Provenance, workspaceLinkDirection(false, target.Provenance), false)
				}
			}
		}
	}
	if len(structural) == 0 && len(persistedRelations) > 0 {
		for _, relation := range persistedRelations {
			targetPath := relation.DestinationPath
			targetType := relation.DestinationType
			inbound := strings.EqualFold(targetPath, notePath)
			if inbound {
				targetPath = relation.SourcePath
				targetType = ""
			}
			direction := workspaceLinkDirection(inbound, relation.Provenance)
			if relation.Structural {
				addRelationItem(&structural, relation.RelationName, targetPath, titleFromPath(targetPath), s.pathKind(targetPath), targetType, relation.Provenance, direction, true)
			} else {
				addRelationItem(&ambient, relation.RelationName, targetPath, titleFromPath(targetPath), s.pathKind(targetPath), targetType, relation.Provenance, direction, false)
			}
		}
	}

	var (
		backlinks   []obsidian.Backlink
		codeRelated []NoteWorkspaceLink
		fetchWG     sync.WaitGroup
		fetchMu     sync.Mutex
		fetchErr    error
	)
	setFetchErr := func(err error) {
		if err == nil {
			return
		}
		fetchMu.Lock()
		if fetchErr == nil {
			fetchErr = err
		}
		fetchMu.Unlock()
	}
	fetchWG.Add(2)
	go func() {
		defer fetchWG.Done()
		result, err := s.noteBacklinks(notePath)
		if err != nil {
			setFetchErr(err)
			return
		}
		fetchMu.Lock()
		backlinks = result
		fetchMu.Unlock()
	}()
	go func() {
		defer fetchWG.Done()
		result, err := s.workspaceCodeRelated(ctx, nodeScope, notePath)
		if err != nil {
			setFetchErr(err)
			return
		}
		fetchMu.Lock()
		codeRelated = result
		fetchMu.Unlock()
	}()
	fetchWG.Wait()
	if fetchErr != nil {
		return nil, fetchErr
	}
	backlinkItems := make([]NoteWorkspaceLink, 0, len(backlinks))
	backlinkSeen := make(map[string]struct{}, len(backlinks))
	for _, backlink := range backlinks {
		if _, ok := backlinkSeen[backlink.Referrer]; ok || hasSeen("note", backlink.Referrer) {
			continue
		}
		backlinkSeen[backlink.Referrer] = struct{}{}
		backlinkItems = append(backlinkItems, NoteWorkspaceLink{
			Path:       backlink.Referrer,
			Title:      titleFromPath(backlink.Referrer),
			Kind:       "note",
			Provenance: string(backlink.LinkType),
			Direction:  linkDirectionIncoming,
		})
		// Not marked seen: a note that also links back keeps its outbound row.
	}

	connectedItems := make([]NoteWorkspaceLink, 0)
	for _, link := range rendered.Links {
		if s.pathKind(link.Target) != "note" || hasSeen("note", link.Target) {
			continue
		}
		connectedItems = append(connectedItems, NoteWorkspaceLink{
			Path:       link.Target,
			Title:      titleFromPath(link.Target),
			Kind:       "note",
			Provenance: link.Kind,
			Direction:  linkDirectionOutgoing,
		})
		addSeen("note", link.Target)
	}

	allNotePaths := collectNotePathsFromLinks(structural, ambient, backlinkItems, connectedItems, codeRelated)
	titles, summaries := s.resolveNoteDisplay(ctx, allNotePaths)
	applyNoteDisplay(structural, titles, summaries)
	applyNoteDisplay(ambient, titles, summaries)
	applyNoteDisplay(backlinkItems, titles, summaries)
	applyNoteDisplay(connectedItems, titles, summaries)
	applyNoteDisplay(codeRelated, titles, summaries)

	groups := make([]NoteWorkspaceGroup, 0, 5)
	appendGroup := func(key, label string, items []NoteWorkspaceLink) {
		if len(items) == 0 {
			return
		}
		sortWorkspaceLinks(items)
		groups = append(groups, NoteWorkspaceGroup{Key: key, Label: label, Items: items})
	}
	appendGroup("structural", "Structural relations", structural)
	appendGroup("ambient", "Ambient relations", ambient)
	appendGroup("backlinks", "Backlinks", backlinkItems)
	appendGroup("connected", "Connected notes", connectedItems)
	appendGroup("code", "Linked code", codeRelated)
	return groups, nil
}

func (s *Server) workspacePersistedRelations(ctx context.Context, notePath string) ([]ontology.InspectRelation, bool, error) {
	if s == nil || s.runtime == nil {
		return nil, false, nil
	}
	store := s.runtime.Intel()
	if store == nil {
		return nil, false, nil
	}
	rows, err := store.OntologyEdgesForPath(ctx, notePath, true, "", 0)
	if err != nil {
		return nil, true, err
	}
	out := make([]ontology.InspectRelation, 0, len(rows))
	for _, row := range rows {
		out = append(out, ontology.InspectRelation{
			RelationName:    row.RelationName,
			SourcePath:      row.SrcPath,
			DestinationPath: row.DstPath,
			DestinationType: row.DstType,
			Provenance:      row.Provenance,
			Structural:      row.Structural,
		})
	}
	return out, true, nil
}

func (s *Server) sectionRelationGroups(ctx context.Context, notePath string, rendered RenderedFileResponse) (map[string][]NoteWorkspaceGroup, error) {
	sections := flattenRenderedSections(rendered.Sections)
	if len(sections) == 0 {
		return nil, nil
	}

	sectionByKey := make(map[string]RenderedSection, len(sections)*2)
	for _, section := range sections {
		if section.ID == "" {
			continue
		}
		for _, key := range sectionAnchorKeys(section) {
			sectionByKey[key] = section
		}
	}
	if len(sectionByKey) == 0 {
		return nil, nil
	}

	cache := s.notePathCache(ctx)
	type pendingSectionLink struct {
		sectionID  string
		fullTarget string
		targetPath string
		anchor     string
		provenance string
		cacheKey   string
	}
	pendingLinks := make([]pendingSectionLink, 0)
	targetLocators := make([]string, 0)
	seenTargetLocator := map[string]struct{}{}
	for _, section := range sections {
		if section.ID == "" || strings.TrimSpace(section.Content) == "" {
			continue
		}
		links := s.resolveMarkdownLinksCompat(notePath, section.Content)
		if len(links) == 0 {
			continue
		}
		seen := make(map[string]struct{})
		for _, link := range links {
			resolved, ok := resolveLinkDetail(cache, s.cfg.VaultDef, notePath, obsidian.LinkDetail{
				Target:   link.Text,
				LinkType: link.Kind,
			})
			if !ok || strings.TrimSpace(resolved.Path) == "" {
				continue
			}
			targetPath := string(paths.NormalizeNotePath(resolved.Path))
			if targetPath == notePath {
				continue
			}
			fullTarget := joinResolvedNoteTarget(resolved)
			key := fullTarget + "|" + link.Kind
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			cacheKey := ""
			if fragment := strings.TrimSpace(resolved.Fragment); fragment != "" {
				cacheKey = targetPath + "#" + fragment
				if _, ok := seenTargetLocator[cacheKey]; !ok {
					seenTargetLocator[cacheKey] = struct{}{}
					targetLocators = append(targetLocators, cacheKey)
				}
			}
			pendingLinks = append(pendingLinks, pendingSectionLink{
				sectionID:  section.ID,
				fullTarget: fullTarget,
				targetPath: targetPath,
				anchor:     joinAnchorFragment(resolved.Fragment, fragmentTypeFromResolvedTarget(resolved)),
				provenance: link.Kind,
				cacheKey:   cacheKey,
			})
		}
	}
	structuralByLocator := s.ontologyNodesByLocator(ctx, targetLocators)
	outboundBySection := make(map[string][]NoteWorkspaceLink)
	for _, pending := range pendingLinks {
		var structuralNode *StructuralNodeResponse
		if pending.cacheKey != "" {
			structuralNode = structuralByLocator[pending.cacheKey]
		}
		outboundBySection[pending.sectionID] = append(outboundBySection[pending.sectionID], NoteWorkspaceLink{
			Path:           pending.fullTarget,
			Title:          titleFromPath(pending.targetPath),
			Kind:           "note",
			ResolvedType:   "",
			Anchor:         pending.anchor,
			StructuralNode: structuralNode,
			Provenance:     pending.provenance,
			Direction:      linkDirectionOutgoing,
		})
	}

	backlinks, err := s.noteBacklinksDetailed(notePath)
	if err != nil {
		return nil, err
	}
	inboundBySection := make(map[string][]NoteWorkspaceLink)
	for _, backlink := range backlinks {
		key := sectionAnchorLookupKey(backlink.Fragment, backlink.LinkType)
		if key == "" {
			continue
		}
		section, ok := sectionByKey[key]
		if !ok || section.ID == "" {
			continue
		}
		inboundBySection[section.ID] = append(inboundBySection[section.ID], NoteWorkspaceLink{
			Path:       backlink.Referrer,
			Title:      titleFromPath(backlink.Referrer),
			Kind:       "note",
			Provenance: string(backlink.LinkType),
			Direction:  linkDirectionIncoming,
		})
	}

	allNotePaths := collectNotePathsFromLinks(mapValuesToSlices(outboundBySection)...)
	allNotePaths = append(allNotePaths, collectNotePathsFromLinks(mapValuesToSlices(inboundBySection)...)...)
	titles, summaries := s.resolveNoteDisplay(ctx, allNotePaths)

	result := make(map[string][]NoteWorkspaceGroup)
	for _, section := range sections {
		if section.ID == "" {
			continue
		}
		groups := make([]NoteWorkspaceGroup, 0, 2)
		if items := outboundBySection[section.ID]; len(items) > 0 {
			applyNoteDisplay(items, titles, summaries)
			sortWorkspaceLinks(items)
			groups = append(groups, NoteWorkspaceGroup{
				Key:   "section-outbound",
				Label: "Section links",
				Items: items,
			})
		}
		if items := inboundBySection[section.ID]; len(items) > 0 {
			applyNoteDisplay(items, titles, summaries)
			sortWorkspaceLinks(items)
			groups = append(groups, NoteWorkspaceGroup{
				Key:   "section-backlinks",
				Label: "Section backlinks",
				Items: items,
			})
		}
		if len(groups) > 0 {
			result[section.ID] = groups
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func structuralNodeFromOntologyNode(node codeanchor.IntelOntologyNode) *StructuralNodeResponse {
	if strings.TrimSpace(node.NodeID) == "" {
		return nil
	}
	locator := node.NodeKind
	if locator == "" {
		locator = string(ontology.NodeKindSection)
	}
	title := firstNonEmptyString(node.DisplayLabel, node.Title, titleFromPath(node.NotePath))
	return &StructuralNodeResponse{
		NodeID:       node.NodeID,
		Fragment:     node.Fragment,
		Locator:      locator,
		Title:        title,
		TypeName:     node.TypeName,
		NotePath:     node.NotePath,
		ParentNodeID: node.ParentNodeID,
	}
}

func (s *Server) ontologyNodesByLocator(ctx context.Context, locators []string) map[string]*StructuralNodeResponse {
	out := map[string]*StructuralNodeResponse{}
	if len(locators) == 0 || s == nil || s.runtime == nil {
		return out
	}
	store := s.runtime.Intel()
	if store == nil {
		return out
	}
	rows, err := store.OntologyNodesByNoteFragments(ctx, locators)
	if err != nil {
		return out
	}
	for locator, node := range rows {
		structural := structuralNodeFromOntologyNode(node)
		if structural == nil {
			continue
		}
		nodeResponse := *structural
		out[locator] = &nodeResponse
	}
	return out
}

func (s *Server) workspaceCodeRelated(ctx context.Context, nodeScope *noderead.Scope, notePath string) ([]NoteWorkspaceLink, error) {
	store := s.runtime.Intel()
	if store == nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]NoteWorkspaceLink, 0)

	// Primary source: code files bound to this note via code anchors
	// (symbol_fqn, call_file, path_prefix, and glob scopes).
	files, err := store.CodeFilesForNote(ctx, notePath)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file == "" || strings.EqualFold(file, notePath) {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		out = append(out, NoteWorkspaceLink{
			Path:       file,
			Title:      titleFromPath(file),
			Kind:       "code",
			Provenance: "code-anchor",
		})
	}

	// Secondary source: doc graph edges that point at code files. Keep this
	// behind noderead so workspace graph consumers use the same projected read
	// model as the graph APIs.
	if nodeScope == nil {
		nodeScope = noderead.NewService(s.cfg.VaultDef, &obsidian.Note{}, store, nil).NewScope(ctx, noderead.ScopeOptions{})
	}
	codeLinks, err := nodeScope.CodeGraphLinks(ctx, ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindNote}, 50)
	if err != nil {
		return nil, err
	}
	for _, link := range codeLinks {
		if _, ok := seen[link.Path]; ok {
			continue
		}
		seen[link.Path] = struct{}{}
		out = append(out, NoteWorkspaceLink{
			Path:       link.Path,
			Title:      titleFromPath(link.Path),
			Kind:       "code",
			Provenance: link.Provenance,
		})
	}
	return out, nil
}

func (s *Server) noteBacklinks(notePath string) ([]obsidian.Backlink, error) {
	if s != nil && s.runtime != nil {
		if store := s.runtime.Intel(); store != nil {
			rows, err := store.GraphDocBacklinksForPath(context.Background(), string(paths.NormalizeNotePath(notePath)), 0)
			if err == nil {
				out := make([]obsidian.Backlink, 0, len(rows))
				seen := make(map[string]obsidian.BacklinkType, len(rows))
				for _, row := range rows {
					if row.SrcPath == "" {
						continue
					}
					linkType := backlinkTypeForGraphKind(row.Kind)
					if existing, ok := seen[row.SrcPath]; ok && backlinkTypeRank(existing) >= backlinkTypeRank(linkType) {
						continue
					}
					seen[row.SrcPath] = linkType
				}
				for referrer, linkType := range seen {
					out = append(out, obsidian.Backlink{Referrer: referrer, LinkType: linkType})
				}
				sort.Slice(out, func(i, j int) bool {
					if out[i].Referrer == out[j].Referrer {
						return out[i].LinkType < out[j].LinkType
					}
					return out[i].Referrer < out[j].Referrer
				})
				return out, nil
			}
		}
	}
	backlinks, err := obsidian.CollectBacklinks(s.cfg.VaultDef, &obsidian.Note{}, []string{notePath}, obsidian.DefaultWikilinkOptions, nil)
	if err != nil {
		return nil, err
	}
	return backlinks[string(paths.NormalizeNotePath(notePath))], nil
}

func (s *Server) noteBacklinksDetailed(notePath string) ([]obsidian.Backlink, error) {
	notePath = string(paths.NormalizeNotePath(notePath))
	cache := s.notePathCache(context.Background())
	if s != nil && s.runtime != nil {
		if store := s.runtime.Intel(); store != nil {
			rows, err := store.GraphDocBacklinksForPath(context.Background(), notePath, 0)
			if err == nil {
				noteReader := &obsidian.Note{}
				out := make([]obsidian.Backlink, 0, len(rows))
				seen := make(map[string]struct{}, len(rows))
				for _, row := range rows {
					referrer := strings.TrimSpace(row.SrcPath)
					if referrer == "" {
						continue
					}
					links, readErr := s.resolvedNoteLinks(referrer, noteReader, cache)
					if readErr != nil {
						continue
					}
					for _, link := range links {
						if string(paths.NormalizeNotePath(link.Path)) != notePath {
							continue
						}
						key := referrer + "\x00" + string(link.LinkType) + "\x00" + link.Fragment
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
						out = append(out, obsidian.Backlink{
							Referrer: referrer,
							LinkType: link.LinkType,
							Fragment: link.Fragment,
						})
					}
				}
				sort.Slice(out, func(i, j int) bool {
					if out[i].Referrer == out[j].Referrer {
						if out[i].Fragment == out[j].Fragment {
							return out[i].LinkType < out[j].LinkType
						}
						return out[i].Fragment < out[j].Fragment
					}
					return out[i].Referrer < out[j].Referrer
				})
				return out, nil
			}
		}
	}
	backlinks, err := obsidian.CollectBacklinks(s.cfg.VaultDef, &obsidian.Note{}, []string{notePath}, obsidian.DefaultWikilinkOptions, nil)
	if err != nil {
		return nil, err
	}
	items := backlinks[notePath]
	sort.Slice(items, func(i, j int) bool {
		if items[i].Referrer == items[j].Referrer {
			return items[i].Fragment < items[j].Fragment
		}
		return items[i].Referrer < items[j].Referrer
	})
	return items, nil
}

func (s *Server) resolvedNoteLinks(referrer string, noteReader obsidian.NoteReader, cache *obsidian.NotePathCache) ([]resolvedNoteLink, error) {
	s.noteLinksMu.Lock()
	if s.noteLinksByPath != nil && time.Since(s.noteLinksAt) >= 5*time.Second {
		s.noteLinksByPath = nil
	}
	if links, ok := s.noteLinksByPath[referrer]; ok {
		s.noteLinksMu.Unlock()
		return links, nil
	}
	s.noteLinksMu.Unlock()

	content, err := noteReader.GetContents(s.cfg.VaultDef, referrer)
	if err != nil {
		return nil, err
	}
	scanned := s.resolveMarkdownLinksCompat(referrer, content)
	links := make([]resolvedNoteLink, 0, len(scanned))
	for _, link := range scanned {
		resolved, ok := resolveLinkDetail(cache, s.cfg.VaultDef, referrer, obsidian.LinkDetail{
			Target:   link.Text,
			LinkType: link.Kind,
		})
		if !ok || strings.TrimSpace(resolved.Path) == "" {
			continue
		}
		links = append(links, resolvedNoteLink{
			Path:     resolved.Path,
			Fragment: resolved.Fragment,
			LinkType: backlinkTypeForResolvedLink(link),
		})
	}

	s.noteLinksMu.Lock()
	if s.noteLinksByPath == nil {
		s.noteLinksByPath = make(map[string][]resolvedNoteLink)
	}
	s.noteLinksByPath[referrer] = links
	s.noteLinksAt = time.Now()
	s.noteLinksMu.Unlock()
	return links, nil
}

func (s *Server) renderedEmbeds(ctx context.Context, rel string, content string) []RenderedEmbed {
	links := s.markdownEmbedsCompat(rel, content)
	return s.renderedEmbedTargets(ctx, rel, links)
}

func (s *Server) renderedProjectedEmbeds(ctx context.Context, rel string, facts []noteformat.UnresolvedAuthoredLinkFact) []RenderedEmbed {
	links := make([]markdownEmbedCompat, 0, len(facts))
	for _, fact := range facts {
		if !fact.Embed {
			continue
		}
		linkType := ""
		switch fact.Resolution {
		case noteformat.LinkResolutionNoteReference:
			linkType = "wikilink"
		case noteformat.LinkResolutionRelativePath:
			linkType = "mdlink"
		default:
			continue
		}
		links = append(links, markdownEmbedCompat{Target: fact.ResolverInput, LinkType: linkType})
	}
	return s.renderedEmbedTargets(ctx, rel, links)
}

func (s *Server) renderedEmbedTargets(ctx context.Context, rel string, links []markdownEmbedCompat) []RenderedEmbed {
	cache := s.notePathCache(context.Background())
	out := make([]RenderedEmbed, 0)
	seen := map[string]struct{}{}
	targetPaths := make([]string, 0)
	for _, link := range links {
		target := link.Target
		resolved := false
		switch link.LinkType {
		case "wikilink":
			if notePath, ok := cache.ResolveNote(link.Target); ok {
				target = string(paths.NormalizeNotePath(notePath))
				resolved = true
			} else if notePath, ok := exactCachedNotePath(cache, link.Target); ok {
				target = notePath
				resolved = true
			}
		case "mdlink":
			if s.cfg.VaultDef.SupportsMarkdownLinks() {
				if notePath, ok := cache.ResolveMdLink(link.Target, rel); ok {
					target = string(paths.NormalizeNotePath(notePath))
					resolved = true
				}
			}
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, RenderedEmbed{
			Target:   target,
			Title:    titleFromPath(target),
			Kind:     s.pathKind(target),
			Resolved: resolved,
		})
		targetPaths = append(targetPaths, target)
	}
	titles, summaries := s.resolveNoteDisplay(ctx, targetPaths)
	if len(titles) > 0 || len(summaries) > 0 {
		for i := range out {
			if out[i].Kind != "note" {
				continue
			}
			if title, ok := titles[out[i].Target]; ok && title != "" {
				out[i].Title = title
			}
			if summary, ok := summaries[out[i].Target]; ok && summary != "" {
				out[i].Preview = summary
			}
		}
	}
	return out
}

func exactCachedNotePath(cache *obsidian.NotePathCache, target string) (string, bool) {
	if cache == nil {
		return "", false
	}
	path := filepath.ToSlash(strings.TrimSpace(target))
	extension := filepath.Ext(path)
	if extension == "" {
		return "", false
	}
	resolved, ok := cache.ResolveNote(strings.TrimSuffix(path, extension))
	if !ok || !strings.EqualFold(filepath.Ext(resolved), extension) {
		return "", false
	}
	return resolved, true
}

// pickIdentityStatusField selects the type's identity-level status field, if
// any. WHY: mirrors `web/src/components/ontologyFieldPicking.ts::pickIdentityFields`
// — both pick the first enum field whose lowercased name ends with "status".
// Keep these heuristics in lockstep; if you change one, change the other or the
// type-home card pill diverges from the identity strip inside the note pane.
func pickIdentityStatusField(typeDoc *ontology.TypeDoc) *ontology.FieldDoc {
	if typeDoc == nil {
		return nil
	}
	for i := range typeDoc.Fields {
		field := &typeDoc.Fields[i]
		if len(field.EnumValues) == 0 {
			continue
		}
		if strings.HasSuffix(strings.ToLower(field.Name), "status") {
			return field
		}
	}
	return nil
}

// overlayIdentityStatuses replaces indexed identity statuses for notes the edit
// session touches with their staged values, so the type list does not show a
// committed status beside staged titles and membership.
func overlayIdentityStatuses(ctx context.Context, scope *noderead.Scope, overlay *noderead.ReadOverlay, items []ontology.NodeListItem, field *ontology.FieldDoc, statusByRef map[string]string) error {
	if field == nil || overlay.Empty() {
		return nil
	}
	refs := make([]ontology.NodeRef, 0)
	for _, item := range items {
		if overlay.TouchesPath(item.NotePath) {
			refs = append(refs, item.Ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	records, err := scope.Hydrate(ctx, refs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
	if err != nil {
		return err
	}
	for _, record := range records {
		key := record.Ref.String()
		if values := scope.FieldValues(record, field.Name); len(values) > 0 && strings.TrimSpace(values[0]) != "" {
			statusByRef[key] = strings.TrimSpace(values[0])
		} else {
			delete(statusByRef, key)
		}
	}
	return nil
}

// overlayNoteTags replaces indexed tags for notes the edit session touches with
// the tags their staged source projects, the way the indexer derives them.
func (s *Server) overlayNoteTags(overlay *noderead.ReadOverlay, notePaths []string, tagsByPath map[string][]string) {
	if overlay.Empty() {
		return
	}
	for _, notePath := range notePaths {
		facts, ok := s.stagedNoteFacts(overlay, notePath)
		if !ok {
			continue
		}
		seen := map[string]struct{}{}
		tags := make([]string, 0, len(facts.Tags))
		for _, fact := range facts.Tags {
			tag := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(fact.Value, "#")))
			if _, dup := seen[tag]; tag == "" || dup {
				continue
			}
			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		tagsByPath[notePath] = tags
	}
}

// identityStatusByItemRefs reads the identity-status field value for each item
// in the type-home list, keyed by the item's canonical ref string. Returns an
// empty map when the type has no identity status field. Uses the same indexed
// read path (OntologyNodeFieldValuesByNodeIDs) the node-catalog reads use, so
// card values never disagree with the identity strip.
func (s *Server) identityStatusByItemRefs(ctx context.Context, items []ontology.NodeListItem, field *ontology.FieldDoc) (map[string]string, error) {
	if field == nil || len(items) == 0 {
		return map[string]string{}, nil
	}
	store := s.runtime.Intel()
	if store == nil {
		return map[string]string{}, nil
	}

	rootPaths := make([]string, 0, len(items))
	rootKeyByPath := make(map[string]string, len(items))
	locators := make([]string, 0, len(items))
	keyByLocator := make(map[string]string, len(items))
	for _, item := range items {
		key := item.Ref.String()
		path := item.NotePath
		if path == "" {
			path = item.Ref.NotePath
		}
		if key == "" {
			key = path
		}
		if path == "" || key == "" {
			continue
		}
		if strings.TrimSpace(item.Ref.Fragment) != "" {
			if _, ok := keyByLocator[key]; !ok {
				keyByLocator[key] = key
				locators = append(locators, key)
			}
		} else if _, ok := rootKeyByPath[path]; !ok {
			rootKeyByPath[path] = key
			rootPaths = append(rootPaths, path)
		}
	}
	if len(rootPaths) == 0 && len(locators) == 0 {
		return map[string]string{}, nil
	}
	nodeIDs := make([]string, 0, len(items))
	keyByNodeID := make(map[string]string, len(items))
	seenNodeIDs := make(map[string]struct{}, len(items))
	addNode := func(node codeanchor.IntelOntologyNode, key string) {
		if node.NodeID == "" || key == "" {
			return
		}
		if _, ok := seenNodeIDs[node.NodeID]; ok {
			return
		}
		seenNodeIDs[node.NodeID] = struct{}{}
		nodeIDs = append(nodeIDs, node.NodeID)
		keyByNodeID[node.NodeID] = key
	}

	if len(rootPaths) > 0 {
		nodes, err := store.OntologyNodesByPaths(ctx, rootPaths)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.Fragment != "" {
				continue
			}
			addNode(node, rootKeyByPath[node.NotePath])
		}
	}
	if len(locators) > 0 {
		nodesByLocator, err := store.OntologyNodesByNoteFragments(ctx, locators)
		if err != nil {
			return nil, err
		}
		for _, locator := range locators {
			node, ok := nodesByLocator[locator]
			if !ok {
				notePath, fragment, _ := strings.Cut(locator, "#")
				fragment = strings.TrimPrefix(fragment, "#")
				blockID := strings.TrimPrefix(fragment, "^")
				for _, candidate := range nodesByLocator {
					if candidate.NotePath == notePath && (candidate.Fragment == fragment || candidate.BlockID == blockID) {
						node = candidate
						ok = true
						break
					}
				}
			}
			if ok {
				addNode(node, keyByLocator[locator])
			}
		}
	}
	if len(nodeIDs) == 0 {
		return map[string]string{}, nil
	}
	rows, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, []string{field.Name})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		// Stored FieldName is normalized lowercase; FieldDoc.Name is camelCase.
		if !strings.EqualFold(row.FieldName, field.Name) {
			continue
		}
		key := keyByNodeID[row.NodeID]
		if key == "" {
			continue
		}
		if _, exists := out[key]; exists {
			continue
		}
		value := strings.TrimSpace(row.ValueText)
		if value == "" {
			continue
		}
		out[key] = value
	}
	return out, nil
}

func (s *Server) noteTagsByPaths(ctx context.Context, pathsList []string) (map[string][]string, error) {
	store := s.runtime.Intel()
	if store == nil || len(pathsList) == 0 {
		return map[string][]string{}, nil
	}
	rows, err := store.CurrentNoteTags(ctx, pathsList)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(pathsList))
	for _, row := range rows {
		out[row.NotePath] = append(out[row.NotePath], row.TagNorm)
	}
	for path, tags := range out {
		sort.Strings(tags)
		out[path] = tags
	}
	return out, nil
}

// assessmentLoader fetches decoded assessments for note paths that the caller
// has not already loaded. Scope.AssessmentsByPaths satisfies it and memoizes.
type assessmentLoader func(context.Context, []string) (map[string]*ontology.NoteAssessment, error)

// hydratedAssessments copies the non-nil decoded assessments and pulls in the
// fix targets referenced from outside the requested set, so fix suggestions can
// look up neighbours and both counting surfaces see the same population.
func hydratedAssessments(ctx context.Context, assessments map[string]*ontology.NoteAssessment, load assessmentLoader) (map[string]*ontology.NoteAssessment, error) {
	all := make(map[string]*ontology.NoteAssessment, len(assessments))
	for notePath, assessment := range assessments {
		if assessment == nil {
			continue
		}
		all[notePath] = assessment
	}
	if err := hydrateFixTargetAssessments(ctx, all, load); err != nil {
		return nil, err
	}
	return all, nil
}

// assessmentIssueDetails returns both a bool map for filtering and a flattened
// list of issue messages per note so list items can surface concrete problems
// (e.g. on the Issues home screen). Per-note lists include the top-level
// assessment Issues followed by field- and relation-scoped issues.
func assessmentIssueDetails(ctx context.Context, schema *ontology.Schema, assessments map[string]*ontology.NoteAssessment, load assessmentLoader) (map[string]bool, map[string][]OntologyNoteIssueItem, int, int, error) {
	all, err := hydratedAssessments(ctx, assessments, load)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	issuesByPath := make(map[string]bool, len(all))
	itemsByPath := make(map[string][]OntologyNoteIssueItem, len(all))
	ambiguousCount := 0
	issueCount := 0
	for notePath, assessment := range all {
		if len(assessment.CandidateTypes) > 1 {
			ambiguousCount++
		}
		if ontology.AssessmentHasIssues(assessment) {
			issuesByPath[notePath] = true
			itemsByPath[notePath] = collectAssessmentIssues(assessment, schema, all)
			issueCount++
		}
	}
	return issuesByPath, itemsByPath, ambiguousCount, issueCount, nil
}

func hydrateFixTargetAssessments(ctx context.Context, allAssessments map[string]*ontology.NoteAssessment, load assessmentLoader) error {
	if len(allAssessments) == 0 || load == nil {
		return nil
	}
	missing := missingFixTargetPaths(allAssessments)
	if len(missing) == 0 {
		return nil
	}
	loaded, err := load(ctx, missing)
	if err != nil {
		return err
	}
	for path, assessment := range loaded {
		if assessment == nil {
			continue
		}
		allAssessments[path] = assessment
	}
	return nil
}

func missingFixTargetPaths(allAssessments map[string]*ontology.NoteAssessment) []string {
	if len(allAssessments) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(allAssessments))
	for path := range allAssessments {
		seen[path] = struct{}{}
	}
	var out []string
	for _, assessment := range allAssessments {
		for _, target := range assessmentFixTargets(assessment) {
			if target == "" {
				continue
			}
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}
			out = append(out, target)
		}
	}
	sort.Strings(out)
	return out
}

func assessmentFixTargets(assessment *ontology.NoteAssessment) []string {
	seen := make(map[string]struct{})
	var out []string
	visitAssessmentIssues(assessment, func(issue ontology.ValidationIssue, _ string) {
		target := strings.TrimSpace(issue.FixTarget)
		if target == "" {
			return
		}
		if _, ok := seen[target]; ok {
			return
		}
		seen[target] = struct{}{}
		out = append(out, target)
	})
	return out
}

func visitAssessmentIssues(assessment *ontology.NoteAssessment, visit func(ontology.ValidationIssue, string)) {
	if assessment == nil {
		return
	}
	visitGroup := func(issues []ontology.ValidationIssue, fallbackField string) {
		for _, issue := range issues {
			field := issue.FieldName
			if field == "" {
				field = fallbackField
			}
			visit(issue, field)
		}
	}
	visitGroup(assessment.Issues, "")
	for _, field := range assessment.Fields {
		visitGroup(field.Issues, field.Name)
	}
	for _, relation := range assessment.Relations {
		visitGroup(relation.Issues, relation.Name)
	}
}

func collectAssessmentIssues(assessment *ontology.NoteAssessment, schema *ontology.Schema, allAssessments map[string]*ontology.NoteAssessment) []OntologyNoteIssueItem {
	if assessment == nil {
		return nil
	}

	// Build a fix lookup keyed by the concrete issue instance for fast matching.
	fixes := ontology.SuggestFixes(assessment, schema, allAssessments)
	fixByKey := make(map[string]*ontology.FixSuggestion, len(fixes))
	for i := range fixes {
		fix := &fixes[i]
		key := fix.IssueKey
		fixByKey[key] = fix
	}

	out := make([]OntologyNoteIssueItem, 0)
	visitAssessmentIssues(assessment, func(issue ontology.ValidationIssue, fieldName string) {
		item := OntologyNoteIssueItem{
			Code:    issue.Code,
			Field:   fieldName,
			Message: issue.Message,
		}
		if fix, ok := fixByKey[ontology.ValidationIssueKey(issue, fieldName)]; ok {
			item.Fixable = true
			item.FixOps = fixOpsToEditOps(fix.Ops)
		}
		out = append(out, item)
	})
	return out
}

func fixOpsToEditOps(ops []ontology.FixOp) []OntologyEditOp {
	if len(ops) == 0 {
		return nil
	}
	out := make([]OntologyEditOp, len(ops))
	for i, op := range ops {
		out[i] = OntologyEditOp{
			Kind:       op.Kind,
			Path:       op.Path,
			NodeID:     op.NodeID,
			Structural: op.Structural,
			Field:      op.Field,
			Value:      op.Value,
			Values:     op.Values,
			BlockID:    op.BlockID,
		}
	}
	return out
}

// mapRenderedSections converts the parsed SectionNode tree into the serialized
// RenderedSection tree.
//
// Breaking change (typed sections feature): Content is the section's *own*
// content — everything between the heading line and the first descendant
// section, plus any trailing content after the last descendant. Consumers that
// want to render the full subtree must recurse into Children themselves and
// concatenate. This matches how the typed-section frontend walker renders so
// that descendants aren't double-rendered.
func mapRenderedSections(nodes []*ontology.SectionNode) []RenderedSection {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]RenderedSection, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		out = append(out, RenderedSection{
			ID:       node.ID,
			Title:    node.Title,
			Level:    node.Level,
			Content:  ontology.SectionOwnContent(node),
			Children: mapRenderedSections(node.Children),
			BlockID:  node.BlockID,
		})
	}
	return out
}

// indexRenderedSections builds an ID→*RenderedSection lookup over the rendered
// tree. The caller must not mutate slice lengths while the pointers are in use
// (the rendered tree is frozen after construction, so this is safe in practice).
func indexRenderedSections(sections []RenderedSection, out map[string]*RenderedSection) {
	for i := range sections {
		out[sections[i].ID] = &sections[i]
		indexRenderedSections(sections[i].Children, out)
	}
}

// annotateTypedSections walks the ontology schema for an enclosing parent type
// (either a note type at the top level, or a section type during recursion)
// and decorates matching RenderedSections with TypeName, Properties, and the
// interpolated preview template when @preview is declared.
//
// The enclosing note's PropertyCase is threaded through so section scalar
// fields resolve their inline keys the same way the query resolver and the
// index-time assessor do.
func annotateTypedSections(
	nodes []*ontology.SectionNode,
	parentType *ontology.NoteType,
	notePropertyCase ontology.PropertyCase,
	schema *ontology.Schema,
	idMap map[string]*RenderedSection,
	parentFieldPath string,
) {
	if parentType == nil || schema == nil || len(nodes) == 0 {
		return
	}
	for _, field := range parentType.Fields {
		if field == nil || field.Kind != ontology.FieldKindSection {
			continue
		}
		matches := matchWebSectionNodesForField(nodes, field)
		if len(matches) == 0 {
			continue
		}
		sectionType := schema.Types[field.TypeName]
		for _, match := range matches {
			if match == nil {
				continue
			}
			rendered := idMap[match.ID]
			if rendered == nil {
				continue
			}
			rendered.TypeName = field.TypeName
			rendered.FieldName = field.Name
			rendered.FieldPath = joinFieldPath(parentFieldPath, field.Name)
			rendered.FieldList = field.List
			rendered.SectionDisplay = field.SectionDisplay
			if sectionType == nil {
				continue
			}
			rendered.IdentifierField = identifierFieldName(sectionType)
			props := extractSectionPropertyValues(match, sectionType, notePropertyCase)
			if len(props) > 0 {
				rendered.Properties = props
			}
			if sectionType.Preview != nil {
				rendered.PreviewTemplate = interpolatePreviewTemplate(sectionType.Preview.Template, props, match.Title)
				rendered.Collapsed = sectionType.Preview.Collapsed
			}
			annotateTypedSections(
				match.Children,
				sectionType,
				notePropertyCase,
				schema,
				idMap,
				rendered.FieldPath,
			)
		}
	}
}

func joinFieldPath(parent, field string) string {
	if strings.TrimSpace(parent) == "" {
		return field
	}
	return parent + "." + field
}

func buildStructuralView(rendered RenderedFileResponse, typeDoc *ontology.TypeDoc) *StructuralViewResponse {
	if len(rendered.Sections) == 0 {
		return nil
	}
	sections := buildStructuralSections(rendered.Sections)
	if len(sections) == 0 {
		return nil
	}
	rootContent, rootChildren := structuralFileRoot(rendered, sections)
	view := &StructuralViewResponse{
		DefaultView: "markdown",
		Root: StructuralNodeResponse{
			NodeID:   rendered.Path,
			Locator:  "FILE",
			Title:    rendered.Title,
			TypeName: rendered.ResolvedType,
			NotePath: rendered.Path,
			Content:  rootContent,
			Children: rootChildren,
		},
	}
	if rendered.ResolvedType != "" {
		view.DefaultView = "structural"
	}
	if typeDoc == nil {
		return view
	}
	view.Tabs = buildStructuralTabs(sections, typeDoc.Fields)
	if len(view.Tabs) == 0 {
		view.DefaultView = "markdown"
	}
	return view
}

func structuralFileRoot(rendered RenderedFileResponse, sections []StructuralNodeResponse) (string, []StructuralNodeResponse) {
	if len(sections) == 1 && sections[0].Level == ontology.SectionLevelH1 {
		root := sections[0]
		if len(root.Children) > 0 {
			return root.Content, root.Children
		}
		return root.Content, nil
	}
	return extractStructuralPreface(rendered.Rendered), sections
}

func extractStructuralPreface(markdown string) string {
	if strings.TrimSpace(markdown) == "" {
		return ""
	}
	lines := strings.Split(markdown, "\n")
	offset := 0
	inCode := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCode = !inCode
			offset += len(line) + 1
			continue
		}
		if !inCode {
			left := strings.TrimLeft(line, " ")
			leading := len(line) - len(left)
			if leading <= 3 && strings.HasPrefix(left, "#") {
				hashes := 0
				for hashes < len(left) && left[hashes] == '#' {
					hashes++
				}
				if hashes > 0 && hashes <= 6 {
					title := strings.TrimSpace(strings.TrimRight(left[hashes:], "# \t"))
					if title != "" {
						return strings.TrimSpace(markdown[:offset])
					}
				}
			}
		}
		offset += len(line) + 1
	}
	return strings.TrimSpace(markdown)
}

func buildSectionStructuralView(section RenderedSection, typeDoc *ontology.TypeDoc) *StructuralViewResponse {
	nodes := buildStructuralSections([]RenderedSection{section})
	if len(nodes) == 0 {
		return nil
	}
	root := nodes[0]
	root.Locator = firstNonEmpty(typeDocLocator(typeDoc), "SECTION")
	view := &StructuralViewResponse{
		DefaultView: "markdown",
		Root:        root,
	}
	if section.TypeName != "" {
		view.DefaultView = "structural"
	}
	if typeDoc == nil {
		return view
	}
	view.Tabs = buildStructuralTabs(root.Children, typeDoc.Fields)
	if len(view.Tabs) == 0 {
		view.DefaultView = "markdown"
	}
	return view
}

func buildStructuralTabs(nodes []StructuralNodeResponse, fields []ontology.FieldDoc) []StructuralTabResponse {
	var tabs []StructuralTabResponse
	for _, field := range fields {
		if field.Kind != ontology.FieldKindSection {
			continue
		}
		items := collectStructuralMatches(nodes, field)
		if len(items) == 0 {
			continue
		}
		tab := StructuralTabResponse{
			Key:       field.Name,
			FieldName: field.Name,
			Label:     firstNonEmpty(field.Description, field.Name),
			Count:     len(items),
			Nodes:     items,
		}
		if strings.TrimSpace(field.SectionHeading) != "" {
			tab.Label = field.SectionHeading
		}
		if tab.Label == "" {
			tab.Label = field.Name
		}
		tabs = append(tabs, tab)
	}
	return tabs
}

func buildStructuralSections(sections []RenderedSection) []StructuralNodeResponse {
	if len(sections) == 0 {
		return nil
	}
	out := make([]StructuralNodeResponse, 0, len(sections))
	for _, section := range sections {
		node := StructuralNodeResponse{
			NodeID:          section.ID,
			Fragment:        renderedSectionFragment(section),
			Locator:         "SECTION",
			Title:           section.Title,
			TypeName:        section.TypeName,
			Level:           section.Level,
			Content:         section.Content,
			NotePath:        renderedSectionNotePath(section),
			ParentNodeID:    section.ParentID,
			FieldName:       section.FieldName,
			FieldPath:       section.FieldPath,
			FieldList:       section.FieldList,
			SectionDisplay:  section.SectionDisplay,
			Properties:      section.Properties,
			IdentifierField: section.IdentifierField,
			Preview:         section.PreviewTemplate,
			PreviewTemplate: section.PreviewTemplate,
			Collapsed:       section.Collapsed,
			Children:        buildStructuralSections(section.Children),
		}
		out = append(out, node)
	}
	return out
}

func collectStructuralMatches(nodes []StructuralNodeResponse, field ontology.FieldDoc) []StructuralNodeResponse {
	if len(nodes) == 0 {
		return nil
	}
	if strings.TrimSpace(field.SectionHeading) != "" {
		out := make([]StructuralNodeResponse, 0)
		var walk func([]StructuralNodeResponse)
		walk = func(current []StructuralNodeResponse) {
			for _, node := range current {
				if node.Level == field.SectionLevel && strings.TrimSpace(node.Title) == strings.TrimSpace(field.SectionHeading) {
					out = append(out, node)
				}
				if len(node.Children) > 0 {
					walk(node.Children)
				}
			}
		}
		walk(nodes)
		return out
	}
	if !field.List {
		return nil
	}
	roots := structuralRoots(nodes)
	out := make([]StructuralNodeResponse, 0, len(roots))
	for _, node := range roots {
		if node.Level == field.SectionLevel {
			out = append(out, node)
		}
	}
	return out
}

func structuralRoots(nodes []StructuralNodeResponse) []StructuralNodeResponse {
	if len(nodes) != 1 {
		return nodes
	}
	only := nodes[0]
	if only.Level == ontology.SectionLevelH1 && len(only.Children) > 0 {
		return only.Children
	}
	return nodes
}

func renderedSectionNotePath(section RenderedSection) string {
	parts := strings.SplitN(section.ID, "#", 2)
	return parts[0]
}

func renderedSectionFragment(section RenderedSection) string {
	parts := strings.SplitN(section.ID, "#", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func identifierFieldName(noteType *ontology.NoteType) string {
	if noteType == nil {
		return ""
	}
	for _, field := range noteType.Fields {
		if field != nil && field.IsPreferredIdentifier {
			return field.Name
		}
	}
	for _, field := range noteType.Fields {
		if field != nil && field.IsIdentifier {
			return field.Name
		}
	}
	return ""
}

// matchWebSectionNodesForField mirrors the query resolver's matcher: fields
// with an explicit heading resolve via FindMatchingSections; list-typed fields
// without a heading match every direct child at the requested level in
// document order.
func matchWebSectionNodesForField(nodes []*ontology.SectionNode, field *ontology.Field) []*ontology.SectionNode {
	if field == nil {
		return nil
	}
	if strings.TrimSpace(field.SectionHeading) != "" {
		return ontology.FindMatchingSections(nodes, field.SectionLevel, field.SectionHeading)
	}
	if !field.List {
		return nil
	}
	nodes = ontology.UnwrapSingleH1SectionRoot(nodes)
	out := make([]*ontology.SectionNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.Level == field.SectionLevel {
			out = append(out, node)
		}
	}
	return out
}

// extractSectionPropertyValues reads a section's inline properties (scoped to
// its own content) and builds a display map keyed by declared field name. List
// values are joined with ", " so the frontend renders them in a single string.
func extractSectionPropertyValues(node *ontology.SectionNode, sectionType *ontology.NoteType, notePropertyCase ontology.PropertyCase) map[string]string {
	if node == nil || sectionType == nil {
		return nil
	}
	inline := ontology.SectionInlineProperties(ontology.SectionOwnContent(node))
	if len(inline) == 0 {
		return nil
	}
	out := make(map[string]string)
	for _, field := range sectionType.Fields {
		if field == nil {
			continue
		}
		if field.Kind != ontology.FieldKindScalar && field.Kind != ontology.FieldKindEnum {
			continue
		}
		key := ontology.DefaultPropertyName(field.Name, notePropertyCase)
		values := caseInsensitiveInlineValues(inline, key)
		if len(values) == 0 {
			continue
		}
		out[field.Name] = strings.Join(values, ", ")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func caseInsensitiveInlineValues(values map[string][]string, key string) []string {
	if len(values) == 0 {
		return nil
	}
	if v, ok := values[key]; ok {
		return v
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for k, v := range values {
		if strings.ToLower(strings.TrimSpace(k)) == needle {
			return v
		}
	}
	return nil
}

// interpolatePreviewTemplate substitutes `{{name}}` placeholders in the schema
// template with resolved values. `{{title}}` comes from the section's heading
// text; other names come from the extracted properties map. Unknown names are
// left as literal `{{name}}` — the compiler rejects them, so this path is only
// hit for valid templates.
func interpolatePreviewTemplate(template string, props map[string]string, title string) string {
	if template == "" {
		return ""
	}
	return previewPlaceholderInterpolator.ReplaceAllStringFunc(template, func(match string) string {
		name := strings.TrimSpace(previewPlaceholderInterpolator.FindStringSubmatch(match)[1])
		if name == "title" {
			return title
		}
		if value, ok := props[name]; ok {
			return value
		}
		return match
	})
}

var previewPlaceholderInterpolator = regexp.MustCompile(`{{\s*([A-Za-z_][A-Za-z0-9_]*)\s*}}`)

func stripProjectedFrontmatter(snapshot *ontology.DocumentSnapshot) string {
	if snapshot == nil {
		return ""
	}
	content := snapshot.Content
	range_ := snapshot.FrontmatterRange
	if range_.Valid(len(content)) && range_.End > range_.Start {
		content = content[range_.End:]
	}
	return strings.TrimLeft(content, "\r\n")
}

func compactSnippet(content string, max int) string {
	content = strings.TrimSpace(content)
	content = strings.Join(strings.Fields(content), " ")
	if max <= 0 || len(content) <= max {
		return content
	}
	return strings.TrimSpace(content[:max]) + "..."
}

func (s *Server) pathKind(p string) string {
	kind, _ := s.classifyFile(p)
	if kind == "" {
		return "file"
	}
	return kind
}

func sortWorkspaceLinks(items []NoteWorkspaceLink) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].ResolvedType != items[j].ResolvedType {
			return items[i].ResolvedType < items[j].ResolvedType
		}
		return items[i].Title < items[j].Title
	})
}

func flattenRenderedSections(sections []RenderedSection) []RenderedSection {
	if len(sections) == 0 {
		return nil
	}
	out := make([]RenderedSection, 0)
	var walk func([]RenderedSection)
	walk = func(current []RenderedSection) {
		for _, section := range current {
			out = append(out, section)
			if len(section.Children) > 0 {
				walk(section.Children)
			}
		}
	}
	walk(sections)
	return out
}

func (s *Server) invalidateNoteCaches() {
	if s == nil {
		return
	}
	s.noteMu.Lock()
	s.noteCache = nil
	s.noteMu.Unlock()

	s.invalidateNoteLinksCache()
}

func (s *Server) invalidateNoteLinksCache() {
	if s == nil {
		return
	}
	s.noteLinksMu.Lock()
	s.noteLinksByPath = nil
	s.noteLinksAt = time.Time{}
	s.noteLinksMu.Unlock()
}

func sectionAnchorKeys(section RenderedSection) []string {
	keys := make([]string, 0, 2)
	if blockID := strings.TrimSpace(section.BlockID); blockID != "" {
		keys = append(keys, sectionAnchorLookupKey(blockID, obsidian.BacklinkTypeBlock))
	}
	if title := strings.TrimSpace(section.Title); title != "" {
		keys = append(keys, sectionAnchorLookupKey(title, obsidian.BacklinkTypeHeading))
	}
	return keys
}

func schemaTypeDoc(defs *ontologyDefinitions, typeName string) *ontology.TypeDoc {
	if defs == nil || defs.schema == nil || strings.TrimSpace(typeName) == "" {
		return nil
	}
	docs, err := ontology.SchemaDocs(defs.schema, typeName)
	if err != nil || len(docs) == 0 {
		return nil
	}
	doc := docs[0]
	return &doc
}

func typeDocLocator(doc *ontology.TypeDoc) string {
	if doc == nil {
		return ""
	}
	return doc.Locator
}

func sectionAnchorLookupKey(fragment string, linkType obsidian.BacklinkType) string {
	fragment = strings.TrimSpace(strings.TrimPrefix(fragment, "#"))
	if fragment == "" {
		return ""
	}
	// Caret fragments are canonical block targets even when the source scanner
	// classifies markdown anchors as "heading".
	if strings.HasPrefix(fragment, "^") {
		return "block:" + strings.TrimPrefix(fragment, "^")
	}
	switch linkType {
	case obsidian.BacklinkTypeBlock:
		return "block:" + fragment
	case obsidian.BacklinkTypeHeading:
		return "heading:" + slugifyHeading(fragment)
	default:
		if strings.HasPrefix(strings.TrimSpace(fragment), "^") {
			return "block:" + strings.TrimPrefix(strings.TrimSpace(fragment), "^")
		}
		return "heading:" + slugifyHeading(fragment)
	}
}

func resolveLinkDetail(cache *obsidian.NotePathCache, vaultDef obsidian.VaultDefinition, fromNote string, link obsidian.LinkDetail) (obsidian.ResolvedNoteTarget, bool) {
	if cache == nil {
		return obsidian.ResolvedNoteTarget{}, false
	}
	switch link.LinkType {
	case "wikilink":
		return cache.ResolveNoteTarget(link.Target)
	case "mdlink":
		if vaultDef.SupportsMarkdownLinks() {
			return cache.ResolveMdLinkTarget(link.Target, fromNote)
		}
	}
	return obsidian.ResolvedNoteTarget{}, false
}

func fragmentTypeFromResolvedTarget(target obsidian.ResolvedNoteTarget) string {
	fragment := strings.TrimSpace(target.Fragment)
	if fragment == "" {
		return ""
	}
	if strings.HasPrefix(fragment, "^") {
		return "block"
	}
	return "heading"
}

func joinResolvedNoteTarget(target obsidian.ResolvedNoteTarget) string {
	path := strings.TrimSpace(target.Path)
	if path == "" {
		return ""
	}
	return joinLinkFragment(path, target.Fragment, fragmentTypeFromResolvedTarget(target))
}

func mapValuesToSlices(in map[string][]NoteWorkspaceLink) [][]NoteWorkspaceLink {
	if len(in) == 0 {
		return nil
	}
	out := make([][]NoteWorkspaceLink, 0, len(in))
	for _, items := range in {
		if len(items) == 0 {
			continue
		}
		out = append(out, items)
	}
	return out
}

func slugifyHeading(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return "section"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "section"
	}
	return out
}

// lookupNoteTitle returns the indexed title for a single note path, or empty
// when the store is unavailable or the note has not been indexed yet.
func (s *Server) lookupNoteTitle(ctx context.Context, notePath string) string {
	store := s.runtime.Intel()
	if store == nil || notePath == "" {
		return ""
	}
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{notePath})
	if err != nil {
		return ""
	}
	if row, ok := rows[notePath]; ok {
		return strings.TrimSpace(row.Title)
	}
	return ""
}

// resolveNoteDisplay returns the indexed note titles and summary frontmatter
// values for a batch of note paths. Paths that refer to code files (or that
// are missing from the index) are silently skipped; callers should fall back
// to their existing title extraction for those entries.
func (s *Server) resolveNoteDisplay(ctx context.Context, notePaths []string) (map[string]string, map[string]string) {
	titles := map[string]string{}
	summaries := map[string]string{}
	store := s.runtime.Intel()
	if store == nil || len(notePaths) == 0 {
		return titles, summaries
	}
	unique := make([]string, 0, len(notePaths))
	seen := make(map[string]struct{}, len(notePaths))
	for _, p := range notePaths {
		if p == "" || s.pathKind(p) != "note" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		unique = append(unique, p)
	}
	if len(unique) == 0 {
		return titles, summaries
	}
	if rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, unique); err == nil {
		for path, row := range rows {
			if title := strings.TrimSpace(row.Title); title != "" {
				titles[path] = title
			}
		}
	}
	if values, err := store.CurrentNotePropertyValues(ctx, unique, []string{"summary"}, 0); err == nil {
		for _, row := range values {
			if row.PropertyName != "summary" || row.IsList {
				continue
			}
			if _, ok := summaries[row.NotePath]; ok {
				continue
			}
			text := strings.TrimSpace(row.ValueText)
			if text == "" {
				continue
			}
			summaries[row.NotePath] = compactSnippet(text, 240)
		}
	}
	return titles, summaries
}

// applyNoteDisplay fills in Title and Summary for the supplied links using the
// lookup tables from resolveNoteDisplay. Missing titles fall back to the
// caller-provided value (typically filename stem), preserving old behavior.
func applyNoteDisplay(items []NoteWorkspaceLink, titles, summaries map[string]string) {
	for i := range items {
		if items[i].Kind != "note" {
			continue
		}
		notePath := noteDisplayPath(items[i].Path)
		if title, ok := titles[notePath]; ok && title != "" {
			items[i].Title = title
		}
		if items[i].Summary == "" {
			if summary, ok := summaries[notePath]; ok {
				items[i].Summary = summary
			}
		}
	}
}

// collectNotePathsFromLinks gathers every note path referenced by the provided
// workspace links so we can batch title+summary lookups in one shot.
func collectNotePathsFromLinks(groups ...[]NoteWorkspaceLink) []string {
	out := make([]string, 0)
	for _, group := range groups {
		for _, item := range group {
			if item.Kind == "note" && item.Path != "" {
				out = append(out, noteDisplayPath(item.Path))
			}
		}
	}
	return out
}

func noteDisplayPath(path string) string {
	if idx := strings.Index(path, "#"); idx >= 0 {
		path = path[:idx]
	}
	return strings.TrimSpace(path)
}

func backlinkTypeForGraphKind(kind string) obsidian.BacklinkType {
	switch kind {
	case "wikilink", "mdlink":
		return obsidian.BacklinkTypeBasic
	}
	_, subtype, ok := semdb.ParseNoteLinkKind(kind)
	if !ok {
		return obsidian.BacklinkTypeBasic
	}
	switch subtype {
	case "alias":
		return obsidian.BacklinkTypeAlias
	case "heading":
		return obsidian.BacklinkTypeHeading
	case "block":
		return obsidian.BacklinkTypeBlock
	case "embed":
		return obsidian.BacklinkTypeEmbed
	default:
		return obsidian.BacklinkTypeBasic
	}
}

func backlinkTypeForSubtype(subtype string) obsidian.BacklinkType {
	switch subtype {
	case "alias":
		return obsidian.BacklinkTypeAlias
	case "heading":
		return obsidian.BacklinkTypeHeading
	case "block":
		return obsidian.BacklinkTypeBlock
	case "embed":
		return obsidian.BacklinkTypeEmbed
	default:
		return obsidian.BacklinkTypeBasic
	}
}

func backlinkTypeForResolvedLink(link ResolvedLink) obsidian.BacklinkType {
	anchor := strings.TrimSpace(link.Anchor)
	if strings.HasPrefix(anchor, "block:") {
		return obsidian.BacklinkTypeBlock
	}
	if strings.HasPrefix(anchor, "heading:") {
		return obsidian.BacklinkTypeHeading
	}
	return backlinkTypeForSubtype(link.Kind)
}

func backlinkTypeRank(kind obsidian.BacklinkType) int {
	switch kind {
	case obsidian.BacklinkTypeAlias:
		return 1
	case obsidian.BacklinkTypeHeading:
		return 2
	case obsidian.BacklinkTypeBlock:
		return 3
	case obsidian.BacklinkTypeEmbed:
		return 4
	default:
		return 0
	}
}
