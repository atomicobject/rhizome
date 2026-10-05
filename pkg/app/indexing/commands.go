package indexing

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// CodeIndexCommandOptions configures code-intel table ingestion without
// semantic embedding.
type CodeIndexCommandOptions struct {
	VaultPath  string
	VaultDef   obsidian.VaultDefinition
	CodeConfig codeanchor.Config
	ErrWriter  io.Writer
}

// CodeAnchorsCommandOptions configures code-anchor note matching/indexing.
type CodeAnchorsCommandOptions struct {
	VaultPath  string
	VaultDef   obsidian.VaultDefinition
	CodeConfig codeanchor.Config
	ErrWriter  io.Writer
}

type acquireCommandIndexLock func(context.Context, string, bool, bool, io.Writer) (func() error, error)

func withCommandIndexLock(ctx context.Context, vaultPath string, out io.Writer, run func() error) error {
	return withCommandIndexLockUsing(ctx, vaultPath, out, TryAcquireIndexLock, run)
}

func withCommandIndexLockUsing(ctx context.Context, vaultPath string, out io.Writer, acquire acquireCommandIndexLock, run func() error) error {
	release, err := acquire(ctx, obsidian.IndexLockPath(vaultPath), true, false, out)
	if err != nil {
		return err
	}
	defer func() {
		if err := release(); err != nil {
			fmt.Fprintf(out, "Warning: failed to release index lock: %v\n", err)
		}
	}()
	return run()
}

// RunCodeIndexCommand indexes code without semantic embedding.
func RunCodeIndexCommand(ctx context.Context, opts CodeIndexCommandOptions) error {
	out := opts.ErrWriter
	if out == nil {
		out = os.Stderr
	}
	return withCommandIndexLock(ctx, opts.VaultPath, out, func() error {
		return runCodeIndexCommandUnlocked(ctx, opts, out)
	})
}

