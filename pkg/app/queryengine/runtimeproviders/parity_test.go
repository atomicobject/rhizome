package runtimeproviders

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	semdb "github.com/atomicobject/rhizome/pkg/store"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSharedProvidersExecuteOntologyAndCodeRoots(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`type Spec @node(paths:["specs/*.md"]) {id:String! @identifier(preferred:true,prefix:"SPEC")}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing.go"), []byte("package pkg\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing_test.go"), []byte("package pkg\n"), 0o600))
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	exec, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	vault := obsidian.VaultDefinition{Path: root}
	deps, err := queryengine.Dependencies(ontologyquery.Deps{VaultDef: vault, Store: store, NoteReader: &obsidian.Note{}}, unifiedsearch.Options{VaultPath: root, VaultDef: vault, IntelStore: store})
	require.NoError(t, err)
	providers := New(schema, exec, deps, nil)
	deps.OntologyRuntime, deps.CodeRuntime = providers, providers
	engine, err := queryengine.New(schema, exec, deps)
	require.NoError(t, err)
	prepared, errs := ontologyquery.Prepare(exec, `{
 ontology { nextId(type:"Spec") {available next} authoringGuide(type:"Spec") {available markdown} }
 code { testsForCode(path:"pkg/thing.go") {available tests {path}} }
 }`)
	require.Empty(t, errs)
	result, err := engine.Run(context.Background(), prepared)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	ontologyData := result.Data["ontology"].(map[string]any)
	require.Equal(t, true, ontologyData["nextId"].(map[string]any)["available"])
	require.Equal(t, "SPEC-0001", ontologyData["nextId"].(map[string]any)["next"])
	require.Equal(t, true, ontologyData["authoringGuide"].(map[string]any)["available"])
	require.NotEmpty(t, ontologyData["authoringGuide"].(map[string]any)["markdown"])
	codeData := result.Data["code"].(map[string]any)["testsForCode"].(map[string]any)
	require.Equal(t, true, codeData["available"])
	require.Equal(t, []any{map[string]any{"path": "pkg/thing_test.go"}}, codeData["tests"])
}
