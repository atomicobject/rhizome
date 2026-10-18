package operations

// Docs: [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md), [Graph (Hub)](docs/hubs/Graph (Hub).md)

import (
	"context"
	"fmt"
	"sort"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	graphindex "github.com/atomicobject/rhizome/pkg/graph/index"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	// DocGraphAnalysisParams controls unified doc-graph community analysis.
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type DocGraphAnalysisParams struct {
	NoteMetadata          notemeta.Indexer
	ExcludePatterns       []string
	IncludePatterns       []string
	UseConfig             bool
	IncludeTags           bool
	IntelStore            *semdb.Store
	MetadataStoreFallback MetadataStoreFallbackPolicy
	WikilinkOptions       markdownsyntax.WikilinkOptions
}

// DocGraphAnalysis returns a community analysis backed by persisted doc graph scores.
func DocGraphAnalysis(ctx context.Context, vault repoconfig.VaultManager, note obsidian.NoteReader, params DocGraphAnalysisParams) (*vaultgraph.GraphAnalysis, error) {
	if err := params.NoteMetadata.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}
	vaultPath := vaultDef.BasePath()
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)

	store := params.IntelStore
	closeStore := func() {}
	if store == nil && params.MetadataStoreFallback.allowsStoreOpen() {
		opened, closeFn, err := openDocGraphStore(vaultPath)
		if err != nil {
			return nil, err
		}
		if opened == nil {
			return nil, fmt.Errorf("doc graph store unavailable for vault %q", vaultPath)
		}
		store = opened
		closeStore = closeFn
	}
	if store == nil {
		// A planned one-shot caller owns no writable index. Preserve the live
		// community result instead of opening a second metadata store.
		return liveDocGraphAnalysis(vault, note, params)
	}
	defer closeStore()

	excludePatterns := expandPatterns(params.ExcludePatterns)
	includePatterns := expandPatterns(params.IncludePatterns)
	var authorityFactors []repoconfig.AuthorityFactorRule
	if params.UseConfig {
		{
			resolvedConfig1, configErr1 := repoconfig.Load(vaultPath)
			cfg, err := resolvedConfig1.Graph, configErr1
			if err == nil {
				excludePatterns = append(cfg.Ignore, excludePatterns...)
				authorityFactors = cfg.AuthorityFactors
			}
		}
	}

	excludedSet, err := buildNoteFilterSet(vault, note, excludePatterns, params.MetadataStoreFallback)
	if err != nil {
		return nil, err
	}
	includedSet, err := buildNoteFilterSet(vault, note, includePatterns, params.MetadataStoreFallback)
	if err != nil {
		return nil, err
	}

	entries, err := loadNoteEntries(ctx, vaultDef, note)
	if err != nil {
		return nil, err
	}
	meta, notePaths := buildNoteMeta(entries, vaultDef, note, vaultPaths, params.IncludeTags, excludedSet, includedSet)

	scores, err := store.GraphDocScores(ctx)
	if err != nil {
		return nil, err
	}
	if len(scores) == 0 && !params.MetadataStoreFallback.allowsStoreOpen() {
		// The managed reader may be current but intentionally lacks this
		// optional projection. Do not materialize it through a query-only open.
		return liveDocGraphAnalysis(vault, note, params)
	}
	if params.MetadataStoreFallback.allowsStoreOpen() {
		if err := ensureDocScoresWithEntries(ctx, params.NoteMetadata, vaultDef, store, params.WikilinkOptions, entries); err != nil {
			return nil, err
		}

		scores, err = store.GraphDocScores(ctx)
		if err != nil {
			return nil, err
		}
	}

	nodes := make(map[string]vaultgraph.GraphNode, len(scores))
	for _, score := range scores {
		kind := score.DocType
		if kind != "note" && kind != "code" {
			continue
		}
		isNote := kind == "note"
		path := vaultPaths.NormalizeDocPath(score.DocPath)
		if path == "" {
			continue
		}
		if isNote {
			if _, ok := notePaths[path]; !ok {
				continue
			}
		}
		community := score.Community
		if community == "" {
			community = path
		}
		nodes[path] = vaultgraph.GraphNode{
			Path:      path,
			Title:     paths.NoteTitle(path),
			Kind:      kind,
			Hub:       score.Hub,
			Authority: score.Authority,
			Community: community,
		}
	}

	adjacency := make(map[string]map[string]struct{}, len(nodes))
	for path := range nodes {
		adjacency[path] = make(map[string]struct{})
	}
	addEdge := func(src, dst string) {
		if src == "" || dst == "" || src == dst {
			return
		}
		if _, ok := nodes[src]; !ok {
			return
		}
		if _, ok := nodes[dst]; !ok {
			return
		}
		adjacency[src][dst] = struct{}{}
	}

	if noteEdges, err := store.GraphDocNoteEdges(ctx); err == nil {
		for _, e := range noteEdges {
			src := vaultPaths.NormalizeDocPath(e.SrcPath)
			dst := vaultPaths.NormalizeDocPath(e.DstPath)
			addEdge(src, dst)
		}
	}
	if mentionEdges, err := store.GraphDocMentionEdges(ctx); err == nil {
		for _, e := range mentionEdges {
			src := vaultPaths.NormalizeDocPath(e.SrcPath)
			dst := vaultPaths.NormalizeDocPath(e.DstPath)
			addEdge(src, dst)
		}
	}
	if codeRefEdges, err := store.GraphDocCodeRefEdges(ctx); err == nil {
		for _, e := range codeRefEdges {
			src := vaultPaths.NormalizeDocPath(e.SrcPath)
			dst := vaultPaths.NormalizeDocPath(e.DstPath)
			addEdge(src, dst)
		}
	}

	inbound := make(map[string]int, len(nodes))
	edgeCount := 0
	for src, dsts := range adjacency {
		neighbors := make([]string, 0, len(dsts))
		for dst := range dsts {
			neighbors = append(neighbors, dst)
			inbound[dst]++
		}
		sort.Strings(neighbors)
		node := nodes[src]
		node.Neighbors = neighbors
		node.Outbound = len(neighbors)
		nodes[src] = node
		edgeCount += len(neighbors)
	}
	for path, node := range nodes {
		node.Inbound = inbound[path]
		if metaEntry, ok := meta[path]; ok {
			node.Tags = metaEntry.tags
		}
		nodes[path] = node
	}

	orphans := make([]string, 0)
	for path, node := range nodes {
		if node.Inbound == 0 && node.Outbound == 0 {
			orphans = append(orphans, path)
		}
	}
	sort.Strings(orphans)

	analysis := &vaultgraph.GraphAnalysis{
		Nodes:          nodes,
		WeakComponents: weakComponents(adjacency),
		Orphans:        orphans,
		Stats: vaultgraph.GraphStatsSummary{
			NodeCount: len(nodes),
			EdgeCount: edgeCount,
		},
		EffectiveTimes: noteTimes(meta),
	}
	if params.UseConfig && len(authorityFactors) > 0 {
		_, err := ApplyAuthorityFactorRules(analysis, vaultDef, note, authorityFactors)
		if err != nil {
			return nil, err
		}
	}
	vaultgraph.RefreshGraphAnalysisDerived(analysis)

	return analysis, nil
}

