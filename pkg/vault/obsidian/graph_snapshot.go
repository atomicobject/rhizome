package obsidian

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// GraphSnapshot is the source-neutral input consumed by graph analysis and
// backlink derivation. Producers may build it from live note content or from
// persisted metadata rows.
type GraphSnapshot struct {
	Nodes map[string]GraphSnapshotNode
	Edges []GraphSnapshotEdge
}

// GraphSnapshotNode contains the raw note facts graph consumers need.
type GraphSnapshotNode struct {
	Path        string
	Tags        []string
	ContentTime time.Time
	Size        int64
}

// GraphSnapshotEdge records one resolved note link. LinkType identifies the
// rendered link family (wikilink or mdlink); Subtype preserves backlink shape.
type GraphSnapshotEdge struct {
	Source   string
	Target   string
	LinkType string
	Subtype  BacklinkType
	Fragment string
}

// BuildGraphSnapshot builds a source-neutral graph input from a NoteReader.
// It is the filesystem/snapshot-provider adapter; algorithms consume only the
// returned DTO.
func BuildGraphSnapshot(vaultDef VaultDefinition, note NoteReader, options GraphAnalysisOptions) (*GraphSnapshot, error) {
	entries, err := graphNoteEntries(vaultDef, note)
	if err != nil {
		return nil, err
	}
	entries = mergeGraphDocEntries(entries, discoverGraphDocEntries(vaultDef, options))
	allNotes := make([]string, 0, len(entries))
	for _, entry := range entries {
		allNotes = append(allNotes, entry.Path)
	}
	cache := BuildNotePathCacheWithAliases(allNotes, AliasesFromNoteEntries(entries))

	snapshot := &GraphSnapshot{
		Nodes: make(map[string]GraphSnapshotNode, len(entries)),
		Edges: make([]GraphSnapshotEdge, 0),
	}
	for _, entry := range entries {
		path := normalizeGraphSnapshotPath(entry.Path)
		if path == "" {
			continue
		}
		tags := append([]string(nil), entry.Tags...)
		if len(tags) == 0 {
			tags = extractTags(entry.Content)
		}
		contentTime := entry.ContentTime
		if contentTime.IsZero() {
			contentTime, _ = ResolveContentTime(path, entry.Content)
		}
		snapshot.Nodes[path] = GraphSnapshotNode{
			Path:        path,
			Tags:        tags,
			ContentTime: contentTime,
			Size:        int64(len([]byte(entry.Content))),
		}

		for _, detail := range ScanAllLinks(entry.Content, options.WikilinkOptions, MdLinkOptions(options.WikilinkOptions)) {
			if (detail.LinkType == "wikilink" && !vaultDef.SupportsWikilinks()) ||
				(detail.LinkType == "mdlink" && !vaultDef.SupportsMarkdownLinks()) {
				continue
			}
			resolved, ok := resolveGraphSnapshotLink(cache, path, detail)
			if !ok {
				continue
			}
			target := normalizeGraphSnapshotPath(resolved.Path)
			if target == "" || target == path {
				continue
			}
			snapshot.Edges = append(snapshot.Edges, GraphSnapshotEdge{
				Source:   path,
				Target:   target,
				LinkType: detail.LinkType,
				Subtype:  BacklinkType(detail.Subtype),
				Fragment: resolved.Fragment,
			})
		}
	}
	snapshot.Normalize()
	return snapshot, nil
}

func discoverGraphDocEntries(vaultDef VaultDefinition, options GraphAnalysisOptions) []NoteEntry {
	if !options.IncludeDocsInGraph || vaultDef.BasePath() == "" {
		return nil
	}
	discoveryDef := vaultDef
	if discoveryDef.IsCollection() {
		discoveryDef.Includes = []string{"**/*.md"}
	}
	candidates, err := DiscoverFiles(discoveryDef)
	if err != nil {
		return nil
	}
	vaultPaths, err := paths.NewVaultPaths(vaultDef.BasePath())
	if err != nil {
		return nil
	}
	docPatterns := NormalizeDocPatterns(options.DocPatterns)
	docMinBytes := options.DocMinBytes
	if docMinBytes <= 0 {
		docMinBytes = 200
	}

	entries := make([]NoteEntry, 0)
	for _, candidate := range candidates {
		path := paths.NormalizeNotePath(candidate)
		if path == "" || !MatchDocPatternNormalized(path.String(), docPatterns) {
			continue
		}
		abs, err := vaultPaths.AbsNotePath(path)
		if err != nil {
			continue
		}
		content, err := os.ReadFile(abs.String())
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(content))
		if len(trimmed) < docMinBytes && !strings.Contains(trimmed, "[[") {
			continue
		}
		var contentTime time.Time
		if info, statErr := os.Stat(abs.String()); statErr == nil {
			contentTime = info.ModTime()
		}
		entries = append(entries, NoteEntry{
			Path:        path.String(),
			Content:     trimmed,
			ContentTime: contentTime,
		})
	}
	return entries
}

