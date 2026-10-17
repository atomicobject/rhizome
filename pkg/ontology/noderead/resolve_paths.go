package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Scope) canonicalCandidateRefs(ctx context.Context, candidates []obsidian.ResolvedNoteTarget) ([]ontology.NodeRef, error) {
	paths := make([]string, 0, len(candidates))
	locators := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, candidate.Path)
		if candidate.Fragment != "" {
			locators = append(locators, candidate.Path+"#"+candidate.Fragment)
		}
	}

	typeRows := map[string]semdb.OntologyNoteTypeRow{}
	catalogRows := map[string]readmodel.NodeRow{}
	if s != nil && s.service != nil && s.service.Store != nil {
		var err error
		typeRows, err = s.service.Store.OntologyTypesByPaths(ctx, paths)
		if err != nil {
			return nil, err
		}
		if len(locators) > 0 {
			catalogRows, err = s.service.Store.OntologyNodesBySourceLocators(ctx, locators)
			if err != nil {
				return nil, err
			}
		}
	}

	refs := make([]ontology.NodeRef, 0, len(candidates))
	for _, candidate := range candidates {
		locator := candidate.Path
		if candidate.Fragment != "" {
			locator += "#" + candidate.Fragment
		}
		if row, ok := catalogRows[locator]; ok {
			refs = append(refs, nodeRefFromCatalogRow(row))
			continue
		}
		ref := ontology.NodeRef{
			NotePath: candidate.Path,
			Kind:     ontology.NodeKindNote,
			TypeName: strings.TrimSpace(typeRows[candidate.Path].TypeName),
		}
		if candidate.Fragment != "" {
			ref.Kind = ontology.NodeKindEmbedded
			ref.Fragment = candidate.Fragment
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func explicitResolveTarget(input string, target string) (parsedResolveTarget, bool) {
	isCode := strings.HasPrefix(strings.TrimSpace(input), "code:")
	if isCode {
		target = strings.TrimPrefix(strings.TrimSpace(target), "code:")
	}
	notePath, fragment := splitLocatorTarget(target)
	nodeID := ""
	if index := strings.Index(target, "#node:"); !isCode && index > 0 {
		notePath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(target[:index])))
		nodeID = strings.TrimSpace(target[index+len("#node:"):])
		fragment = "node:" + nodeID
	}
	if notePath == "" || strings.Contains(notePath, "://") {
		return parsedResolveTarget{}, false
	}
	if !(isCode || nodeID != "" || strings.Contains(notePath, "/") || filepath.IsAbs(notePath)) {
		return parsedResolveTarget{}, false
	}
	ref := ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindNote}
	if isCode {
		ref.Kind = "CODE_FILE"
	}
	sourceLocator := notePath
	if fragment != "" {
		sourceLocator = notePath + "#" + fragment
		ref = ontology.NodeRef{NotePath: notePath, Fragment: fragment, Kind: ontology.NodeKindEmbedded}
		if nodeID != "" {
			ref.Fragment = ""
			ref.NodeID = nodeID
			ref.Kind = ontology.NodeKindSection
			sourceLocator = ontology.NodeSourceLocator(ref)
		} else if structural := strings.TrimPrefix(fragment, "struct:"); structural != fragment {
			ref.Fragment = ""
			ref.Structural = strings.TrimSpace(structural)
			ref.Kind = ontology.NodeKindSection
		}
	}
	return parsedResolveTarget{
		Input:         input,
		SourceLocator: sourceLocator,
		NotePath:      notePath,
		Fragment:      fragment,
		Ref:           ref,
	}, true
}

func (s *Scope) notePathCache(ctx context.Context) (*obsidian.NotePathCache, error) {
	if s == nil || s.service == nil || s.service.NoteReader == nil {
		return nil, nil
	}
	s.mu.Lock()
	if s.notePathCacheLoaded {
		cache := s.cachedNotePathCache
		s.mu.Unlock()
		return cache, nil
	}
	generation := s.cacheGeneration
	index, err := s.overlayIndexLocked(ctx)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	cache, err := s.buildNotePathCache(ctx, index)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.cacheGeneration == generation {
		s.cachedNotePathCache = cache
		s.notePathCacheLoaded = true
	}
	s.mu.Unlock()
	return cache, nil
}

func (s *Scope) notePathCacheLocked(ctx context.Context) (*obsidian.NotePathCache, error) {
	if s == nil || s.service == nil || s.service.NoteReader == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.notePathCacheLoaded {
		return s.cachedNotePathCache, nil
	}
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return nil, err
	}
	cache, err := s.buildNotePathCache(ctx, index)
	if err != nil {
		return nil, err
	}
	s.cachedNotePathCache, s.notePathCacheLoaded = cache, true
	return cache, nil
}

func (s *Scope) buildNotePathCache(ctx context.Context, index *ReadOverlayIndex) (*obsidian.NotePathCache, error) {
	notes, err := s.service.NoteReader.GetNotesList(s.service.VaultDef)
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, err
	}
	var aliases map[string][]string
	if provider, ok := s.service.NoteReader.(obsidian.NoteEntriesProvider); ok {
		entries, err := provider.NoteEntriesSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		aliases = obsidian.AliasesFromNoteEntries(entries)
	}
	if aliases == nil {
		if provider := s.service.Store; provider != nil {
			rows, err := provider.CurrentNoteAliases(ctx)
			if err != nil {
				return nil, err
			}
			aliases = rows
		}
	}
	if index != nil {
		paths := make(map[string]bool, len(notes)+len(index.TouchedPaths))
		for _, path := range notes {
			paths[path] = true
		}
		stagedAliases := make(map[string][]string, len(aliases)+len(index.TouchedPaths))
		for path, values := range aliases {
			stagedAliases[path] = values
		}
		for path, snapshot := range index.SnapshotsByPath {
			if snapshot != nil {
				paths[path] = true
				stagedAliases[path] = obsidian.AliasListFromFrontmatter(snapshot.Frontmatter)
			}
		}
		for _, record := range index.RecordsByRef {
			if record.Ref.Kind == ontology.NodeKindNote {
				paths[record.Ref.NotePath] = true
				stagedAliases[record.Ref.NotePath] = obsidian.AliasListFromFrontmatter(record.Frontmatter)
			}
		}
		for path := range index.DeletedPaths {
			delete(paths, path)
			delete(stagedAliases, path)
		}
		notes = make([]string, 0, len(paths))
		for path := range paths {
			notes = append(notes, path)
		}
		sort.Strings(notes)
		aliases = stagedAliases
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return obsidian.BuildNotePathCacheWithAliases(notes, aliases), nil
}
