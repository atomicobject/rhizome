package indexing

import (
	"context"
	"fmt"
	"os"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/semanticops"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func prepareProvider(cfg embeddings.Config, apiKey string) (embeddings.Provider, embeddings.ProviderConfig, error) {
	return semanticops.PrepareProvider(cfg, apiKey)
}

func persistCodeEmbeddingsConfig(vaultPath string, cfg embeddings.Config, explicit bool) error {
	if cfg.Enabled {
		return obsidian.SaveCodeEmbeddingsConfig(vaultPath, cfg)
	}

	if explicit {
		return obsidian.SaveCodeEmbeddingsConfig(vaultPath, cfg)
	}

	return nil
}

func shouldAutoVacuum(ctx context.Context, store *semdb.Store) (bool, error) {
	if store == nil {
		return false, nil
	}
	stats, err := store.VacuumStats(ctx)
	if err != nil {
		return false, err
	}
	return vacuumStatsNeedReclaim(stats), nil
}

// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us3-ac2]]
// WHY: full VACUUM rewrites the DB and needs exclusive access, so automatic
// maintenance only runs it when SQLite freelist stats prove meaningful reclaim.
func vacuumStatsNeedReclaim(stats semdb.VacuumStats) bool {
	const (
		largeReclaimableBytes = 64 * 1024 * 1024
		minReclaimableBytes   = 16 * 1024 * 1024
		minFreeRatio          = 0.20
	)
	if stats.ReclaimableBytes >= largeReclaimableBytes {
		return true
	}
	return stats.ReclaimableBytes >= minReclaimableBytes && stats.FreeRatio() >= minFreeRatio
}

func recordAutoVacuum(dbPath string) error {
	if dbPath == "" {
		return nil
	}
	statePath := dbPath + ".vacuum_state"
	return os.WriteFile(statePath, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0o644)
}
