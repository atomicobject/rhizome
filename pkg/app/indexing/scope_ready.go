package indexing

import (
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/bmatcuk/doublestar/v4"
)

type scopeReadyIndex struct {
	anchorBySymbolKey     map[string]map[int64]struct{}
	anchorByAnnotationKey map[string]map[int64]struct{}
	anchorsByID           map[int64]codeanchor.Anchor
	noteAnchorIDsByPath   map[string]map[int64]struct{}

	dirtySet   map[int64]struct{}
	readySet   map[int64]struct{}
	readyQueue []int64

	globalFallback bool
}

func newScopeReadyIndex() scopeReadyIndex {
	return scopeReadyIndex{
		anchorBySymbolKey:     make(map[string]map[int64]struct{}),
		anchorByAnnotationKey: make(map[string]map[int64]struct{}),
		anchorsByID:           make(map[int64]codeanchor.Anchor),
		noteAnchorIDsByPath:   make(map[string]map[int64]struct{}),
		dirtySet:              make(map[int64]struct{}),
		readySet:              make(map[int64]struct{}),
	}
}

func (s *scopeReadyIndex) seed(byPath map[string][]codeanchor.Anchor) {
	if s == nil {
		return
	}
	for path, anchors := range byPath {
		s.replaceNoteAnchors(path, anchors)
	}
}

func (s *scopeReadyIndex) replaceNoteAnchors(path string, anchors []codeanchor.Anchor) {
	if s == nil {
		return
	}
	path = normalizeNotePath(path)
	if path == "" {
		return
	}
	if existing := s.noteAnchorIDsByPath[path]; len(existing) > 0 {
		for id := range existing {
			s.removeAnchor(id)
		}
		delete(s.noteAnchorIDsByPath, path)
	}
	if len(anchors) == 0 {
		s.refreshGlobalFallback()
		return
	}
	current := make(map[int64]struct{}, len(anchors))
	for _, anchor := range anchors {
		s.addAnchor(anchor)
		current[anchor.ID] = struct{}{}
	}
	s.noteAnchorIDsByPath[path] = current
	s.refreshGlobalFallback()
}

func (s *scopeReadyIndex) enqueueDirect(ids []int64) {
	if s == nil {
		return
	}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		s.dirtySet[id] = struct{}{}
		s.enqueueReady(id)
	}
}

func (s *scopeReadyIndex) noteCodeFootprints(footprints []codeanchor.ScopeReadyFootprint) {
	if s == nil {
		return
	}
	for _, footprint := range footprints {
		path := normalizeNotePath(footprint.Path)
		if path == "" {
			continue
		}
		for _, key := range footprint.SymbolKeys {
			for id := range s.anchorBySymbolKey[key] {
				s.dirtySet[id] = struct{}{}
				s.enqueueReady(id)
			}
		}
		for _, key := range footprint.AnnotationKeys {
			for id := range s.anchorByAnnotationKey[key] {
				s.dirtySet[id] = struct{}{}
				s.enqueueReady(id)
			}
		}
		for id, anchor := range s.anchorsByID {
			switch anchor.Kind {
			case codeanchor.AnchorPath:
				if matchesPathPrefix(path, anchor.PathPrefix) {
					s.dirtySet[id] = struct{}{}
					s.enqueueReady(id)
				}
			case codeanchor.AnchorGlob:
				if matchesAnyGlob(path, anchor.Globs) {
					s.dirtySet[id] = struct{}{}
					s.enqueueReady(id)
				}
			}
		}
	}
}

func (s *scopeReadyIndex) hasGlobalFallback() bool {
	return s != nil && s.globalFallback
}

func (s *scopeReadyIndex) drainReady() []int64 {
	if s == nil || len(s.readyQueue) == 0 {
		return nil
	}
	out := append([]int64(nil), s.readyQueue...)
	s.readyQueue = nil
	clear(s.readySet)
	return out
}

func (s *scopeReadyIndex) markRebuilt(ids []int64) {
	if s == nil {
		return
	}
	for _, id := range ids {
		delete(s.dirtySet, id)
		delete(s.readySet, id)
	}
}

