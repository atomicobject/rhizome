package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestInitRuntimeUsesProvidedRuntimeWithoutOwnership(t *testing.T) {
	provided := &Runtime{}

	rt, status, err := initRuntime(context.Background(), Config{
		Vault:        &obsidian.Vault{Name: "testvault"},
		VaultDef:     obsidian.VaultDefinition{Name: "testvault", Path: t.TempDir()},
		VaultPath:    t.TempDir(),
		Runtime:      provided,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Same(t, provided, rt)
	require.Empty(t, ownedCleanup(rt))
	require.True(t, status.Ready)
	require.Equal(t, "embeddings disabled", status.ReadyReason)
}

func TestNewServerRequiresExplicitNoteMetadataIndexer(t *testing.T) {
	root := t.TempDir()
	server, err := NewServer(context.Background(), Config{
		Vault:     &obsidian.Vault{Name: "testvault"},
		VaultDef:  obsidian.VaultDefinition{Name: "testvault", Path: root},
		VaultPath: root,
	}, nil)
	require.Nil(t, server)
	require.ErrorContains(t, err, "note metadata indexer")
}

func TestNewServerAllowsRemoteApplicationWithoutHTMLContentOrigin(t *testing.T) {
	for _, origin := range []string{"http://192.0.2.10:8787", "https://rhizome.example.test"} {
		t.Run(origin, func(t *testing.T) {
			root := t.TempDir()
			server, err := NewServer(context.Background(), Config{
				Vault:             &obsidian.Vault{Name: "testvault"},
				VaultDef:          obsidian.VaultDefinition{Name: "testvault", Path: root},
				VaultPath:         root,
				Runtime:           &Runtime{},
				NoteMetadata:      testNoteMetadataIndexer(t),
				ApplicationOrigin: origin,
			}, nil)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, server.Close()) })

			request := httptest.NewRequest(http.MethodGet, origin+"/api/v1/status", nil)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)

			request.Host = "attacker.example.test"
			response = httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			require.Equal(t, http.StatusForbidden, response.Code)
		})
	}
}

func TestSemanticConfigCarriesServerNoteMetadataIndexer(t *testing.T) {
	noteMetadata := testNoteMetadataIndexer(t)
	server := &Server{
		cfg:          Config{NoteMetadata: noteMetadata},
		noteMetadata: noteMetadata,
		runtime:      &Runtime{},
	}

	config := server.semanticConfig()
	require.NoError(t, config.NoteMetadata.Validate())
	require.Equal(t, noteMetadata, config.NoteMetadata)
}

func TestSetIntelStoreClosesOwnedStoreWhenLiveStoreReplacesFallback(t *testing.T) {
	oldStore, err := semdb.Open(filepath.Join(t.TempDir(), "old.db"))
	require.NoError(t, err)
	newStore, err := semdb.Open(filepath.Join(t.TempDir(), "new.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = newStore.Close() })

	rt := &Runtime{IntelStore: oldStore, ownsIntel: true}
	rt.SetIntelStore(newStore)

	require.Error(t, oldStore.DB().PingContext(context.Background()))
	require.NoError(t, newStore.DB().PingContext(context.Background()))
	require.Empty(t, ownedCleanup(rt))
}

type fakeLiveHealthProvider struct{}

func (fakeLiveHealthProvider) LiveHealth() bootstrap.LiveHealth {
	return bootstrap.LiveHealth{
		Role:              "leader",
		WatcherMode:       "single-process",
		PendingDirtyCount: 2,
		Scheduler: bootstrap.LiveSchedulerHealth{
			EmbeddingPendingNotes: 1,
			GraphPending:          true,
		},
		LastCompletedEpoch: &bootstrap.LiveEpoch{
			ID:     7,
			Status: "completed",
			Phase:  "complete",
		},
	}
}

func TestStatusResponseSerializesLiveHealth(t *testing.T) {
	rt := &Runtime{}
	rt.SetLiveHealthProvider(fakeLiveHealthProvider{})
	srv := &Server{
		runtime: rt,
		lastReady: StatusResponse{
			VaultName: "testvault",
			VaultPath: t.TempDir(),
			Ready:     true,
		},
	}

	status := srv.status(context.Background())
	require.NotNil(t, status.Live)
	require.Equal(t, "leader", status.Live.Role)
	require.Equal(t, 2, status.Live.PendingDirtyCount)

	payload, err := json.Marshal(status)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"live"`)
	require.Contains(t, string(payload), `"lastCompletedEpoch"`)
}
