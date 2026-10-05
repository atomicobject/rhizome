package noderead

import (
	"context"
	"sort"
	"strings"
	"unicode"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func (s *Scope) subtreeInboundRelations(ctx context.Context, selection RelationSelection, committed NeighborhoodResult) (NeighborhoodResult, error) {
	if s == nil || s.service == nil || s.service.NoteReader == nil {
		return committed, nil
	}
	cache := s.notePathCache(ctx)
	if cache == nil {
		return committed, nil
	}
	sourcesByTarget := map[string][]ontology.NodeRef{}
	for _, source := range selection.Sources {
		for _, key := range sectionTargetKeys(source) {
			sourcesByTarget[source.NotePath+"\x00"+key] = append(sourcesByTarget[source.NotePath+"\x00"+key], source)
		}
	}
	if len(sourcesByTarget) == 0 {
		return committed, nil
	}
	entries, err := s.relationNoteEntries(ctx)
	if err != nil {
		return NeighborhoodResult{}, err
	}
	targetsBySource := map[string]map[string]struct{}{}
	referrerPaths := make([]string, 0)
	for _, entry := range entries {
		matched := false
		for _, scanned := range ontology.ScanMarkdownBodyLinks(entry.Content, obsidian.DefaultWikilinkOptions, obsidian.DefaultMdLinkOptions) {
			targetPath, targetKey, ok := relationSectionTarget(cache, entry.Path, scanned.Detail)
			if !ok {
				continue
			}
			for _, source := range sourcesByTarget[targetPath+"\x00"+targetKey] {
				key := nodeRefResultKey(source)
				if targetsBySource[key] == nil {
					targetsBySource[key] = map[string]struct{}{}
				}
				targetsBySource[key][entry.Path] = struct{}{}
				matched = true
			}
		}
		if matched {
			referrerPaths = append(referrerPaths, entry.Path)
		}
	}
	types, err := s.TypesByPaths(ctx, referrerPaths)
	if err != nil {
		return NeighborhoodResult{}, err
	}
	edges := append([]NeighborhoodEdge(nil), committed.Edges...)
	for _, source := range selection.Sources {
		paths := make([]string, 0, len(targetsBySource[nodeRefResultKey(source)]))
		for path := range targetsBySource[nodeRefResultKey(source)] {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			typeName := types[path].TypeName
			if !relationTargetTypeAllowed(s.service.Schema, typeName, selection.TargetTypes, selection.TargetInterfaces) {
				continue
			}
			target := ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote, TypeName: typeName}
			edges = append(edges, NeighborhoodEdge{
				Source: source,
				Target: target,
				Edge: semdb.OntologyEdgeRow{
					SrcPath: path, RelationName: selection.Name, DstPath: source.NotePath, DstNodeID: source.NodeID,
					DstType: source.TypeName, Provenance: "section_neighbor", Structural: true,
				},
				Direction: TraversalDirectionInbound, RelationName: selection.Name, Provenance: "section_neighbor",
				Structural: true, TargetType: typeName, Depth: 1,
			})
		}
	}
	sortNeighborhoodEdges(edges)
	return groupNeighborhoodEdges(selection.Sources, edges, 0, 0), nil
}

func (s *Scope) relationNoteEntries(ctx context.Context) ([]obsidian.NoteEntry, error) {
	s.mu.Lock()
	if s.relationEntriesRead {
		entries, err := s.relationEntries, s.relationEntriesErr
		s.mu.Unlock()
		return entries, err
	}
	generation := s.cacheGeneration
	s.mu.Unlock()

	var entries []obsidian.NoteEntry
	var err error
	if provider, ok := s.service.NoteReader.(obsidian.NoteEntriesProvider); ok {
		entries, err = provider.NoteEntriesSnapshot(ctx)
	} else {
		var paths []string
		paths, err = s.service.NoteReader.GetNotesList(s.service.VaultDef)
		if err == nil {
			entries = make([]obsidian.NoteEntry, 0, len(paths))
			for _, path := range paths {
				var content string
				content, err = s.service.NoteReader.GetContents(s.service.VaultDef, path)
				if err != nil {
					break
				}
				entries = append(entries, obsidian.NoteEntry{Path: path, Content: content})
			}
		}
	}
	s.mu.Lock()
	if s.cacheGeneration == generation {
		s.relationEntries = entries
		s.relationEntriesErr = err
		s.relationEntriesRead = true
	}
	s.mu.Unlock()
	return entries, err
}

