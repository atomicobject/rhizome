package noteownership

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
)

const (
	UnreadableSourceDiagnosticCode   = "unreadable_source"
	UnreadableSourceDiagnosticDetail = "authored source could not be read"
	UnstableSourceDiagnosticCode     = "source_changed_during_read"
	UnstableSourceDiagnosticDetail   = "authored source changed while being read"
)

// TransitionPlan is a pure, deterministic source-observation result. It
// never mutates a store. Transitions and sources are returned by deep copy.
type TransitionPlan struct {
	transitions []semdb.OwnershipTransition
	sources     map[paths.NotePath]noteformat.AuthoredSource
}

func (p TransitionPlan) Transitions() []semdb.OwnershipTransition {
	return cloneTransitions(p.transitions)
}
func (p TransitionPlan) Source(path paths.NotePath) (noteformat.AuthoredSource, bool) {
	source, ok := p.sources[path]
	return source, ok
}

// TransitionInput controls source preparation. AffectedPaths are explicit
// live/forced paths. They opt an already note-owned projectable source into
// sealed read observation; an unreadable or unstable source then emits a
// fatal note transition so the injected store atomically clears derived rows.
type TransitionInput struct {
	VaultPaths    paths.VaultPaths
	Snapshot      Snapshot
	Runtime       noteformat.Runtime
	ObservedAt    int64
	AffectedPaths map[string]struct{}
}
type filesystem interface {
	Stat(string) (fs.FileInfo, error)
	ReadFile(string) ([]byte, error)
}
type osFilesystem struct{}

func (osFilesystem) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }
func (osFilesystem) ReadFile(path string) ([]byte, error)  { return os.ReadFile(path) }

func BuildTransitionPlan(ctx context.Context, input TransitionInput) (TransitionPlan, error) {
	return buildTransitionPlanWithFilesystem(ctx, input, osFilesystem{})
}

func buildTransitionPlanWithFilesystem(ctx context.Context, input TransitionInput, filesystem filesystem) (TransitionPlan, error) {
	if err := ctx.Err(); err != nil {
		return TransitionPlan{}, err
	}
	if input.ObservedAt < 0 {
		return TransitionPlan{}, fmt.Errorf("ownership transition observation time must not be negative")
	}
	if input.VaultPaths.Root() == "" {
		return TransitionPlan{}, fmt.Errorf("ownership transition vault root is required")
	}
	if filesystem == nil {
		return TransitionPlan{}, fmt.Errorf("ownership transition filesystem is required")
	}
	candidates := input.Snapshot.Candidates()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path.String() < candidates[j].Path.String() })
	result := TransitionPlan{sources: map[paths.NotePath]noteformat.AuthoredSource{}}
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return TransitionPlan{}, err
		}
		path, err := cleanNotePath(c.Path)
		if err != nil {
			return TransitionPlan{}, err
		}
		if _, duplicate := seen[path.String()]; duplicate {
			return TransitionPlan{}, fmt.Errorf("ownership transition candidate repeats path %q", path)
		}
		seen[path.String()] = struct{}{}
		target, err := transitionTarget(c.Owner)
		if err != nil {
			return TransitionPlan{}, err
		}
		previous, err := transitionTarget(c.PreviousOwner)
		if err != nil {
			return TransitionPlan{}, err
		}
		if target != semdb.OwnershipTargetNote {
			if (target == semdb.OwnershipTargetCode && previous == semdb.OwnershipTargetNote) || (target == semdb.OwnershipTargetUnowned && previous != semdb.OwnershipTargetUnowned) {
				result.transitions = append(result.transitions, semdb.OwnershipTransition{Path: path.String(), Target: target})
			}
			continue
		}
		descriptor, projectable, err := descriptorFor(input.Runtime, c, path)
		if err != nil {
			return TransitionPlan{}, err
		}
		if err := verifyPath(input.VaultPaths, c.AbsPath, path); err != nil {
			return TransitionPlan{}, err
		}
		affected := affected(input.AffectedPaths, path)
		if projectable && previous != semdb.OwnershipTargetCode && !affected {
			continue
		}
		state, source, readable, err := noteState(filesystem, input.VaultPaths, c, path, descriptor, input.ObservedAt)
		if err != nil {
			return TransitionPlan{}, err
		}
		if err := validateState(state, descriptor); err != nil {
			return TransitionPlan{}, err
		}
		result.transitions = append(result.transitions, semdb.OwnershipTransition{Path: path.String(), Target: semdb.OwnershipTargetNote, Note: &state})
		if projectable && readable {
			result.sources[path] = source
		}
	}
	if len(result.sources) == 0 {
		result.sources = nil
	}
	return result, nil
}