func liveDocGraphAnalysis(vault repoconfig.VaultManager, note obsidian.NoteReader, params DocGraphAnalysisParams) (*vaultgraph.GraphAnalysis, error) {
	return GraphAnalysis(vault, note, GraphAnalysisParams{
		NoteMetadata: params.NoteMetadata,
		Options: vaultgraph.GraphAnalysisOptions{
			WikilinkOptions:   params.WikilinkOptions,
			IncludeTags:       params.IncludeTags,
			RecencyCascade:    true,
			RecencyCascadeSet: true,
		},
		ExcludePatterns:       params.ExcludePatterns,
		IncludePatterns:       params.IncludePatterns,
		UseConfig:             params.UseConfig,
		MetadataStoreFallback: params.MetadataStoreFallback,
	})
}

// EnsureDocScores ensures persisted doc scores exist for community analysis.
func EnsureDocScores(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, store *semdb.Store, opts graphindex.DocScoresOptions) error {
	if err := noteMetadata.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if store == nil {
		return nil
	}
	existing, err := store.GraphDocScores(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	entries, err := loadNoteEntries(ctx, vaultDef, note)
	if err != nil {
		return err
	}
	return ensureDocScoresWithEntries(ctx, noteMetadata, vaultDef, store, opts.WikilinkOptions, entries)
}

type noteMeta struct {
	tags        []string
	contentTime time.Time
}

func ensureDocScoresWithEntries(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, store *semdb.Store, opts markdownsyntax.WikilinkOptions, entries []obsidian.NoteEntry) error {
	if store == nil {
		return nil
	}
	scores, err := store.GraphDocScores(ctx)
	if err != nil {
		return err
	}
	if len(scores) > 0 {
		return nil
	}

	sections, err := store.IntelDocSections(ctx)
	if err != nil {
		return err
	}
	if len(sections) == 0 {
		noteSvc := codeanchor.NewServiceWithOptions(
			store,
			nil,
			codeanchor.WithBasePath(vaultDef.BasePath()),
			codeanchor.WithoutWarmCache(),
		)
		for _, entry := range entries {
			source, err := noteMetadata.ProjectNoteSource(entry.Path, entry.Content, 0)
			if err != nil {
				return fmt.Errorf("project graph source %s: %w", entry.Path, err)
			}
			if _, err := noteSvc.IngestNoteSource(ctx, source); err != nil {
				return err
			}
		}

	}

	newScores, err := graphindex.ComputeDocScores(ctx, store, graphindex.DocScoresOptions{
		WikilinkOptions: opts,
		VaultRoot:       vaultDef.BasePath(),
	})
	if err != nil {
		return err
	}
	return store.ReplaceGraphDocScores(ctx, newScores)
}

func openDocGraphStore(vaultPath string) (*semdb.Store, func(), error) {
	basePath := openableVaultBasePath(vaultPath)
	if basePath == "" {
		return nil, nil, nil
	}
	resolvedConfig2, configErr2 := repoconfig.Load(basePath)
	cfg, err := resolvedConfig2.Code, configErr2
	if err != nil {
		cfg = codeanchor.DefaultConfig(basePath)
	}
	return indexstore.Open(basePath, indexstore.Options{Access: indexstore.ReadWrite, Config: &cfg, Create: true})
}

func loadNoteEntries(ctx context.Context, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader) ([]obsidian.NoteEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if provider, ok := note.(obsidian.NoteEntriesProvider); ok {
		if entries, err := provider.NoteEntriesSnapshot(ctx); err == nil {
			return entries, nil
		}
	}

	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	entries := make([]obsidian.NoteEntry, 0, len(allNotes))
	for _, path := range allNotes {
		content, err := note.GetContents(vaultDef, path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, obsidian.NoteEntry{
			Path:    path,
			Content: content,
		})
	}
	return entries, nil
}

func buildNoteFilterSet(vault repoconfig.VaultManager, note obsidian.NoteReader, patterns []string, fallback MetadataStoreFallbackPolicy) (map[string]struct{}, error) {
	set := make(map[string]struct{})
	if len(patterns) == 0 {
		return set, nil
	}
	parsed, expr, err := ParseInputsWithExpression(patterns)
	if err != nil {
		return nil, err
	}
	matches, err := ListFiles(vault, note, ListParams{
		Inputs:                parsed,
		Expression:            expr,
		MaxDepth:              0,
		SkipAnchors:           false,
		SkipEmbeds:            false,
		MetadataStoreFallback: fallback,
	})
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		norm := string(paths.NormalizeNotePath(m))
		set[norm] = struct{}{}
	}
	return set, nil
}

