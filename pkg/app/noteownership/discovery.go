// Package noteownership discovers exclusive note/code ownership and prepares
// source-only ownership transitions. It has no indexing or bootstrap imports.
package noteownership

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const MarkdownFormatID noteformat.FormatID = "markdown"

// Candidate is one exclusive result from the single ownership walk.
// Its fields contain values only. Snapshot accessors always return copies.
type Candidate struct {
	Path          paths.RelPath
	PreviousOwner notediscovery.Owner
	Owner         notediscovery.Owner
	Provider      noteformat.FormatID
	Language      codeanchor.Lang
	Present       bool
	AbsPath       paths.AbsPath
	ModTime       int64
	Size          int64
}

// Snapshot is the immutable result of one discovery pass.
type Snapshot struct {
	candidates []Candidate
}

func newSnapshot(candidates []Candidate) (Snapshot, error) {
	result := append([]Candidate(nil), candidates...)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path.String() < result[j].Path.String()
	})
	for index, candidate := range result {
		path, err := paths.CleanRelPath(candidate.Path.String())
		if err != nil || path == "" || path != candidate.Path {
			return Snapshot{}, fmt.Errorf("ownership candidate path is not canonical: %q", candidate.Path)
		}
		if index > 0 && result[index-1].Path == candidate.Path {
			return Snapshot{}, fmt.Errorf("ownership candidate repeats path %q", candidate.Path)
		}
	}
	return Snapshot{candidates: result}, nil
}

func (s Snapshot) Candidates() []Candidate {
	return append([]Candidate(nil), s.candidates...)
}

func (s Snapshot) PresentNoteCandidates() []Candidate {
	return s.filter(func(candidate Candidate) bool {
		return candidate.Present && candidate.Owner == notediscovery.Note
	})
}

func (s Snapshot) CodeCandidates() []Candidate {
	return s.filter(func(candidate Candidate) bool {
		return candidate.Present && candidate.Owner == notediscovery.Code
	})
}

func (s Snapshot) OtherCandidates() []Candidate {
	return s.filter(func(candidate Candidate) bool {
		return candidate.Owner != notediscovery.Note && candidate.Owner != notediscovery.Code
	})
}

func (s Snapshot) OwnershipChangeCandidates() []Candidate {
	return s.filter(func(candidate Candidate) bool {
		return Target(candidate.PreviousOwner) != Target(candidate.Owner)
	})
}

func (s Snapshot) NoteKeepPaths() []paths.NotePath {
	result := make([]paths.NotePath, 0, len(s.candidates))
	for _, candidate := range s.candidates {
		if candidate.Present && candidate.Owner == notediscovery.Note {
			result = append(result, paths.NotePath(candidate.Path))
		}
	}
	return result
}

func (s Snapshot) CodeKeepPaths() []paths.CodePath {
	result := make([]paths.CodePath, 0, len(s.candidates))
	for _, candidate := range s.candidates {
		if candidate.Present && candidate.Owner == notediscovery.Code {
			result = append(result, paths.CodePath(candidate.Path))
		}
	}
	return result
}

// RetiredPaths is the deterministic set whose durable target is unowned.
func (s Snapshot) RetiredPaths() []paths.RelPath {
	result := make([]paths.RelPath, 0, len(s.candidates))
	for _, candidate := range s.candidates {
		if Target(candidate.Owner) == notediscovery.Unowned {
			result = append(result, candidate.Path)
		}
	}
	return result
}

