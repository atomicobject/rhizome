package indexcore

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	sqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// Discover captures pre-transition metadata and current ownership under the
// caller's lease. Publish must use the same request, store, and held lease.
func Discover(ctx context.Context, request Request, store *sqlite.Store) (result Discovery, resultErr error) {
	ctx = indexingperf.WithPhase(ctx, "ownership_discovery")
	done := indexingperf.StartSpan(ctx, "ownership_discovery")
	defer func() { done(resultErr) }()
	if err := request.NoteMetadata.Validate(); err != nil {
		return Discovery{}, err
	}
	if store == nil {
		return Discovery{}, fmt.Errorf("structural store is required")
	}
	d := Discovery{FullDiscovery: request.Paths == nil, affected: map[string]struct{}{}}
	if request.Paths != nil && len(request.Paths) == 0 {
		d.noop = true
		return d, nil
	}
	runtime, err := request.NoteMetadata.FormatRuntime()
	if err != nil {
		return d, err
	}
	vp, err := paths.NewVaultPaths(request.VaultDefinition.BasePath())
	if err != nil || vp.Root() == "" {
		return d, fmt.Errorf("structural vault root is required")
	}
	d.CodeRoots, err = ResolveCodeRoots(vp.Root(), CodeRoots(request.CodeConfig))
	if err != nil {
		return d, err
	}
	rawNotes, err := store.NotePaths(ctx)
	if err != nil {
		return d, err
	}
	d.persistedNotes, err = cleanNotePaths(rawNotes)
	if err != nil {
		return d, err
	}
	rawCode, err := store.IndexedFilePaths(ctx)
	if err != nil {
		return d, err
	}
	codePaths, err := cleanCodePaths(rawCode)
	if err != nil {
		return d, err
	}
	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	if err != nil {
		return d, err
	}
	unready, err := store.HasUnreadyDerivedWork(ctx)
	if err != nil {
		return d, err
	}
	d.GraphRecovery, err = store.HasDerivedWork(ctx, anchors.DerivedGraph)
	if err != nil {
		return d, err
	}
	d.CodeRecovery, err = store.HasDerivedWork(ctx, anchors.DerivedCode)
	if err != nil {
		return d, err
	}
	d.NotesRecovery, err = store.HasDerivedWork(ctx, anchors.DerivedNotes)
	if err != nil {
		return d, err
	}
	d.OntologyRecovery, err = store.HasDerivedWork(ctx, anchors.DerivedOntology)
	if err != nil {
		return d, err
	}
	d.Reconciliation, err = noteownership.NewReconciliationTracker(generation, pending)
	if err != nil {
		return d, err
	}
	d.Recovery = pending || unready
	if pending {
		indexingperf.AddCount(ctx, "ownership.pending_reconciliation", 1)
		event(ctx, "reconciliation.pending", "unfinished_structural_work", slog.Int64("generation", generation))
	}
	if unready {
		indexingperf.AddCount(ctx, "derived.recovery.inactive", 1)
	}
	for kind, recovery := range map[string]bool{"graph": d.GraphRecovery, "code": d.CodeRecovery, "notes": d.NotesRecovery, "ontology": d.OntologyRecovery} {
		if recovery {
			indexingperf.AddCount(ctx, "derived.recovery."+kind, 1)
		}
	}
	d.FullDiscovery = d.FullDiscovery || d.Recovery || request.ForceMetadata
	d.input = noteownership.DiscoveryInput{VaultDefinition: request.VaultDefinition, Registry: runtime.Registry(), CodeRoots: d.CodeRoots, CodeLanguage: func(ref paths.CodePathRef) anchors.Lang { return codeintel.DetectCodeLang(ref.Abs.String()) }, PersistedNotePaths: d.persistedNotes, PersistedCodePaths: codePaths}
	for _, p := range request.Paths {
		clean, err := paths.CleanRelPath(p.String())
		if err != nil || clean == "" || clean != p {
			return d, fmt.Errorf("structural path is not canonical: %q", p)
		}
		d.affected[p.String()] = struct{}{}
	}
	d.Snapshot, err = d.rediscover(ctx)
	if err != nil {
		return d, err
	}
	indexingperf.AddCount(ctx, "ownership.candidates", int64(len(d.Snapshot.Candidates())))
	if d.FullDiscovery {
		indexingperf.AddCount(ctx, "ownership.discovery.complete", 1)
	} else {
		indexingperf.AddCount(ctx, "ownership.discovery.scoped", 1)
	}
	for _, c := range d.Snapshot.OwnershipChangeCandidates() {
		d.affected[c.Path.String()] = struct{}{}
	}
	if pending || unready {
		for _, c := range d.Snapshot.Candidates() {
			if c.Present {
				d.affected[c.Path.String()] = struct{}{}
			}
		}
	}
	d.baseline, err = request.NoteMetadata.CapturePublishedBaseline(ctx, request.VaultDefinition, store)
	return d, err
}

func (d Discovery) rediscover(ctx context.Context) (noteownership.Snapshot, error) {
	if d.FullDiscovery {
		return noteownership.Discover(ctx, d.input)
	}
	raw := sortedSet(d.affected)
	scopes := make([]paths.RelPath, len(raw))
	for i, p := range raw {
		scopes[i] = paths.RelPath(p)
	}
	return noteownership.DiscoverScoped(ctx, d.input, scopes)
}
func CodeRoots(c anchors.Config) []string {
	result := append([]string{}, c.PythonRoots...)
	result = append(result, c.GoRoots...)
	result = append(result, c.TSRoots...)
	result = append(result, c.CSharpRoots...)
	return append(result, c.PHPRoots...)
}
func ResolveCodeRoots(root string, configured []string) ([]paths.AbsPath, error) {
	seen := map[string]struct{}{}
	var result []paths.AbsPath
	for _, raw := range configured {
		if filepath.IsAbs(raw) {
			raw = paths.ResolveSymlinks(raw).String()
			if paths.NormalizeAbsPathForCompare(raw) == paths.NormalizeAbsPathForCompare(root) {
				raw = "."
			}
		}
		resolved, err := codeintel.ResolveRoot(root, raw)
		if err != nil {
			return nil, err
		}
		if resolved == "" {
			continue
		}
		resolved = paths.ResolveSymlinks(resolved).String()
		key := paths.NormalizeAbsPathForCompare(resolved)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, paths.AbsPath(resolved))
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}
func cleanNotePaths(raw []string) ([]paths.NotePath, error) {
	result := make([]paths.NotePath, len(raw))
	for i, p := range raw {
		clean, err := paths.CleanNotePath(p)
		if err != nil || clean.String() != p {
			return nil, fmt.Errorf("persisted note path is not canonical: %q", p)
		}
		result[i] = clean
	}
	return result, nil
}
func cleanCodePaths(raw []string) ([]paths.CodePath, error) {
	result := make([]paths.CodePath, len(raw))
	for i, p := range raw {
		clean, err := paths.CleanRelPath(p)
		if err != nil || clean == "" || clean.String() != p {
			return nil, fmt.Errorf("persisted code path is not canonical: %q", p)
		}
		result[i] = paths.CodePath(clean)
	}
	return result, nil
}
func sortedSet(set map[string]struct{}) []string {
	result := make([]string, 0, len(set))
	for p := range set {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}
func mergePaths(groups ...[]string) []string {
	set := map[string]struct{}{}
	for _, group := range groups {
		for _, p := range group {
			if p != "" {
				set[p] = struct{}{}
			}
		}
	}
	return sortedSet(set)
}
