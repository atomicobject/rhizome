package indexing

import (
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestVacuumStatsNeedReclaimUsesMeaningfulFreeSpace(t *testing.T) {
	require.False(t, vacuumStatsNeedReclaim(semdb.VacuumStats{
		PageCount:        1000,
		FreelistCount:    100,
		PageSize:         4096,
		ReclaimableBytes: 100 * 4096,
	}))

	require.True(t, vacuumStatsNeedReclaim(semdb.VacuumStats{
		PageCount:        100000,
		FreelistCount:    20000,
		PageSize:         4096,
		ReclaimableBytes: 20000 * 4096,
	}))

	require.True(t, vacuumStatsNeedReclaim(semdb.VacuumStats{
		PageCount:        100000,
		FreelistCount:    4000,
		PageSize:         4096,
		ReclaimableBytes: 64 * 1024 * 1024,
	}))
}

func TestVacuumStatsNeedReclaimRequiresRatioForModerateFreeSpace(t *testing.T) {
	require.False(t, vacuumStatsNeedReclaim(semdb.VacuumStats{
		PageCount:        100000,
		FreelistCount:    5000,
		PageSize:         4096,
		ReclaimableBytes: 5000 * 4096,
	}))

	require.True(t, vacuumStatsNeedReclaim(semdb.VacuumStats{
		PageCount:        10000,
		FreelistCount:    5000,
		PageSize:         4096,
		ReclaimableBytes: 5000 * 4096,
	}))
}

func TestPersistCodeEmbeddingsConfig_PreservesExplicitDisable(t *testing.T) {
	t.Parallel()

	vault := t.TempDir()

	initial := obsidian.LocalConfig{
		CodeEmbeddings: &embeddings.Config{Enabled: false, Provider: "code", Model: "code-model"},
	}
	if err := obsidian.SaveLocalConfig(vault, initial); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	codeCfg := embeddings.DefaultConfig(vault)
	codeCfg.Enabled = false
	codeCfg.Provider = "code"
	codeCfg.Model = "code-model"

	if err := persistCodeEmbeddingsConfig(vault, codeCfg, true); err != nil {
		t.Fatalf("persist: %v", err)
	}

	reloaded, err := obsidian.LoadLocalConfig(vault)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.CodeEmbeddings == nil {
		t.Fatalf("expected code embeddings block to persist")
	}
	if reloaded.CodeEmbeddings.Enabled {
		t.Fatalf("expected explicit disabled code embeddings to remain disabled")
	}
}