func buildNoteMeta(entries []obsidian.NoteEntry, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, vaultPaths paths.VaultPaths, includeTags bool, excluded, included map[string]struct{}) (map[string]noteMeta, map[string]struct{}) {
	meta := make(map[string]noteMeta, len(entries))
	pathsOut := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		path := vaultPaths.NormalizeDocPath(entry.Path)
		if path == "" {
			continue
		}
		if _, skip := excluded[path]; skip {
			continue
		}
		if len(included) > 0 {
			if _, ok := included[path]; !ok {
				continue
			}
		}
		tags := entry.Tags
		if includeTags && len(tags) == 0 {
			if fact, ok := NoteFactsFromReader(note).LookupFact(entry.Path); ok {
				tags = fact.Tags
			}
		}
		contentTime := entry.ContentTime
		if contentTime.IsZero() {
			if ct, ok := vaultgraph.ResolveContentTime(entry.Path, entry.Content); ok {
				contentTime = ct
			} else if mt, err := note.GetModTime(vaultDef, entry.Path); err == nil {
				contentTime = mt
			}
		}
		meta[path] = noteMeta{tags: tags, contentTime: contentTime}
		pathsOut[path] = struct{}{}
	}
	return meta, pathsOut
}

func noteTimes(meta map[string]noteMeta) map[string]time.Time {
	out := make(map[string]time.Time, len(meta))
	for path, entry := range meta {
		if !entry.contentTime.IsZero() {
			out[path] = entry.contentTime
		}
	}
	return out
}

func weakComponents(adjacency map[string]map[string]struct{}) [][]string {
	if len(adjacency) == 0 {
		return nil
	}
	undirected := make(map[string]map[string]struct{}, len(adjacency))
	for node := range adjacency {
		undirected[node] = make(map[string]struct{})
	}
	for src, dsts := range adjacency {
		for dst := range dsts {
			undirected[src][dst] = struct{}{}
			undirected[dst][src] = struct{}{}
		}
	}

	visited := make(map[string]bool, len(adjacency))
	components := make([][]string, 0, len(adjacency))
	for node := range undirected {
		if visited[node] {
			continue
		}
		queue := []string{node}
		visited[node] = true
		var comp []string
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)
			for neighbor := range undirected[cur] {
				if visited[neighbor] {
					continue
				}
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
		sort.Strings(comp)
		components = append(components, comp)
	}
	return components
}