func runCodeIndexCommandUnlocked(ctx context.Context, opts CodeIndexCommandOptions, out io.Writer) error {
	codeCfg := opts.CodeConfig
	store, cleanup, err := obsidian.OpenIntelStoreForWriteFromConfig(opts.VaultPath, codeCfg)
	if err != nil {
		return err
	}
	defer cleanup()

	tailIdx := codeanchor.NewPathTailIndex(5)
	codeintel.PopulateTailIndexFromRoots(tailIdx, bootstrap.ResolveRoots(opts.VaultPath, codeCfg.TSRoots))

	service, _ := bootstrap.NewCodeAnchorService(bootstrap.CodeAnchorServiceConfig{
		VaultPath:       opts.VaultPath,
		CodeCfg:         codeCfg,
		Store:           store,
		TailIndex:       tailIdx,
		Linker:          codeintel.NewDocLinkerWithStore(opts.VaultPath, store),
		IncludeIndexers: true,
		WriteAccess:     true,
	})

	storedVersion, hasVersion, _ := store.IndexerVersion(ctx)
	if hasVersion && storedVersion != codeanchor.IndexerVersion {
		fmt.Fprintf(out, "Note: Indexer version changed (%s -> %s); consider running a full reindex\n",
			storedVersion, codeanchor.IndexerVersion)
	}

	if !service.HasAnyIndexer() {
		fmt.Fprintln(out, "Warning: no code indexers available (build may lack cgo/tree-sitter); code files will be skipped.")
	}
	if skipped := codeanchor.UnsupportedExtensions(service); len(skipped) > 0 {
		fmt.Fprintf(out, "Skipping unsupported code extensions (no indexer): %s\n", strings.Join(skipped, ", "))
	}

	matcher := ignore.LoadUnifiedMatcher(opts.VaultPath, opts.VaultDef.Excludes)
	noteResult, err := codeintel.IngestNotesForVault(ctx, service, opts.VaultDef, matcher, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Ingested %d notes\n", noteResult.Count)
	printNoteBuildWarnings(progressSinkFunc(func(line string) { fmt.Fprintln(out, line) }), noteResult.BuildErrors)

	ignoreMatcher := ignore.LoadUnifiedMatcher(opts.VaultPath, opts.VaultDef.Excludes)
	total := codeintel.IndexResult{
		Skipped:     make(map[codeanchor.Lang]int),
		Unsupported: make(map[codeanchor.Lang]int),
	}
	seenPathsSet := make(map[string]struct{})
	changedCallerPathsSet := make(map[string]struct{})
	var defDeltasAcc codeanchor.DefDeltaAccumulator
	for _, root := range codeCfg.CodeRoots() {
		absRoot, err := codeintel.ResolveRoot(opts.VaultPath, root)
		if err != nil {
			return err
		}
		if absRoot == "" {
			continue
		}
		res, err := codeintel.IndexRoot(ctx, service, opts.VaultPath, absRoot, ignoreMatcher, nil)
		if err != nil {
			return err
		}
		total.Indexed += res.Indexed
		total.Unchanged += res.Unchanged
		for _, path := range res.SeenPaths {
			if path != "" {
				seenPathsSet[path] = struct{}{}
			}
		}
		for _, path := range res.ChangedCallerPaths {
			if path != "" {
				changedCallerPathsSet[path] = struct{}{}
			}
		}
		defDeltasAcc.Add(res.DefDeltas)
		codeintel.MergeLangCounts(total.Skipped, res.Skipped)
		codeintel.MergeLangCounts(total.Unsupported, res.Unsupported)
	}
	defDeltas := defDeltasAcc.Finalize()

	changedCallerPaths := make([]string, 0, len(changedCallerPathsSet))
	for path := range changedCallerPathsSet {
		changedCallerPaths = append(changedCallerPaths, path)
	}
	if len(changedCallerPaths) > 0 || defDeltas.HasChanges() {
		sort.Strings(changedCallerPaths)
		defSymbolCount := len(defDeltas.AddedSymbols) + len(defDeltas.RemovedSymbols)
		defModuleCount := len(defDeltas.AddedModules) + len(defDeltas.RemovedModules)
		fallbackReason := ""
		if defDeltas.HasChanges() {
			if ready, ok, err := store.ReverseIndexBackfillComplete(ctx); err == nil {
				if !ok || !ready {
					fallbackReason = "reverse_index_backfill_incomplete"
				}
			} else {
				fallbackReason = "reverse_index_backfill_unknown"
			}
		}

		impactedCallers := 0
		rebuildCount := 0
		if fallbackReason != "" {
			seenPaths := make([]string, 0, len(seenPathsSet))
			for path := range seenPathsSet {
				seenPaths = append(seenPaths, path)
			}
			sort.Strings(seenPaths)
			if err := service.RebuildCallEdgesForPaths(ctx, seenPaths); err != nil {
				fmt.Fprintf(out, "Warning: failed to rebuild call edges: %v\n", err)
			}
			rebuildCount = len(seenPaths)
		} else {
			summary, err := service.RebuildCallEdgesForDefDeltas(ctx, changedCallerPaths, defDeltas)
			if err != nil {
				fmt.Fprintf(out, "Warning: failed to rebuild call edges: %v\n", err)
			}
			impactedCallers = summary.ImpactedCallers
			rebuildCount = summary.RebuildPaths
		}

		logLine := fmt.Sprintf("Call-edge rebuild: changed_callers=%d def_symbols=%d def_modules=%d impacted_callers=%d rebuild_paths=%d",
			len(changedCallerPaths),
			defSymbolCount,
			defModuleCount,
			impactedCallers,
			rebuildCount,
		)
		if fallbackReason != "" {
			logLine += fmt.Sprintf(" fallback=%s", fallbackReason)
		}
		fmt.Fprintln(out, logLine)
	}

	if total.Unchanged > 0 {
		fmt.Fprintf(out, "Indexed %d code files (%d unchanged)\n", total.Indexed, total.Unchanged)
	} else {
		fmt.Fprintf(out, "Indexed %d code files\n", total.Indexed)
	}
	if len(total.Skipped) > 0 {
		fmt.Fprintf(out, "Skipped unsupported code languages (no indexer): %s\n", codeintel.FormatLangCounts(total.Skipped))
	}
	if len(total.Unsupported) > 0 {
		fmt.Fprintf(out, "Indexer unavailable for some files (unsupported): %s\n", codeintel.FormatLangCounts(total.Unsupported))
	}

	if total.Indexed > 0 {
		if err := service.RecomputeAnchorScopes(ctx); err != nil {
			return err
		}
	}

	if err := store.SetIndexerVersion(ctx, codeanchor.IndexerVersion); err != nil {
		fmt.Fprintf(out, "Warning: failed to store indexer version: %v\n", err)
	}

	codeCfg.Enabled = true
	if err := obsidian.SaveCodeConfig(opts.VaultPath, codeCfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "Code index updated at %s (indexer %s)\n", codeCfg.IndexPath, codeanchor.IndexerVersion)
	return nil
}

// RunCodeAnchorsCommand refreshes note-defined code-anchor matches.
func RunCodeAnchorsCommand(ctx context.Context, opts CodeAnchorsCommandOptions) error {
	out := opts.ErrWriter
	if out == nil {
		out = os.Stderr
	}
	return withCommandIndexLock(ctx, opts.VaultPath, out, func() error {
		return runCodeAnchorsCommandUnlocked(ctx, opts, out)
	})
}

func runCodeAnchorsCommandUnlocked(ctx context.Context, opts CodeAnchorsCommandOptions, out io.Writer) error {
	codeCfg := opts.CodeConfig
	store, cleanup, err := obsidian.OpenIntelStoreForWriteFromConfig(opts.VaultPath, codeCfg)
	if err != nil {
		return err
	}
	defer cleanup()

	tailIdx := codeanchor.NewPathTailIndex(5)
	codeintel.PopulateTailIndexFromRoots(tailIdx, bootstrap.ResolveRoots(opts.VaultPath, codeCfg.TSRoots))

	service, _ := bootstrap.NewCodeAnchorService(bootstrap.CodeAnchorServiceConfig{
		VaultPath:       opts.VaultPath,
		CodeCfg:         codeCfg,
		Store:           store,
		TailIndex:       tailIdx,
		Linker:          codeintel.NewDocLinker(opts.VaultPath),
		IncludeIndexers: false,
		WriteAccess:     true,
	})

	matcher := ignore.LoadUnifiedMatcher(opts.VaultPath, opts.VaultDef.Excludes)
	noteResult, err := codeintel.IngestNotesForVault(ctx, service, opts.VaultDef, matcher, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Ingested %d notes\n", noteResult.Count)
	printNoteBuildWarnings(progressSinkFunc(func(line string) { fmt.Fprintln(out, line) }), noteResult.BuildErrors)

	if err := service.RecomputeAnchorScopes(ctx); err != nil {
		return err
	}
	if err := store.SetIndexerVersion(ctx, codeanchor.IndexerVersion); err != nil {
		fmt.Fprintf(out, "Warning: failed to store indexer version: %v\n", err)
	}

	codeCfg.Enabled = true
	if err := obsidian.SaveCodeConfig(opts.VaultPath, codeCfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "Code anchors refreshed at %s (indexer %s)\n", codeCfg.IndexPath, codeanchor.IndexerVersion)
	return nil
}