func mergeGraphDocEntries(entries, docEntries []NoteEntry) []NoteEntry {
	existing := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		existing[normalizeGraphSnapshotPath(entry.Path)] = struct{}{}
	}
	for _, entry := range docEntries {
		path := normalizeGraphSnapshotPath(entry.Path)
		if _, ok := existing[path]; ok {
			continue
		}
		entries = append(entries, entry)
		existing[path] = struct{}{}
	}
	return entries
}

func graphNoteEntries(vaultDef VaultDefinition, note NoteReader) ([]NoteEntry, error) {
	if provider, ok := note.(NoteEntriesProvider); ok {
		if entries, err := provider.NoteEntriesSnapshot(context.Background()); err == nil && len(entries) > 0 {
			return entries, nil
		}
	}
	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	entries := make([]NoteEntry, 0, len(allNotes))
	for _, path := range allNotes {
		content, err := note.GetContents(vaultDef, path)
		if err != nil {
			return nil, err
		}
		var contentTime time.Time
		// The filesystem reader can provide mtime without changing the mock and
		// remote-reader contract. Snapshot providers carry their own timestamp.
		if _, filesystemReader := note.(*Note); filesystemReader {
			contentTime, _ = note.GetModTime(vaultDef, path)
		}
		entries = append(entries, NoteEntry{Path: path, Content: content, ContentTime: contentTime})
	}
	return entries, nil
}

func resolveGraphSnapshotLink(cache *NotePathCache, source string, detail LinkDetail) (ResolvedNoteTarget, bool) {
	switch detail.LinkType {
	case "wikilink":
		return cache.ResolveNoteTarget(detail.Target)
	case "mdlink":
		return cache.ResolveMdLinkTarget(detail.Target, source)
	default:
		return ResolvedNoteTarget{}, false
	}
}

func normalizeGraphSnapshotPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return paths.NormalizeNotePath(path).String()
}

// Normalize canonicalizes and deterministically orders a graph snapshot.
func (s *GraphSnapshot) Normalize() {
	if s == nil {
		return
	}
	nodes := make(map[string]GraphSnapshotNode, len(s.Nodes))
	for key, node := range s.Nodes {
		path := normalizeGraphSnapshotPath(node.Path)
		if path == "" {
			path = normalizeGraphSnapshotPath(key)
		}
		if path == "" {
			continue
		}
		node.Path = path
		node.Tags = append([]string(nil), node.Tags...)
		sort.Strings(node.Tags)
		nodes[path] = node
	}
	s.Nodes = nodes

	edges := make([]GraphSnapshotEdge, 0, len(s.Edges))
	seen := make(map[string]struct{}, len(s.Edges))
	for _, edge := range s.Edges {
		edge.Source = normalizeGraphSnapshotPath(edge.Source)
		edge.Target = normalizeGraphSnapshotPath(edge.Target)
		if edge.Source == "" || edge.Target == "" || edge.Source == edge.Target {
			continue
		}
		if _, ok := nodes[edge.Source]; !ok {
			continue
		}
		if _, ok := nodes[edge.Target]; !ok {
			continue
		}
		key := edge.Source + "\x00" + edge.Target + "\x00" + edge.LinkType + "\x00" + string(edge.Subtype) + "\x00" + edge.Fragment
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		if edges[i].LinkType != edges[j].LinkType {
			return graphLinkTypeRank(edges[i].LinkType) < graphLinkTypeRank(edges[j].LinkType)
		}
		if edges[i].Subtype != edges[j].Subtype {
			return edges[i].Subtype < edges[j].Subtype
		}
		return edges[i].Fragment < edges[j].Fragment
	})
	s.Edges = edges
}

