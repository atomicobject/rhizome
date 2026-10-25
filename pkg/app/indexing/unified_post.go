package indexing

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	graphindex "github.com/atomicobject/rhizome/pkg/graph/index"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type unifiedPostIndexOptions struct {
	ForceGraph       bool
	VaultPath        string
	Vacuum           bool
	CurrentScopeHash string
	CodeEmbExplicit  bool
	// SkipConfigPersistence mirrors UnifiedOptions.SkipConfigPersistence.
	SkipConfigPersistence bool
}

func runUnifiedPostIndex(
	ctx context.Context,
	opts unifiedPostIndexOptions,
	progress *integratedProgress,
	intelStore *semdb.Store,
	writeQueue *indexwriter.Writer,
	semanticResources *unifiedSemanticResources,
	codeCfg codeanchor.Config,
	embCfg embeddings.Config,
	codeEmbCfg embeddings.Config,
	total codeintel.IndexResult,
	noteResult codeintel.NoteIngestResult,
) error {
	if semanticResources != nil {
		semanticResources.close()
	}

	// Graph scores (HITS + label propagation) derived from intel tables.
	// Only rebuild when notes actually changed (including deletes) to avoid expensive recomputation on no-op runs.
	noteChanged := len(noteResult.ChangedPaths) > 0 || noteResult.Deleted > 0
	progress.AdvanceTo("Computing graph", ontologyEnd)
	if noteChanged || total.Indexed > 0 || opts.ForceGraph {
		if err := runPhase(ctx, "compute_graph", func(phaseCtx context.Context) error {
			progress.SetStage("Computing doc scores")
			scores, err := graphindex.ComputeDerivedScores(phaseCtx, intelStore, graphindex.DocScoresOptions{WikilinkOptions: markdownsyntax.DefaultWikilinkOptions, VaultRoot: opts.VaultPath})
			if err != nil {
				return fmt.Errorf("graph scores: %w", err)
			}
			if err := writeQueue.SubmitGraphScores(phaseCtx, scores); err != nil {
				return fmt.Errorf("publish graph scores: %w", err)
			}

			return nil
		}); err != nil {
			return err
		}
	}
	progress.AdvanceTo("Computing graph", graphEnd)

	// Persist configs and gitignore entries.
	progress.AdvanceTo("Saving index state", graphEnd)
	if !opts.SkipConfigPersistence {
		if codeEmbCfg.Enabled {
			if err := persistCodeEmbeddingsConfig(opts.VaultPath, codeEmbCfg, opts.CodeEmbExplicit); err != nil {
				return err
			}
		}
		if err := repoconfig.SaveCodeConfig(opts.VaultPath, codeCfg); err != nil {
			return err
		}
		if err := repoconfig.SaveEmbeddingsConfig(opts.VaultPath, embCfg); err != nil {
			return err
		}
	}
	progress.AdvanceTo("Saving index state", graphEnd)

	return writeQueue.SubmitMaintenance(ctx, func(ctx context.Context) error {
		// Docs:
		// - [[indexing-observability-maintenance-policy#^spec-0047-us3]]
		// - [[indexing-workflow#^spec-0036-us1-ac1]]
		var autoVac bool
		if !opts.Vacuum {
			var err error
			autoVac, err = shouldAutoVacuum(ctx, intelStore)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: auto VACUUM check failed: %v\n", err)
			}
		}
		if opts.Vacuum || autoVac {
			progress.AdvanceTo("Vacuuming index", graphEnd)
			if err := runPhase(indexingperf.WithPhase(ctx, "vacuum"), "vacuum", func(phaseCtx context.Context) error {
				return intelStore.Vacuum(phaseCtx)
			}); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: VACUUM failed: %v\n", err)
			} else if autoVac {
				_ = recordAutoVacuum(codeCfg.IndexPath)
			}
		}
		progress.AdvanceTo("Optimizing index", graphEnd)
		if err := runPhase(ctx, "analyze_optimize_checkpoint", func(phaseCtx context.Context) error {
			if err := intelStore.Analyze(phaseCtx); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: ANALYZE failed: %v\n", err)
			}
			if err := intelStore.Optimize(phaseCtx); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: PRAGMA optimize failed: %v\n", err)
			}
			if err := intelStore.CheckIntegrity(phaseCtx); err != nil {
				reportIntegrityFailure(phaseCtx, err)
			}
			if err := intelStore.Checkpoint(phaseCtx, true); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: WAL checkpoint failed: %v\n", err)
			}
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: post-index maintenance failed: %v\n", err)
		}

		// Store scope config hash after successful indexing.
		if opts.CurrentScopeHash != "" {
			if err := intelStore.SetScopeConfigHash(ctx, opts.CurrentScopeHash); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to store scope config hash: %v\n", err)
			}
		}

		return ctx.Err()
	})
}

// integrityTablePattern finds the table SQLite names in a check result, such as
// "malformed inverted index for FTS5 table main.intel_fts" or
// `corruption found reading blob ... from table "intel_fts"`.
var integrityTablePattern = regexp.MustCompile(`\btable "?(?:[A-Za-z_]\w*\.)?([A-Za-z_]\w*)`)

// reportIntegrityFailure retains the failure as a WARN event on the index
// operation, which the recorder also mirrors to stderr, so `rzm diagnostics`
// shows it after the console output is gone.
func reportIntegrityFailure(ctx context.Context, err error) {
	reason := "check_failed"
	if sqliteutil.IsCorruptError(err) {
		reason = "corrupt"
	}
	recorder := diagnostics.FromContext(ctx)
	if recorder == nil {
		fmt.Fprintf(os.Stderr, "Warning: integrity check failed: %v\n", err)
		return
	}
	attrs := []slog.Attr{slog.String("reason_code", reason)}
	if match := integrityTablePattern.FindStringSubmatch(err.Error()); match != nil {
		attrs = append(attrs, slog.String("table", match[1]))
	}
	recorder.Event(ctx, slog.LevelWarn, "indexing", "integrity.failed", err.Error(), attrs...)
}
