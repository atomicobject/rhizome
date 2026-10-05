package coderefs

// Docs:
// - [Coderefs (Hub)](docs/hubs/Coderefs (Hub).md)
// - [Coderefs - scanning + indexing](docs/reference/guides/Coderefs - scanning + indexing.md)

import (
	"sync"
)

// Index maintains a bidirectional mapping between source code files and vault notes.
type Index struct {
	mu     sync.RWMutex
	byNote map[string][]CodeRef // Target note path -> refs pointing to it
	byFile map[string][]CodeRef // Source file path -> refs it contains
}

// NewIndex creates a new, empty code reference index.
func NewIndex() *Index {
	return &Index{
		byNote: make(map[string][]CodeRef),
		byFile: make(map[string][]CodeRef),
	}
}

// ReplaceFile updates the references for a specific source file.
// It atomically removes old references and adds new ones.
//
// SourceFile and Target are expected to already be normalized by the scanner or
// caller. This in-memory index does not canonicalize keys; doing so here would
// hide path-boundary bugs that later SQLite/doc-link joins need surfaced.
func (idx *Index) ReplaceFile(file string, refs []CodeRef) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old references from the byNote map
	if oldRefs, ok := idx.byFile[file]; ok {
		for _, r := range oldRefs {
			idx.removeRefFromNote(r)
		}
	}

	// Update byFile map
	if len(refs) == 0 {
		delete(idx.byFile, file)
		return
	}
	idx.byFile[file] = refs

	// Add new references to byNote map
	for _, r := range refs {
		idx.addRefToNote(r)
	}
}

// RemoveFile removes all references associated with a source file.
func (idx *Index) RemoveFile(file string) {
	idx.ReplaceFile(file, nil)
}

// RefsByNote returns all code references pointing to the given note path.
func (idx *Index) RefsByNote(notePath string) []CodeRef {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	refs := idx.byNote[notePath]
	if len(refs) == 0 {
		return nil
	}
	// Return a copy to prevent data races if the caller modifies the slice
	out := make([]CodeRef, len(refs))
	copy(out, refs)
	return out
}

// RefsByFile returns all code references found in the given source file.
func (idx *Index) RefsByFile(filePath string) []CodeRef {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	refs := idx.byFile[filePath]
	if len(refs) == 0 {
		return nil
	}
	out := make([]CodeRef, len(refs))
	copy(out, refs)
	return out
}

// AllRefsByNote returns a copy of the entire byNote map.
// Used by CodeRefProvider to expose all code references.
func (idx *Index) AllRefsByNote() map[string][]CodeRef {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	out := make(map[string][]CodeRef, len(idx.byNote))
	for k, v := range idx.byNote {
		copied := make([]CodeRef, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

// AllRefsByFile returns a copy of the entire byFile map.
func (idx *Index) AllRefsByFile() map[string][]CodeRef {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	out := make(map[string][]CodeRef, len(idx.byFile))
	for k, v := range idx.byFile {
		copied := make([]CodeRef, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

// Helper: remove a single ref from the byNote map
// Caller must hold the lock
func (idx *Index) removeRefFromNote(ref CodeRef) {
	refs := idx.byNote[ref.Target]
	for i, r := range refs {
		if r.SourceFile == ref.SourceFile && r.Line == ref.Line && r.Kind == ref.Kind {
			// Found it. Remove it by swapping with the last element.
			// Order doesn't strictly matter, but swapping is O(1).
			// However, to keep things deterministic for tests, let's use standard deletion.
			idx.byNote[ref.Target] = append(refs[:i], refs[i+1:]...)
			break
		}
	}
	if len(idx.byNote[ref.Target]) == 0 {
		delete(idx.byNote, ref.Target)
	}
}

// Helper: add a single ref to the byNote map
// Caller must hold the lock
func (idx *Index) addRefToNote(ref CodeRef) {
	idx.byNote[ref.Target] = append(idx.byNote[ref.Target], ref)
}