func graphLinkTypeRank(linkType string) int {
	switch linkType {
	case "wikilink":
		return 0
	case "mdlink":
		return 1
	default:
		return 2
	}
}

func graphSnapshotAdjacency(snapshot *GraphSnapshot, options GraphAnalysisOptions, applyDocPolicy bool) (map[string]map[string]struct{}, map[string][]string, map[string]time.Time) {
	adjacency := make(map[string]map[string]struct{})
	tags := make(map[string][]string)
	times := make(map[string]time.Time)
	if snapshot == nil {
		return adjacency, tags, times
	}
	linkedDocs := make(map[string]struct{})
	if applyDocPolicy {
		for _, edge := range snapshot.Edges {
			if edge.LinkType == "wikilink" {
				linkedDocs[edge.Source] = struct{}{}
			}
		}
	}
	docPatterns := NormalizeDocPatterns(options.DocPatterns)
	docMinBytes := options.DocMinBytes
	if docMinBytes <= 0 {
		docMinBytes = 200
	}
	for path, node := range snapshot.Nodes {
		if applyDocPolicy && MatchDocPatternNormalized(path, docPatterns) {
			if !options.IncludeDocsInGraph {
				continue
			}
			if node.Size < int64(docMinBytes) {
				if _, linked := linkedDocs[path]; !linked {
					continue
				}
			}
		}
		if _, skip := options.ExcludedPaths[path]; skip {
			continue
		}
		if len(options.IncludedPaths) > 0 {
			if _, ok := options.IncludedPaths[path]; !ok {
				continue
			}
		}
		adjacency[path] = make(map[string]struct{})
		if options.IncludeTags && len(node.Tags) > 0 {
			tags[path] = append([]string(nil), node.Tags...)
		}
		if !node.ContentTime.IsZero() {
			times[path] = node.ContentTime
		}
	}
	for _, edge := range snapshot.Edges {
		// Vault graph algorithms historically operate on wikilinks. Markdown
		// edges remain available to backlink consumers on the same DTO.
		if edge.LinkType != "wikilink" {
			continue
		}
		if options.SkipEmbeds && edge.Subtype == BacklinkTypeEmbed {
			continue
		}
		if options.SkipAnchors && (edge.Subtype == BacklinkTypeHeading || edge.Subtype == BacklinkTypeBlock) {
			continue
		}
		if _, ok := adjacency[edge.Source]; !ok {
			continue
		}
		if _, ok := adjacency[edge.Target]; !ok {
			continue
		}
		adjacency[edge.Source][edge.Target] = struct{}{}
	}
	return adjacency, tags, times
}

// CollectBacklinksFromGraph derives first-degree backlinks without reading
// note content. Every requested target is represented in the result.
func CollectBacklinksFromGraph(snapshot *GraphSnapshot, targets []string, suppressedTags []string) map[string][]Backlink {
	result := make(map[string][]Backlink, len(targets))
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		path := normalizeGraphSnapshotPath(target)
		targetSet[path] = struct{}{}
		result[path] = []Backlink{}
	}
	if snapshot == nil {
		return result
	}
	seen := make(map[string]map[string]struct{}, len(targets))
	for _, edge := range snapshot.Edges {
		if _, ok := targetSet[edge.Target]; !ok {
			continue
		}
		if node, ok := snapshot.Nodes[edge.Source]; ok && containsSuppressedTags(node.Tags, suppressedTags) {
			continue
		}
		if seen[edge.Target] == nil {
			seen[edge.Target] = make(map[string]struct{})
		}
		key := edge.Source + "\x00" + edge.Fragment
		if _, ok := seen[edge.Target][key]; ok {
			continue
		}
		seen[edge.Target][key] = struct{}{}
		result[edge.Target] = append(result[edge.Target], Backlink{
			Referrer: edge.Source,
			LinkType: edge.Subtype,
			Fragment: edge.Fragment,
		})
	}
	for target := range result {
		sort.Slice(result[target], func(i, j int) bool {
			if result[target][i].Referrer != result[target][j].Referrer {
				return result[target][i].Referrer < result[target][j].Referrer
			}
			if result[target][i].LinkType != result[target][j].LinkType {
				return result[target][i].LinkType < result[target][j].LinkType
			}
			return result[target][i].Fragment < result[target][j].Fragment
		})
	}
	return result
}