func (s *scopeReadyIndex) enqueueReady(id int64) {
	if _, ok := s.readySet[id]; ok {
		return
	}
	s.readySet[id] = struct{}{}
	s.readyQueue = append(s.readyQueue, id)
}

func (s *scopeReadyIndex) addAnchor(anchor codeanchor.Anchor) {
	s.anchorsByID[anchor.ID] = anchor
	switch anchor.Kind {
	case codeanchor.AnchorFunc, codeanchor.AnchorBaseClass, codeanchor.AnchorKind("functionUse"):
		if anchor.BaseSym != nil {
			key := scopeSymbolKey(*anchor.BaseSym)
			if key != "" {
				addScopeAnchorID(s.anchorBySymbolKey, key, anchor.ID)
			}
		}
	case codeanchor.AnchorAnnotation:
		if anchor.Ann != nil {
			key := scopeSymbolKey(anchor.Ann.Symbol)
			if key != "" {
				addScopeAnchorID(s.anchorByAnnotationKey, key, anchor.ID)
			}
		}
	}
}

func (s *scopeReadyIndex) removeAnchor(id int64) {
	anchor, ok := s.anchorsByID[id]
	if !ok {
		return
	}
	switch anchor.Kind {
	case codeanchor.AnchorFunc, codeanchor.AnchorBaseClass, codeanchor.AnchorKind("functionUse"):
		if anchor.BaseSym != nil {
			removeScopeAnchorID(s.anchorBySymbolKey, scopeSymbolKey(*anchor.BaseSym), id)
		}
	case codeanchor.AnchorAnnotation:
		if anchor.Ann != nil {
			removeScopeAnchorID(s.anchorByAnnotationKey, scopeSymbolKey(anchor.Ann.Symbol), id)
		}
	}
	delete(s.anchorsByID, id)
	delete(s.dirtySet, id)
	delete(s.readySet, id)
}

func (s *scopeReadyIndex) refreshGlobalFallback() {
	s.globalFallback = false
	for _, anchor := range s.anchorsByID {
		if scopeWildcardAnchor(anchor) {
			s.globalFallback = true
			return
		}
	}
}

func addScopeAnchorID(index map[string]map[int64]struct{}, key string, id int64) {
	if key == "" || id <= 0 {
		return
	}
	current := index[key]
	if current == nil {
		current = make(map[int64]struct{})
		index[key] = current
	}
	current[id] = struct{}{}
}

func removeScopeAnchorID(index map[string]map[int64]struct{}, key string, id int64) {
	if key == "" {
		return
	}
	current := index[key]
	if current == nil {
		return
	}
	delete(current, id)
	if len(current) == 0 {
		delete(index, key)
	}
}

func scopeSymbolKey(ref codeanchor.SymbolRef) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ToLower(string(ref.Lang))+"|"+ref.Pkg+"|"+ref.Name, "\\", "/"))
}

func normalizeNotePath(path string) string {
	return strings.TrimSpace(strings.ReplaceAll(filepath.ToSlash(path), "\\", "/"))
}

func matchesPathPrefix(target, prefix string) bool {
	target = normalizeNotePath(target)
	prefix = normalizeNotePath(prefix)
	if target == "" || prefix == "" {
		return false
	}
	if target == prefix {
		return true
	}
	return strings.HasPrefix(target, prefix+"/")
}

func matchesAnyGlob(target string, globs []string) bool {
	target = normalizeNotePath(target)
	if target == "" || len(globs) == 0 {
		return false
	}
	for _, pat := range globs {
		pat = normalizeNotePath(pat)
		if pat == "" {
			continue
		}
		ok, _ := doublestar.Match(pat, target)
		if ok {
			return true
		}
	}
	return false
}

func scopeWildcardAnchor(anchor codeanchor.Anchor) bool {
	switch anchor.Kind {
	case codeanchor.AnchorBaseClass, codeanchor.AnchorFunc:
		return anchor.BaseSym != nil && anchor.BaseSym.Pkg == ""
	default:
		return false
	}
}

func normalizeScopeAnchorIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
