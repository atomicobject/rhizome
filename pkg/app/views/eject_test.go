package views

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestEjectRefusesToOverwriteAndWritesNothing(t *testing.T) {
	vault := t.TempDir()
	group := filepath.Join(viewconfig.DefaultSourceRoot(vault), "group")
	require.NoError(t, os.MkdirAll(group, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(group, "trace.tsx"), []byte("mine"), 0o644))
	service := New(ServiceOptions{VaultPath: vault, Bundled: bundledGroupViews()})

	_, err := service.Eject(context.Background(), BriefingViewID)
	require.ErrorIs(t, err, ErrEjectRefused)
	require.ErrorContains(t, err, ".rhizome/views/group/trace.tsx")
	entries, err := os.ReadDir(group)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a refused eject writes no file")
	mine, err := os.ReadFile(filepath.Join(group, "trace.tsx"))
	require.NoError(t, err)
	require.Equal(t, "mine", string(mine))
}

func TestEjectOnlyCopiesBundledViews(t *testing.T) {
	service := New(ServiceOptions{VaultPath: t.TempDir(), Bundled: bundledGroupViews()})
	_, err := service.Eject(context.Background(), "team.board")
	require.ErrorIs(t, err, ErrViewNotFound)
}

func TestEjectCopiesTheWholeFolder(t *testing.T) {
	vault := t.TempDir()
	result, err := New(ServiceOptions{VaultPath: vault, Bundled: bundledGroupViews()}).Eject(context.Background(), SectionsViewID)
	require.NoError(t, err)
	require.Equal(t, ".rhizome/views/group", result.Folder)
	require.Len(t, result.Written, 6, "sibling views share the folder and come along")
	catalog, err := New(ServiceOptions{VaultPath: vault, Bundled: bundledGroupViews()}).Catalog(context.Background())
	require.NoError(t, err)
	require.Empty(t, catalog.Issues)
	for _, entry := range catalog.Views {
		require.NotEqual(t, viewconfig.OriginBundled, entry.Origin, "%s still bundled", entry.ID)
	}
}

// RHIZOME_BUNDLED_VIEWS_DIR serves the source folder, tests and fixtures
// included; an eject copies only what the kit build ships.
func TestEjectLeavesTestsAndFixturesBehind(t *testing.T) {
	bundled := bundledGroupViews()
	for _, rel := range []string{"group/briefing.test.tsx", "group/__fixtures__/records.json", "group/fixtures/records.json", "group/lib/model.test.ts"} {
		bundled[rel] = &fstest.MapFile{Data: []byte("test only")}
	}
	bundled["group/lib/model.ts"] = &fstest.MapFile{Data: []byte("export const model = 1")}
	result, err := New(ServiceOptions{VaultPath: t.TempDir(), Bundled: bundled}).Eject(context.Background(), BriefingViewID)
	require.NoError(t, err)
	require.Len(t, result.Written, 7, "six view files and the shared model")
	require.Contains(t, result.Written, ".rhizome/views/group/lib/model.ts")
}

// failingFS fails to open one file, as a read error mid-copy would.
type failingFS struct {
	fstest.MapFS
	broken string
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.broken {
		return nil, errors.New("unreadable")
	}
	return f.MapFS.Open(name)
}

func (f failingFS) ReadFile(name string) ([]byte, error) {
	if name == f.broken {
		return nil, errors.New("unreadable")
	}
	return f.MapFS.ReadFile(name)
}

func TestEjectRemovesWhatItWroteWhenACopyFails(t *testing.T) {
	vault := t.TempDir()
	bundled := bundledGroupViews()
	bundled["group/zz/late.ts"] = &fstest.MapFile{Data: []byte("export {}")}
	service := New(ServiceOptions{VaultPath: vault, Bundled: failingFS{MapFS: bundled, broken: "group/zz/late.ts"}})

	_, err := service.Eject(context.Background(), BriefingViewID)
	require.ErrorContains(t, err, "unreadable")
	entries, err := os.ReadDir(viewconfig.DefaultSourceRoot(vault))
	require.NoError(t, err)
	require.Empty(t, entries, "a failed eject leaves no partial copy")

	service = New(ServiceOptions{VaultPath: vault, Bundled: bundledGroupViews()})
	_, err = service.Eject(context.Background(), BriefingViewID)
	require.NoError(t, err, "a retry is not refused by the failed attempt's files")
}

func TestEjectRefusesWhenASiblingViewIsAlreadyOverridden(t *testing.T) {
	vault := t.TempDir()
	override := filepath.Join(viewconfig.DefaultSourceRoot(vault), "custom")
	require.NoError(t, os.MkdirAll(override, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(override, "trace.yaml"), []byte("apiVersion: rhizome.view.v1\nid: group.trace\nname: Our trace\nsource: {kind: custom, entry: trace.tsx}\nmount: {kind: group, group: \"*\"}\n"), 0o644))
	_, err := New(ServiceOptions{VaultPath: vault, Bundled: bundledGroupViews()}).Eject(context.Background(), BriefingViewID)
	require.ErrorIs(t, err, ErrEjectRefused)
	require.ErrorContains(t, err, "group.trace")
	_, statErr := os.Stat(filepath.Join(viewconfig.DefaultSourceRoot(vault), "group"))
	require.True(t, os.IsNotExist(statErr), "nothing is written")
}

// A repository that ejected the group views before the type Briefing joined
// their folder is refused with the way forward: move the earlier copy out of
// .rhizome/views, eject again, and reapply its changes.
func TestEjectAfterAnEarlierGroupEjectSaysHowToProceed(t *testing.T) {
	vault := t.TempDir()
	earlier := New(ServiceOptions{VaultPath: vault, Bundled: bundledGroupViews()})
	_, err := earlier.Eject(context.Background(), BriefingViewID)
	require.NoError(t, err)

	service := New(ServiceOptions{VaultPath: vault, Bundled: bundledViews()})
	_, err = service.Eject(context.Background(), TypeBriefingViewID)
	require.ErrorIs(t, err, ErrEjectRefused)
	require.ErrorContains(t, err, "move .rhizome/views/group out of .rhizome/views, eject type.briefing again, and reapply your changes to the new copy")

	views := viewconfig.DefaultSourceRoot(vault)
	require.NoError(t, os.Rename(filepath.Join(views, "group"), filepath.Join(vault, "group-earlier")))
	result, err := service.Eject(context.Background(), TypeBriefingViewID)
	require.NoError(t, err)
	require.Contains(t, result.Written, ".rhizome/views/group/type-briefing.yaml")
	require.Contains(t, result.Written, ".rhizome/views/group/briefing.yaml")
}