func sectionTargetKeys(ref ontology.NodeRef) []string {
	seen := map[string]struct{}{}
	add := func(fragment string) {
		fragment = strings.TrimSpace(strings.TrimPrefix(fragment, "#"))
		if fragment == "" {
			return
		}
		if strings.HasPrefix(fragment, "^") {
			seen["block:"+strings.TrimPrefix(fragment, "^")] = struct{}{}
			return
		}
		seen["heading:"+relationSlug(fragment)] = struct{}{}
	}
	add(ref.Fragment)
	if idx := strings.Index(ref.NodeID, "#"); idx >= 0 && idx < len(ref.NodeID)-1 {
		add(ref.NodeID[idx+1:])
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	return out
}

func relationSectionTarget(cache *obsidian.NotePathCache, fromPath string, target obsidian.LinkDetail) (string, string, bool) {
	var resolved obsidian.ResolvedNoteTarget
	var ok bool
	if target.LinkType == "mdlink" {
		resolved.Path, ok = cache.ResolveMdLink(target.Target, fromPath)
		if idx := strings.Index(target.Target, "#"); idx >= 0 && idx < len(target.Target)-1 {
			resolved.Fragment = target.Target[idx+1:]
		}
	} else {
		resolved, ok = cache.ResolveNoteTarget(target.Target)
	}
	fragment := strings.TrimSpace(resolved.Fragment)
	if !ok || resolved.Path == "" || fragment == "" {
		return "", "", false
	}
	if strings.HasPrefix(fragment, "^") {
		return resolved.Path, "block:" + strings.TrimPrefix(fragment, "^"), true
	}
	return resolved.Path, "heading:" + relationSlug(fragment), true
}

func relationSlug(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Scope) overlaySubtreeOutboundRelations(ctx context.Context, selection RelationSelection, committed NeighborhoodResult) (NeighborhoodResult, error) {
	if s == nil || s.opts.ReadOverlay == nil || s.opts.ReadOverlay.Empty() {
		return committed, nil
	}
	s.mu.Lock()
	index, err := s.overlayIndexLocked(ctx)
	s.mu.Unlock()
	if err != nil || index == nil {
		return committed, err
	}
	touchedSources := map[string]ontology.NodeRef{}
	for _, source := range selection.Sources {
		if _, touched := index.TouchedPaths[source.NotePath]; touched {
			touchedSources[nodeRefResultKey(source)] = source
		}
	}
	if len(touchedSources) == 0 {
		return committed, nil
	}
	edges := make([]NeighborhoodEdge, 0, len(committed.Edges))
	for _, edge := range committed.Edges {
		if _, replace := touchedSources[nodeRefResultKey(edge.Source)]; !replace {
			edges = append(edges, edge)
		}
	}
	cache := s.notePathCache(ctx)
	if cache == nil {
		return groupNeighborhoodEdges(selection.Sources, edges, 0, 0), nil
	}
	for _, source := range touchedSources {
		snapshot := index.SnapshotsByPath[source.NotePath]
		if snapshot == nil {
			continue
		}
		section := snapshot.SectionsByID[source.NodeID]
		if section == nil {
			continue
		}
		paths := make([]string, 0)
		seenPaths := map[string]struct{}{}
		for _, scanned := range ontology.ScanMarkdownBodyLinks(ontology.SectionBody(section), obsidian.DefaultWikilinkOptions, obsidian.DefaultMdLinkOptions) {
			var targetPath string
			var ok bool
			if scanned.Detail.LinkType == "mdlink" {
				targetPath, ok = cache.ResolveMdLink(scanned.Detail.Target, source.NotePath)
			} else {
				targetPath, ok = cache.ResolveNote(scanned.Detail.Target)
			}
			if !ok || strings.TrimSpace(targetPath) == "" {
				continue
			}
			if _, seen := seenPaths[targetPath]; seen {
				continue
			}
			seenPaths[targetPath] = struct{}{}
			paths = append(paths, targetPath)
		}
		types, err := s.TypesByPaths(ctx, paths)
		if err != nil {
			return NeighborhoodResult{}, err
		}
		for _, targetPath := range paths {
			typeName := types[targetPath].TypeName
			if !relationTargetTypeAllowed(s.service.Schema, typeName, selection.TargetTypes, selection.TargetInterfaces) {
				continue
			}
			target := ontology.NodeRef{NotePath: targetPath, Kind: ontology.NodeKindNote, TypeName: typeName}
			edges = append(edges, NeighborhoodEdge{
				Source: source,
				Target: target,
				Edge: semdb.OntologyEdgeRow{
					SrcPath: source.NotePath, SrcNodeID: source.NodeID, RelationName: selection.Name,
					DstPath: targetPath, DstType: typeName, Provenance: "section_neighbor", Structural: true,
				},
				Direction: TraversalDirectionOutbound, RelationName: selection.Name, Provenance: "section_neighbor",
				Structural: true, TargetType: typeName, Depth: 1,
			})
		}
	}
	sortNeighborhoodEdges(edges)
	return groupNeighborhoodEdges(selection.Sources, edges, 0, 0), nil
}

func relationTargetTypeAllowed(schema *ontology.Schema, actual string, concrete, interfaces []string) bool {
	if len(concrete) > 0 {
		allowed := false
		for _, candidate := range concrete {
			if actual == candidate {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	for _, iface := range interfaces {
		if ontology.TypeMatchesOrImplements(schema, actual, iface) {
			return true
		}
	}
	return len(interfaces) == 0
}