func (s Snapshot) filter(keep func(Candidate) bool) []Candidate {
	result := make([]Candidate, 0, len(s.candidates))
	for _, candidate := range s.candidates {
		if keep(candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

// Target maps ignored paths to their durable unowned representation.
func Target(owner notediscovery.Owner) notediscovery.Owner {
	if owner == notediscovery.Ignored || owner == notediscovery.Unowned {
		return notediscovery.Unowned
	}
	return owner
}

// DiscoveryInput injects caller-owned code eligibility. CodeLanguage returns
// an empty language for unsupported files and runs only inside code roots.
type DiscoveryInput struct {
	VaultDefinition    obsidian.VaultDefinition
	Registry           noteformat.Registry
	CodeRoots          []paths.AbsPath
	CodeLanguage       func(paths.CodePathRef) codeanchor.Lang
	PersistedNotePaths []paths.NotePath
	PersistedCodePaths []paths.CodePath
}

// Discover makes exactly one vault walk. notediscovery remains a pure plan:
// filesystem state and persisted ownership are composed here, not in it.
func Discover(ctx context.Context, input DiscoveryInput) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	vaultPaths, err := paths.NewVaultPaths(input.VaultDefinition.BasePath())
	if err != nil || vaultPaths.Root() == "" {
		return Snapshot{}, fmt.Errorf("ownership discovery vault base path is required")
	}
	selector, err := CompileSelector(SelectorInput{
		VaultDefinition: input.VaultDefinition,
		Registry:        input.Registry,
		CodeRoots:       input.CodeRoots,
		CodeLanguage:    input.CodeLanguage,
	})
	if err != nil {
		return Snapshot{}, err
	}
	previous, err := persistedOwners(input.PersistedNotePaths, input.PersistedCodePaths)
	if err != nil {
		return Snapshot{}, err
	}
	candidates := make(map[string]Candidate)
	walkStarted := time.Now()
	err = filepath.WalkDir(vaultPaths.Root(), discoveryWalk(ctx, selector, candidates))
	indexingperf.ObserveLatency(ctx, "fs.walk", time.Since(walkStarted))
	if err != nil {
		return Snapshot{}, err
	}
	for path, owner := range previous {
		candidate, found := candidates[path]
		if !found {
			candidate = Candidate{Path: paths.RelPath(path), Owner: notediscovery.Unowned}
		}
		candidate.PreviousOwner = owner
		candidates[path] = candidate
	}
	for path, candidate := range candidates {
		if _, found := previous[path]; !found {
			candidate.PreviousOwner = notediscovery.Unowned
			candidates[path] = candidate
		}
	}
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate)
	}
	return newSnapshot(result)
}

// DiscoverScoped observes only the requested files and directory subtrees.
// Persisted owners in the requested scope remain candidates even when their
// current filesystem path is absent, so deletion and ownership hand-off
// transitions stay explicit.
func DiscoverScoped(ctx context.Context, input DiscoveryInput, requested []paths.RelPath) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	vaultPaths, err := paths.NewVaultPaths(input.VaultDefinition.BasePath())
	if err != nil || vaultPaths.Root() == "" {
		return Snapshot{}, fmt.Errorf("ownership discovery vault base path is required")
	}
	selector, err := CompileSelector(SelectorInput{
		VaultDefinition: input.VaultDefinition,
		Registry:        input.Registry,
		CodeRoots:       input.CodeRoots,
		CodeLanguage:    input.CodeLanguage,
	})
	if err != nil {
		return Snapshot{}, err
	}
	scopes, err := canonicalDiscoveryScopes(requested)
	if err != nil {
		return Snapshot{}, err
	}
	previous, err := persistedOwners(input.PersistedNotePaths, input.PersistedCodePaths)
	if err != nil {
		return Snapshot{}, err
	}
	candidates := make(map[string]Candidate)
	walkStarted := time.Now()
	for _, scope := range scopes {
		abs, err := vaultPaths.Abs(scope)
		if err != nil {
			return Snapshot{}, err
		}
		statStarted := time.Now()
		info, err := os.Stat(abs.String())
		indexingperf.ObserveLatency(ctx, "fs.stat", time.Since(statStarted))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Snapshot{}, err
		}
		if info.IsDir() {
			if err := filepath.WalkDir(abs.String(), discoveryWalk(ctx, selector, candidates)); err != nil {
				return Snapshot{}, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := discoverFile(ctx, selector, abs.String(), fs.FileInfoToDirEntry(info), candidates); err != nil {
			return Snapshot{}, err
		}
	}
	indexingperf.ObserveLatency(ctx, "fs.walk", time.Since(walkStarted))
	for path, owner := range previous {
		if !inDiscoveryScope(path, scopes) {
			continue
		}
		candidate, found := candidates[path]
		if !found {
			candidate = Candidate{Path: paths.RelPath(path), Owner: notediscovery.Unowned}
		}
		candidate.PreviousOwner = owner
		candidates[path] = candidate
	}
	for path, candidate := range candidates {
		if _, found := previous[path]; !found {
			candidate.PreviousOwner = notediscovery.Unowned
			candidates[path] = candidate
		}
	}
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate)
	}
	return newSnapshot(result)
}

