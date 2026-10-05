package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAgentIdentifierSuggestionsUseRefreshedSchema(t *testing.T) {
	const initial = `type Spec @node(paths: ["specs/*.md"]) { id: String! @field @identifier(preferred: true, prefix: "SPEC") }`
	const sibling = `
type Archive @node(paths: ["archive/*.md"]) { legacyId: String! @field(source: "legacy-id") @identifier(preferred: true, prefix: "SPEC") }
`
	for _, tc := range []struct {
		name, command, freshSource string
		removeSchema               bool
	}{
		{"next-id", "next-id", "", false},
		{"guide", "ontology-authoring-guide", "", false},
		{"guide schema unavailable", "ontology-authoring-guide", "", true},
		{"guide changed prefix", "ontology-authoring-guide", "id", false},
		{"guide changed prefix and source", "ontology-authoring-guide", "record-key", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{
				".rhizome/ontology/schema.graphql": initial,
				"specs/one.md":                     "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# One\n",
				"archive/two.md":                   "---\nlegacy-id: SPEC-0002\naliases: [SPEC-0002]\n---\n# Two\n",
			}
			if tc.freshSource != "" {
				files["specs/one.md"] = "---\nid: REQ-0007\nrecord-key: REQ-0007\naliases: [REQ-0007]\n---\n# One\n"
			}
			vault := setupAgentTestVault(t, files)
			original, err := ontology.LoadSchema(vault.path)
			require.NoError(t, err)
			configFile := obsidian.ObsidianConfigFile
			t.Cleanup(func() { obsidian.ObsidianConfigFile = configFile })
			var once sync.Once
			var changed bool
			var changeErr error
			obsidian.ObsidianConfigFile = func() (string, error) {
				pcs := make([]uintptr, 40)
				frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs)])
				for {
					frame, more := frames.Next()
					if strings.HasSuffix(frame.Function, "/bootstrap.NewLiveRuntime") {
						// Edit only at runtime startup, after the guide's schema load.
						once.Do(func() {
							schemaPath := filepath.Join(vault.path, ".rhizome/ontology/schema.graphql")
							if tc.removeSchema {
								changeErr = os.Rename(schemaPath, schemaPath+".disabled")
							} else {
								fresh := initial + sibling
								if tc.freshSource != "" {
									fresh = `type Spec @node(paths: ["specs/*.md"]) { id: String! @field(source: "` + tc.freshSource + `") @identifier(preferred: true, prefix: "REQ") }`
								}
								changeErr = os.WriteFile(schemaPath, []byte(fresh), 0o644)
							}
							changed = changeErr == nil
						})
						break
					}
					if !more {
						break
					}
				}
				if changeErr != nil {
					return "", changeErr
				}
				return configFile()
			}
			stdout, stderr, runErr := runRootCLI(t, nil, []string{"agent", tc.command, "--vault", vault.name, "--type", "Spec"})
			require.NoError(t, runErr)
			require.Empty(t, stderr)
			require.True(t, changed, "schema edit must occur during actual runtime startup")
			if tc.removeSchema {
				require.Contains(t, stdout, "# Ontology Authoring Guide")
				require.Contains(t, stdout, "## Spec")
				require.NotContains(t, stdout, "next available id:")
				return
			}
			current, err := ontology.LoadSchema(vault.path)
			require.NoError(t, err)
			require.NotEqual(t, original.Hash, current.Hash)
			store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			defer store.Close()
			state, err := store.GetOntologySchemaState(context.Background())
			require.NoError(t, err)
			require.Equal(t, current.Hash, state.SchemaHash)
			if tc.freshSource != "" {
				next, err := idalloc.Allocate(context.Background(), current, store, "Spec")
				require.NoError(t, err)
				require.Equal(t, "REQ-0008", next.Next)
				require.Contains(t, stdout, "next available id: `REQ-0008`")
				require.Contains(t, stdout, `"prefix":"REQ"`)
				require.NotContains(t, stdout, `"prefix":"SPEC"`)
				require.Contains(t, stdout, "frontmatter key `"+tc.freshSource+"`")
				require.Contains(t, stdout, "\n"+tc.freshSource+": REQ-0008\n")
				return
			}
			owners, err := store.OntologyPathsByType(context.Background(), "Archive", 0)
			require.NoError(t, err)
			require.Equal(t, []string{"archive/two.md"}, owners)
			if tc.command == "next-id" {
				var result idalloc.Result
				require.NoError(t, json.Unmarshal([]byte(stdout), &result))
				require.Equal(t, "SPEC-0003", result.Next)
				require.Equal(t, []string{"Archive"}, result.SharedWith)
				require.Equal(t, 2, result.OwnersScanned)
			} else {
				require.Contains(t, stdout, "next available id: `SPEC-0003`")
				require.Contains(t, stdout, "id: SPEC-0003")
				require.NotContains(t, stdout, "SPEC-0002")
			}
		})
	}
}

func TestAgentNextIDSchemaAdmissionErrors(t *testing.T) {
	for _, tc := range []struct {
		name, schema, message string
	}{
		{"missing", "", "no ontology files found"},
		{"invalid", "type Broken {", "schema.graphql:1:14: Expected Name, found <EOF>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			if tc.schema != "" {
				files[".rhizome/ontology/schema.graphql"] = tc.schema
			}
			vault := setupAgentTestVault(t, files)
			stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "next-id", "--vault", vault.name, "--type", "Spec"})
			require.ErrorIs(t, err, silentExitError{code: 1})
			require.Empty(t, stdout)
			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
			require.Equal(t, map[string]any{"error": tc.message}, payload)
		})
	}
}
