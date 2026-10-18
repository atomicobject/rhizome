package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

func (s *Server) listTree(ctx context.Context, rel string, limit int) (TreeResponse, error) {
	if limit <= 0 {
		limit = 200
	}
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, rel)
	if err != nil {
		return TreeResponse{}, err
	}
	rel = readPath.rel
	info, err := os.Stat(readPath.abs)
	if err != nil {
		return TreeResponse{}, err
	}
	if !info.IsDir() {
		return TreeResponse{}, errors.New("path is not a directory")
	}
	if ignoreMatcher := s.runtime.IgnoreMatcher(); ignoreMatcher != nil {
		if ignoreMatcher.IsIgnored(rel, true) || ignoreMatcher.IsIgnored(readPath.resolvedRel, true) {
			return TreeResponse{}, errors.New("path is ignored")
		}
	}

	entries, err := os.ReadDir(readPath.abs)
	if err != nil {
		return TreeResponse{}, err
	}
	out := TreeResponse{Path: rel}

	for _, entry := range entries {
		name := entry.Name()
		candidate, ok := s.treeEntry(rel, name)
		if !ok {
			continue
		}
		entryRel := candidate.path.rel
		kind, lang := s.classifyFile(entryRel)
		if candidate.isDir {
			kind = "dir"
		}
		if kind == "" && !candidate.isDir {
			continue
		}
		out.Entries = append(out.Entries, TreeEntry{
			Name:        name,
			Path:        entryRel,
			Kind:        kind,
			Lang:        lang,
			HasChildren: candidate.isDir && s.dirHasChildren(entryRel),
		})
		if len(out.Entries) >= limit {
			out.Truncated = true
			break
		}
	}

	sort.SliceStable(out.Entries, func(i, j int) bool {
		if out.Entries[i].Kind != out.Entries[j].Kind {
			return out.Entries[i].Kind == "dir"
		}
		return out.Entries[i].Name < out.Entries[j].Name
	})
	return out, nil
}

func (s *Server) dirHasChildren(rel string) bool {
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, rel)
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(readPath.abs)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		candidate, ok := s.treeEntry(rel, entry.Name())
		if !ok {
			continue
		}
		if candidate.isDir {
			return true
		}
		kind, _ := s.classifyFile(candidate.path.rel)
		if kind != "" {
			return true
		}
	}
	return false
}

type vaultTreeEntry struct {
	path  vaultReadPath
	isDir bool
}

func (s *Server) treeEntry(parentRel, name string) (vaultTreeEntry, bool) {
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, filepath.Join(parentRel, name))
	if err != nil {
		return vaultTreeEntry{}, false
	}
	info, err := os.Stat(readPath.abs)
	if err != nil {
		return vaultTreeEntry{}, false
	}
	if ignoreMatcher := s.runtime.IgnoreMatcher(); ignoreMatcher != nil &&
		(ignoreMatcher.IsIgnoredShallow(readPath.rel, info.IsDir()) ||
			ignoreMatcher.IsIgnored(readPath.resolvedRel, info.IsDir())) {
		return vaultTreeEntry{}, false
	}
	return vaultTreeEntry{path: readPath, isDir: info.IsDir()}, true
}
