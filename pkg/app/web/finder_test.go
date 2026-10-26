package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFindServesIndexedNotesAndCodeWithFrecency(t *testing.T) {
	t.Parallel()
	fixture := prepareWebFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var found FindResponse
	getJSON(t, httpSrv.URL+"/api/v1/find?q=sync", &found)
	require.NotEmpty(t, found.Items)
	byPath := map[string]FindItem{}
	for _, item := range found.Items {
		byPath[item.Path] = item
	}
	note, ok := byPath["notes/sync-strategy.md"]
	require.True(t, ok, "indexed note is listed: %+v", found.Items)
	require.Equal(t, "note", string(note.Kind))
	require.Equal(t, "Sync Strategy", note.Title)
	require.Equal(t, [][2]int{{0, 4}}, note.TitleMatches)
	var code *FindItem
	for _, item := range found.Items {
		if item.Kind == "code" {
			code = &item
			break
		}
	}
	require.NotNil(t, code, "indexed code files are listed")
	require.NotEmpty(t, code.Lang)

	getJSON(t, httpSrv.URL+"/api/v1/find?q=ignored", &found)
	for _, item := range found.Items {
		require.NotEqual(t, ".venv/ignored.md", item.Path, "ignored paths never reach the catalog")
	}

	getJSONStatus(t, httpSrv.URL+"/api/v1/find?kind=folder", http.StatusBadRequest, &map[string]any{})
	getJSONStatus(t, httpSrv.URL+"/api/v1/find?limit=0", http.StatusBadRequest, &map[string]any{})

	resp, err := http.Post(httpSrv.URL+"/api/v1/find/opens", "application/json", bytes.NewReader([]byte(`{"path":"notes/release-plan.md"}`)))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp, err = http.Post(httpSrv.URL+"/api/v1/find/opens", "application/json", bytes.NewReader([]byte(`{"path":"../outside.md"}`)))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	getJSON(t, httpSrv.URL+"/api/v1/find?limit=3", &found)
	require.Len(t, found.Items, 3)
	require.Equal(t, "notes/release-plan.md", found.Items[0].Path, "an empty query lists recent opens first")
}

func TestFindCatalogRebuildsOnIndexEvents(t *testing.T) {
	t.Parallel()
	fixture := prepareWebFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()
	before, err := srv.finder.Catalog(ctx)
	require.NoError(t, err)
	srv.NotifyGlobalEvent(GlobalEventValidateChanged, nil)
	time.Sleep(2 * finderRebuildDelay)
	unchanged, err := srv.finder.Catalog(ctx)
	require.NoError(t, err)
	require.Same(t, before, unchanged, "unrelated events keep the catalog")

	srv.NotifyGlobalEvent(GlobalEventIndexChanged, nil)
	require.Eventually(t, func() bool {
		after, err := srv.finder.Catalog(ctx)
		return err == nil && after != before && after.Len() == before.Len()
	}, 5*time.Second, 10*time.Millisecond)
}
