package codeanchor

import (
	"sort"
	"strings"
)

type runtimeCodeIndexSource struct {
	Path             string
	SymbolRefs       []SymbolRefRow
	ImportRefs       []ImportRefRow
	ModuleDefs       []ModuleDefRow
	IntelAnchors     []IntelAnchor
	DefDeltas        DefDeltas
	RefFootprint     DurableRefFootprint
	ScopeReady       ScopeReadyFootprint
	HasRebuildInputs bool
}

// NoteRuntimeCodeIndexWorks records freshly persisted code-index outputs so
// same-run rebuild/scoping can reuse them without reloading from sqlite.
func (s *Service) NoteRuntimeCodeIndexWorks(works []CodeIndexWork) {
	if s == nil || len(works) == 0 {
		return
	}
	s.runtimeCodeMu.Lock()
	defer s.runtimeCodeMu.Unlock()

	for _, work := range works {
		path := normalizeRuntimeCodePath(work.Path)
		if path == "" {
			continue
		}
		if prev, ok := s.runtimeCodeSources[path]; ok {
			s.removeRuntimeCallFiles(prev)
		}
		next := runtimeCodeIndexSource{
			Path:             path,
			SymbolRefs:       cloneSymbolRefRows(work.SymbolRefs),
			ImportRefs:       cloneImportRefRows(work.ImportRefs),
			ModuleDefs:       cloneModuleDefRows(work.ModuleDefs),
			IntelAnchors:     cloneIntelAnchors(work.IntelAnchors),
			DefDeltas:        work.DefDeltas,
			RefFootprint:     work.RefFootprint,
			ScopeReady:       work.ScopeReady,
			HasRebuildInputs: work.Summary.ParseStatus.TrustedForIndexing(),
		}
		next.Path = path
		next.RefFootprint.Path = path
		next.ScopeReady.Path = path
		s.runtimeCodeSources[path] = next
		s.runtimeCodeSeq++
		s.runtimeCodeVersions[path] = s.runtimeCodeSeq
		s.addRuntimeCallFiles(next)
	}
}

func (s *Service) runtimeCodeSourcesByPath(paths []string) map[string]runtimeCodeIndexSource {
	if s == nil || len(paths) == 0 {
		return nil
	}
	s.runtimeCodeMu.RLock()
	defer s.runtimeCodeMu.RUnlock()
	out := make(map[string]runtimeCodeIndexSource, len(paths))
	for _, path := range paths {
		path = normalizeRuntimeCodePath(path)
		if path == "" {
			continue
		}
		src, ok := s.runtimeCodeSources[path]
		if !ok {
			continue
		}
		out[path] = cloneRuntimeCodeIndexSource(src)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) RuntimeCodeSourceVersions(paths []string) map[string]uint64 {
	if s == nil || len(paths) == 0 {
		return nil
	}
	s.runtimeCodeMu.RLock()
	defer s.runtimeCodeMu.RUnlock()
	out := make(map[string]uint64, len(paths))
	for _, path := range paths {
		path = normalizeRuntimeCodePath(path)
		if path == "" {
			continue
		}
		if version := s.runtimeCodeVersions[path]; version > 0 {
			out[path] = version
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) runtimeCallFilesByRefs(refs []SymbolRef) map[string][]string {
	if s == nil || len(refs) == 0 {
		return nil
	}
	s.runtimeCodeMu.RLock()
	defer s.runtimeCodeMu.RUnlock()
	out := make(map[string][]string, len(refs))
	for _, ref := range refs {
		key := symbolRefKey(ref)
		if key == "" {
			continue
		}
		paths := s.runtimeCallFiles[key]
		if len(paths) == 0 {
			continue
		}
		items := make([]string, 0, len(paths))
		for path := range paths {
			items = append(items, path)
		}
		sort.Strings(items)
		out[key] = items
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) addRuntimeCallFiles(src runtimeCodeIndexSource) {
	for _, row := range src.SymbolRefs {
		if row.RefKind != RefKindCalls {
			continue
		}
		key := symbolRefKey(SymbolRef{
			Lang:   row.DstLang,
			Pkg:    row.DstPkg,
			Name:   row.DstName,
			Member: row.DstMember,
		})
		if key == "" || src.Path == "" {
			continue
		}
		if s.runtimeCallFiles[key] == nil {
			s.runtimeCallFiles[key] = make(map[string]struct{})
		}
		s.runtimeCallFiles[key][src.Path] = struct{}{}
	}
}

func (s *Service) removeRuntimeCallFiles(src runtimeCodeIndexSource) {
	for _, row := range src.SymbolRefs {
		if row.RefKind != RefKindCalls {
			continue
		}
		key := symbolRefKey(SymbolRef{
			Lang:   row.DstLang,
			Pkg:    row.DstPkg,
			Name:   row.DstName,
			Member: row.DstMember,
		})
		if key == "" || src.Path == "" {
			continue
		}
		paths := s.runtimeCallFiles[key]
		delete(paths, src.Path)
		if len(paths) == 0 {
			delete(s.runtimeCallFiles, key)
		}
	}
}

func cloneRuntimeCodeIndexSource(src runtimeCodeIndexSource) runtimeCodeIndexSource {
	return runtimeCodeIndexSource{
		Path:             src.Path,
		SymbolRefs:       cloneSymbolRefRows(src.SymbolRefs),
		ImportRefs:       cloneImportRefRows(src.ImportRefs),
		ModuleDefs:       cloneModuleDefRows(src.ModuleDefs),
		IntelAnchors:     cloneIntelAnchors(src.IntelAnchors),
		DefDeltas:        src.DefDeltas,
		RefFootprint:     src.RefFootprint,
		ScopeReady:       src.ScopeReady,
		HasRebuildInputs: src.HasRebuildInputs,
	}
}

func cloneSymbolRefRows(rows []SymbolRefRow) []SymbolRefRow {
	return append([]SymbolRefRow(nil), rows...)
}

func cloneImportRefRows(rows []ImportRefRow) []ImportRefRow {
	return append([]ImportRefRow(nil), rows...)
}

func cloneModuleDefRows(rows []ModuleDefRow) []ModuleDefRow {
	return append([]ModuleDefRow(nil), rows...)
}

func cloneIntelAnchors(rows []IntelAnchor) []IntelAnchor {
	return append([]IntelAnchor(nil), rows...)
}

func normalizeRuntimeCodePath(path string) string {
	return strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
}
