package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func publishDerivedTestSave(t *testing.T, w *unifiedSemanticWatcher, schema *ontoschema.Schema, path string) {
	t.Helper()
	save, _, err := w.lane.Submit(t.Context(), lane.Request{Kind: lane.KindValidationRefresh, Run: func(ctx context.Context, _ *indexlock.Lease, _ lane.Reporter) error {
		baseline, err := w.noteMetadataIndexer.CapturePublishedBaseline(ctx, w.vaultDef, w.intelStore)
		if err != nil {
			return err
		}
		bytes, err := os.ReadFile(filepath.Join(w.vaultPath, path))
		if err != nil {
			return err
		}
		stat, err := os.Stat(filepath.Join(w.vaultPath, path))
		if err != nil {
			return err
		}
		format, ok := w.noteRuntime.Provider("markdown")
		if !ok {
			return fmt.Errorf("markdown provider unavailable")
		}
		source, err := noteformat.NewAuthoredSource(paths.NotePath(path), format.Descriptor(), bytes, stat.ModTime().Unix())
		if err != nil {
			return err
		}
		prep, err := w.noteMetadataIndexer.PreparePublishedMetadata(ctx, []paths.NotePath{paths.NotePath(path)}, map[paths.NotePath]noteformat.AuthoredSource{paths.NotePath(path): source})
		if err != nil {
			return err
		}
		delta, full, err := w.noteMetadataIndexer.BuildPublishedPathMetadataDeltaFromPreparation(ctx, w.vaultDef, w.intelStore, baseline, prep, nil)
		if full {
			return fmt.Errorf("direct-save fixture unexpectedly needs full metadata")
		}
		if err != nil {
			return err
		}
		if delta != nil {
			if err := w.intelStore.ApplyNoteMetadataDelta(ctx, *delta); err != nil {
				return err
			}
		}
		queue := w.structuralWriter(ctx)
		_, err = ontology.SyncPublishedPaths(ctx, w.noteMetadataIndexer, w.vaultDef, &obsidian.Note{}, w.intelStore, queue, []string{path}, nil)
		if err != nil {
			return errors.Join(err, queue.Close())
		}
		return errors.Join(queue.FlushAndWait(ctx), queue.Close())
	}})
	require.NoError(t, err)
	watcherJobTerminal(t, save)
	require.NoError(t, save.Err())
}
