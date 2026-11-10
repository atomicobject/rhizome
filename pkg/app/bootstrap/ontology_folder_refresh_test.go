package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestClassifyOntologyFolderChanges(t *testing.T) {
	for _, rel := range []string{
		".rhizome/ontology", ".rhizome/modules", ".rhizome/modules/team", ".rhizome/modules/team/ontology",
	} {
		t.Run(rel, func(t *testing.T) {
			require.Equal(t, internalChangeEffect{refreshOntology: true, invalidateCapabilities: true}, classifyInternalChange(rel))
		})
	}
	for _, rel := range []string{
		".rhizome/modules/Upper", ".rhizome/modules/Bad_Name/ontology", ".rhizome/modules/-bad",
		".rhizome/modules/team/views", ".rhizome/modules/team/ontology/cache.json", ".rhizome/ontology/cache.json",
	} {
		t.Run(rel, func(t *testing.T) {
			require.Equal(t, internalChangeEffect{}, classifyInternalChange(rel))
		})
	}
}

func TestLiveOwnershipOntologyFolderEventsRefreshUnchangedNotes(t *testing.T) {
	for _, folder := range []struct {
		name, path, schema string
	}{
		{"flat ontology", ".rhizome/ontology", ".rhizome/ontology/schema.graphql"},
		{"modules", ".rhizome/modules", ".rhizome/modules/team/ontology/schema.graphql"},
		{"module", ".rhizome/modules/team", ".rhizome/modules/team/ontology/schema.graphql"},
		{"module ontology", ".rhizome/modules/team/ontology", ".rhizome/modules/team/ontology/schema.graphql"},
	} {
		for _, operation := range []string{"create", "remove", "rename"} {
			t.Run(folder.name+"/"+operation, func(t *testing.T) {
				root := t.TempDir()
				writeWatcherConfig(t, root, "both")
				require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "project.md"), []byte("---\nname: Roadmap\n---\n"), 0o644))
				writeSchema := func() {
					schemaPath := filepath.Join(root, filepath.FromSlash(folder.schema))
					require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
					require.NoError(t, os.WriteFile(schemaPath, []byte(`type Project @node(paths: ["notes/*.md"]) { name: String! }`), 0o644))
				}
				before, after := "Project", ""
				if operation == "create" {
					before, after = after, before
				} else {
					writeSchema()
				}
				definition, err := repoconfig.LoadDefinitionFromPath(root)
				require.NoError(t, err)
				w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
				t.Cleanup(func() { require.NoError(t, store.Close()); require.NoError(t, cacheService.Close()) })
				assertType := func(want string) {
					typ, ok, err := store.GetOntologyTypeByPath(t.Context(), "notes/project.md")
					require.NoError(t, err)
					require.Equal(t, want != "", ok)
					if ok {
						require.Equal(t, want, typ.TypeName)
					}
				}
				cacheService.MarkDirty("notes/project.md", cache.DirtyModified)
				runOwnershipBatch(t, w)
				requireLiveOwnershipCompleted(t, w)
				assertType(before)

				var published []string
				w.publishEvent = func(kind string, _ any) { published = append(published, kind) }
				dirtyKind := cache.DirtyRemoved
				folderPath := filepath.Join(root, filepath.FromSlash(folder.path))
				switch operation {
				case "create":
					writeSchema()
					dirtyKind = cache.DirtyModified
				case "remove":
					require.NoError(t, os.RemoveAll(folderPath))
				case "rename":
					require.NoError(t, os.Rename(folderPath, filepath.Join(root, "retired")))
				}
				cacheService.MarkDirty(folder.path, dirtyKind)
				runOwnershipBatch(t, w)
				requireLiveOwnershipCompleted(t, w)
				assertType(after)
				require.Contains(t, published, appruntime.GlobalEventSchemaInvalidated)
				require.Contains(t, published, appruntime.GlobalEventCapabilitiesInvalidated)
			})
		}
	}
}