func canonicalDiscoveryScopes(raw []paths.RelPath) ([]paths.RelPath, error) {
	seen := make(map[string]struct{}, len(raw))
	result := make([]paths.RelPath, 0, len(raw))
	for _, path := range raw {
		clean, err := paths.CleanRelPath(path.String())
		if err != nil || clean == "" || clean != path {
			return nil, fmt.Errorf("ownership discovery scope must be canonical and vault-relative: %q", path)
		}
		if _, exists := seen[clean.String()]; !exists {
			seen[clean.String()] = struct{}{}
			result = append(result, clean)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result, nil
}

func inDiscoveryScope(path string, scopes []paths.RelPath) bool {
	for _, scope := range scopes {
		if path == scope.String() || strings.HasPrefix(path, scope.String()+"/") {
			return true
		}
	}
	return false
}

func discoveryWalk(ctx context.Context, selector Selector, candidates map[string]Candidate) fs.WalkDirFunc {
	return func(abs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			skip, err := selector.skipDirectory(abs, entry.Name())
			if err != nil {
				return err
			}
			if skip {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") || entry.Type()&fs.ModeType != 0 {
			return nil
		}
		return discoverFile(ctx, selector, abs, entry, candidates)
	}
}

func discoverFile(ctx context.Context, selector Selector, abs string, entry fs.DirEntry, candidates map[string]Candidate) error {
	rel, err := selector.vaultPaths.RelStrict(abs)
	if err != nil {
		return err
	}
	selection, err := selector.Select(rel)
	if err != nil {
		return err
	}
	if selection.Owner == notediscovery.Ignored {
		candidates[selection.Path.String()] = Candidate{Path: selection.Path, Owner: notediscovery.Ignored, Present: true}
		return nil
	}
	statStarted := time.Now()
	info, err := entry.Info()
	indexingperf.ObserveLatency(ctx, "fs.stat", time.Since(statStarted))
	if err != nil || !info.Mode().IsRegular() {
		return err
	}
	absPath, err := selector.vaultPaths.Abs(rel)
	if err != nil || absPath == "" {
		return fmt.Errorf("resolve ownership candidate %q: %w", rel, err)
	}
	language := selection.Language
	if selection.Owner != notediscovery.Code {
		language = ""
	}
	candidates[selection.Path.String()] = Candidate{Path: selection.Path, Owner: selection.Owner, Provider: selection.Provider, Language: language, Present: true, AbsPath: absPath, ModTime: info.ModTime().Unix(), Size: info.Size()}
	return nil
}

func persistedOwners(notes []paths.NotePath, code []paths.CodePath) (map[string]notediscovery.Owner, error) {
	result := make(map[string]notediscovery.Owner, len(notes)+len(code))
	for _, path := range notes {
		clean, err := paths.CleanRelPath(path.String())
		if err != nil || clean == "" {
			return nil, fmt.Errorf("persisted note ownership path %q is invalid", path)
		}
		result[clean.String()] = notediscovery.Note
	}
	for _, path := range code {
		clean, err := paths.CleanRelPath(path.String())
		if err != nil || clean == "" {
			return nil, fmt.Errorf("persisted code ownership path %q is invalid", path)
		}
		if prior, found := result[clean.String()]; found && prior != notediscovery.Code {
			return nil, fmt.Errorf("persisted ownership path %q has conflicting note and code owners", clean)
		}
		result[clean.String()] = notediscovery.Code
	}
	return result, nil
}