func affected(paths map[string]struct{}, path paths.NotePath) bool {
	_, ok := paths[path.String()]
	return ok
}
func cleanNotePath(raw paths.RelPath) (paths.NotePath, error) {
	path, err := paths.CleanNotePath(raw.String())
	if err != nil || path == "" || path.String() != raw.String() {
		return "", fmt.Errorf("ownership transition path must be canonical and vault-relative: %q", raw)
	}
	return path, nil
}
func transitionTarget(owner notediscovery.Owner) (semdb.OwnershipTarget, error) {
	switch Target(owner) {
	case notediscovery.Note:
		return semdb.OwnershipTargetNote, nil
	case notediscovery.Code:
		return semdb.OwnershipTargetCode, nil
	case notediscovery.Unowned:
		return semdb.OwnershipTargetUnowned, nil
	}
	return "", fmt.Errorf("unsupported owner %q", owner)
}
func descriptorFor(runtime noteformat.Runtime, c Candidate, path paths.NotePath) (noteformat.Descriptor, bool, error) {
	if !c.Present || c.AbsPath == "" || c.ModTime < 0 || c.Size < 0 || strings.TrimSpace(string(c.Provider)) == "" {
		return noteformat.Descriptor{}, false, fmt.Errorf("note-owned candidate is incomplete")
	}
	provider, known := runtime.Provider(c.Provider)
	if !known {
		return noteformat.Descriptor{}, false, fmt.Errorf("note-owned candidate uses unregistered provider %q", c.Provider)
	}
	claimed, claims := runtime.ProviderForPath(paths.RelPath(path))
	if !claims || claimed.Descriptor().ID != provider.Descriptor().ID {
		return noteformat.Descriptor{}, false, fmt.Errorf("selected provider %q does not claim path %q", c.Provider, path)
	}
	return provider.Descriptor(), runtime.CanProject(c.Provider), nil
}
func verifyPath(vault paths.VaultPaths, abs paths.AbsPath, expected paths.NotePath) error {
	if abs == "" {
		return fmt.Errorf("note-owned candidate has no absolute path")
	}
	actual, err := vault.RelNotePathStrict(abs.String())
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("candidate absolute path resolves to %q, not canonical path %q", actual, expected)
	}
	return nil
}

type readFailure struct {
	code, detail string
	mtime, size  int64
}

