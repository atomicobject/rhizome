package validationproduct

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func noteMetadataDeltaChangedPaths(delta semdb.NoteMetadataDelta) []string {
	paths := make([]string, 0, len(delta.Notes))
	for _, row := range delta.Notes {
		if path := strings.TrimSpace(row.Path); path != "" {
			paths = append(paths, path)
		}
	}
	return dedupeSortedStrings(paths)
}

func unionOntologyPaths(left, right []string) []string {
	return dedupeSortedStrings(append(append([]string(nil), left...), right...))
}

func dedupeSortedStrings(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, value := range input {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeValidationProjectionRequest(request ValidationProjectionRequest) ValidationProjectionRequest {
	if request.VaultPath == "" {
		request.VaultPath = request.VaultDef.BasePath()
	}
	if request.VaultDef.BasePath() == "" {
		request.VaultDef.Path = request.VaultPath
	}
	if request.Target == "" {
		request.Target = ValidationProjectionLive
	}
	return request
}

func validateValidationProjectionRequest(request ValidationProjectionRequest) error {
	if err := request.NoteMetadata.Validate(); err != nil {
		return fmt.Errorf("note metadata indexer: %w", err)
	}
	if strings.TrimSpace(request.VaultPath) == "" {
		return fmt.Errorf("vault path is required")
	}
	vaultPaths, err := paths.NewVaultPaths(request.VaultPath)
	if err != nil {
		return fmt.Errorf("resolve vault path: %w", err)
	}
	definitionPaths, err := paths.NewVaultPaths(request.VaultDef.BasePath())
	if err != nil {
		return fmt.Errorf("resolve vault definition path: %w", err)
	}
	if vaultPaths.Root() == "" || definitionPaths.Root() == "" {
		return fmt.Errorf("vault path is required")
	}
	if !paths.CaseEqual(vaultPaths.Root(), definitionPaths.Root()) {
		return fmt.Errorf("vault path %q does not match vault definition root %q", vaultPaths.Root(), definitionPaths.Root())
	}
	return nil
}

func openValidationProjectionStore(request ValidationProjectionRequest) (*semdb.Store, string, func() error, error) {
	if request.Target == ValidationProjectionScratch {
		dir, err := os.MkdirTemp("", "rzm-validation-")
		if err != nil {
			return nil, "", nil, err
		}
		indexPath := filepath.Join(dir, repoconfig.RhizomeDBFile)
		store, err := semdb.Open(indexPath)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, "", nil, err
		}
		return store, indexPath, func() error {
			closeErr := store.Close()
			removeErr := os.RemoveAll(dir)
			return errors.Join(closeErr, removeErr)
		}, nil
	}
	if request.Target != ValidationProjectionLive {
		return nil, "", nil, fmt.Errorf("unknown validation projection target %q", request.Target)
	}
	resolvedConfig1, configErr1 := repoconfig.Load(request.VaultPath)
	config, err := resolvedConfig1.Code, configErr1
	if err != nil {
		return nil, "", nil, err
	}
	indexPath := repoconfig.UnifiedIndexPath(request.VaultPath, config.IndexPath)
	store, err := semdb.Open(indexPath)
	if err != nil {
		return nil, "", nil, err
	}
	return store, indexPath, store.Close, nil
}

func validationProjectionPaths(ctx context.Context, request ValidationProjectionRequest, reader obsidian.NoteReader, store notemeta.Store, counters *ValidationProjectionCounters, timings map[string]time.Duration) ([]string, []string, error) {
	if request.ExactPaths != nil {
		vaultPaths, err := paths.NewVaultPaths(request.VaultPath)
		if err != nil {
			return nil, nil, err
		}
		changed, err := normalizedNotePathStrings(vaultPaths, request.ExactPaths.Changed)
		if err != nil {
			return nil, nil, fmt.Errorf("normalize changed validation paths: %w", err)
		}
		deleted, err := normalizedNotePathStrings(vaultPaths, request.ExactPaths.Deleted)
		if err != nil {
			return nil, nil, fmt.Errorf("normalize deleted validation paths: %w", err)
		}
		counters.ExactPathsRead = len(changed) + len(deleted)
		return changed, deleted, nil
	}
	started := time.Now()
	dirty, err := request.NoteMetadata.DiscoverDirtyPaths(ctx, request.VaultDef, reader, store)
	timings["discover_notes"] = time.Since(started)
	if err != nil {
		return nil, nil, err
	}
	counters.NoteScans = 1
	return dirty.Changed, dirty.Deleted, nil
}

func normalizedNotePathStrings(vaultPaths paths.VaultPaths, input []paths.NotePath) ([]string, error) {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, raw := range input {
		if strings.TrimSpace(string(raw)) == "" {
			continue
		}
		path, err := vaultPaths.RelNotePathStrict(string(raw))
		if err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		pathString := path.String()
		if _, ok := seen[pathString]; ok {
			continue
		}
		seen[pathString] = struct{}{}
		out = append(out, pathString)
	}
	sort.Strings(out)
	return out, nil
}
