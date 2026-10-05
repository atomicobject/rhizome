package validate

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// validationRunSnapshot is the immutable source view shared by every check in
// one suite execution. Repair authority retains the caller's live reader; only
// validation reads use this captured view.
type validationRunSnapshot struct {
	sources    []notemeta.NoteSourceSnapshot
	byPath     map[string]notemeta.NoteSourceSnapshot
	paths      []string
	candidates brokenLinkMatcherIndex
}

func captureValidationRunSnapshot(ctx context.Context, runCtx RunContext) (*validationRunSnapshot, error) {
	reader := runCtx.NoteReader
	if runCtx.postcheckPaths != nil {
		reader = scopedPostcheckNoteReader{NoteReader: reader, paths: runCtx.postcheckPaths}
	}
	sources, err := runCtx.NoteMetadata.BuildNoteSourceSnapshots(ctx, runCtx.VaultDef, reader)
	if err != nil {
		return nil, fmt.Errorf("capture validation source snapshot: %w", err)
	}
	snapshot := &validationRunSnapshot{
		sources: append([]notemeta.NoteSourceSnapshot(nil), sources...),
		byPath:  make(map[string]notemeta.NoteSourceSnapshot, len(sources)),
		paths:   make([]string, 0, len(sources)),
	}
	for _, source := range sources {
		path := source.Path.String()
		snapshot.byPath[path] = source
		snapshot.paths = append(snapshot.paths, path)
	}
	sort.Strings(snapshot.paths)
	snapshot.candidates = newBrokenLinkMatcherIndex(snapshot.paths)
	return snapshot, nil
}

// scopedPostcheckNoteReader restricts source capture while retaining the live
// reader for source bytes. Deleted paths are absent from its normal inventory.
type scopedPostcheckNoteReader struct {
	obsidian.NoteReader
	paths map[string]struct{}
}

func (r scopedPostcheckNoteReader) GetNotesList(def obsidian.VaultDefinition) ([]string, error) {
	all, err := r.NoteReader.GetNotesList(def)
	if err != nil {
		return nil, err
	}
	selected := make([]string, 0, len(r.paths))
	for _, path := range all {
		if _, ok := r.paths[path]; ok {
			selected = append(selected, path)
		}
	}
	return selected, nil
}

func checksNeedValidationRunSnapshot(checks []string) bool {
	for _, check := range checks {
		switch check {
		case CheckOntology, CheckIdentifiers, CheckBrokenLinks, CheckLinkHygiene,
			CheckCodeFrontmatter, CheckFrozenScopeDrift, CheckFragileExternal, CheckOrphanBlockIDs:
			return true
		}
	}
	return false
}

func (s *validationRunSnapshot) reader(base obsidian.NoteReader) obsidian.NoteReader {
	return &validationSnapshotNoteReader{snapshot: s, base: base}
}

type validationSnapshotNoteReader struct {
	snapshot *validationRunSnapshot
	base     obsidian.NoteReader
}

func (r *validationSnapshotNoteReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	if source, ok := r.source(path); ok {
		return source.Content, nil
	}
	return "", fmt.Errorf("note %q is outside the validation source snapshot", path)
}

func (r *validationSnapshotNoteReader) GetContentsContext(ctx context.Context, cfg obsidian.VaultDefinition, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return r.GetContents(cfg, path)
}

func (r *validationSnapshotNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return append([]string(nil), r.snapshot.paths...), nil
}

func (r *validationSnapshotNoteReader) GetNotesListContext(ctx context.Context, cfg obsidian.VaultDefinition) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.GetNotesList(cfg)
}

func (r *validationSnapshotNoteReader) GetModTime(_ obsidian.VaultDefinition, path string) (time.Time, error) {
	if source, ok := r.source(path); ok {
		return time.Unix(source.Mtime, 0), nil
	}
	return time.Time{}, fmt.Errorf("note %q is outside the validation source snapshot", path)
}

func (r *validationSnapshotNoteReader) Title(path string) (string, bool) {
	if source, ok := r.source(path); ok {
		if title := strings.TrimSpace(source.Title); title != "" {
			return title, true
		}
		return strings.TrimSuffix(filepath.Base(source.Path.String()), filepath.Ext(source.Path.String())), true
	}
	if r.base != nil {
		return r.base.Title(path)
	}
	return "", false
}

func (r *validationSnapshotNoteReader) source(path string) (notemeta.NoteSourceSnapshot, bool) {
	if r == nil || r.snapshot == nil {
		return notemeta.NoteSourceSnapshot{}, false
	}
	normalized, err := paths.CleanNotePath(path)
	if err == nil {
		if source, ok := r.snapshot.byPath[normalized.String()]; ok {
			return source, true
		}
	}
	if filepath.Ext(path) == "" {
		normalized = paths.NormalizeNote(path)
		if source, ok := r.snapshot.byPath[normalized.String()]; ok {
			return source, true
		}
	}
	return notemeta.NoteSourceSnapshot{}, false
}