func noteState(filesystem filesystem, vault paths.VaultPaths, c Candidate, path paths.NotePath, descriptor noteformat.Descriptor, observedAt int64) (semdb.NoteSourceState, noteformat.AuthoredSource, bool, error) {
	source, failure, err := stableRead(filesystem, vault, c, path, descriptor)
	if err != nil {
		return semdb.NoteSourceState{}, noteformat.AuthoredSource{}, false, err
	}
	if failure != nil {
		return fatalState(path, descriptor, *failure, observedAt), noteformat.AuthoredSource{}, false, nil
	}
	return semdb.NoteSourceState{Title: FallbackTitle(path), FormatID: string(descriptor.ID), ContentHash: source.ContentHash(), Mtime: source.Mtime(), Size: source.Size(), ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion, Status: semdb.NoteProjectionStatusStale, ObservedAt: observedAt}, source, true, nil
}
func stableRead(filesystem filesystem, vault paths.VaultPaths, c Candidate, path paths.NotePath, descriptor noteformat.Descriptor) (noteformat.AuthoredSource, *readFailure, error) {
	var last readFailure
	for attempts := 0; attempts < 2; attempts++ {
		if err := verifyPath(vault, c.AbsPath, path); err != nil {
			return noteformat.AuthoredSource{}, nil, err
		}
		before, err := filesystem.Stat(c.AbsPath.String())
		if err != nil {
			return noteformat.AuthoredSource{}, &readFailure{code: UnreadableSourceDiagnosticCode, detail: UnreadableSourceDiagnosticDetail}, nil
		}
		if !before.Mode().IsRegular() {
			return noteformat.AuthoredSource{}, &readFailure{code: UnreadableSourceDiagnosticCode, detail: UnreadableSourceDiagnosticDetail, mtime: before.ModTime().Unix(), size: before.Size()}, nil
		}
		bytes, err := filesystem.ReadFile(c.AbsPath.String())
		if err != nil {
			return noteformat.AuthoredSource{}, &readFailure{code: UnreadableSourceDiagnosticCode, detail: UnreadableSourceDiagnosticDetail, mtime: before.ModTime().Unix(), size: before.Size()}, nil
		}
		after, err := filesystem.Stat(c.AbsPath.String())
		if err != nil {
			return noteformat.AuthoredSource{}, &readFailure{code: UnreadableSourceDiagnosticCode, detail: UnreadableSourceDiagnosticDetail, mtime: before.ModTime().Unix(), size: before.Size()}, nil
		}
		if err := verifyPath(vault, c.AbsPath, path); err != nil {
			return noteformat.AuthoredSource{}, nil, err
		}
		last = readFailure{code: UnstableSourceDiagnosticCode, detail: UnstableSourceDiagnosticDetail, mtime: after.ModTime().Unix(), size: after.Size()}
		if !after.Mode().IsRegular() || !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() || int64(len(bytes)) != after.Size() {
			continue
		}
		source, err := noteformat.NewAuthoredSource(path, descriptor, bytes, after.ModTime().Unix())
		if err != nil {
			return noteformat.AuthoredSource{}, nil, err
		}
		return source, nil, nil
	}
	return noteformat.AuthoredSource{}, &last, nil
}
func fatalState(path paths.NotePath, descriptor noteformat.Descriptor, failure readFailure, observedAt int64) semdb.NoteSourceState {
	return semdb.NoteSourceState{Title: FallbackTitle(path), FormatID: string(descriptor.ID), Mtime: failure.mtime, Size: failure.size, ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion, Status: semdb.NoteProjectionStatusFatal, DiagnosticCode: failure.code, DiagnosticDetail: failure.detail, ObservedAt: observedAt}
}

// FallbackTitle preserves basename casing while removing only its final extension.
func FallbackTitle(path paths.NotePath) string {
	base := pathpkg.Base(path.String())
	ext := pathpkg.Ext(base)
	if ext == base {
		return base
	}
	return strings.TrimSuffix(base, ext)
}
func validateState(state semdb.NoteSourceState, descriptor noteformat.Descriptor) error {
	if state.FormatID != string(descriptor.ID) || state.ProviderVersion != descriptor.ProviderVersion || state.ProjectionVersion != descriptor.ProjectionVersion || state.Mtime < 0 || state.Size < 0 || state.ObservedAt < 0 {
		return fmt.Errorf("invalid ownership source state")
	}
	if state.Status == semdb.NoteProjectionStatusStale && strings.TrimSpace(state.ContentHash) != "" && state.DiagnosticCode == "" && state.DiagnosticDetail == "" {
		return nil
	}
	if state.Status == semdb.NoteProjectionStatusFatal && state.ContentHash == "" && ((state.DiagnosticCode == UnreadableSourceDiagnosticCode && state.DiagnosticDetail == UnreadableSourceDiagnosticDetail) || (state.DiagnosticCode == UnstableSourceDiagnosticCode && state.DiagnosticDetail == UnstableSourceDiagnosticDetail)) {
		return nil
	}
	return fmt.Errorf("invalid ownership source state")
}
func cloneTransitions(values []semdb.OwnershipTransition) []semdb.OwnershipTransition {
	result := make([]semdb.OwnershipTransition, len(values))
	for i, value := range values {
		result[i] = value
		if value.Note != nil {
			note := *value.Note
			result[i].Note = &note
		}
	}
	return result
}
