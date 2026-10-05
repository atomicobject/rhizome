package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/qualityeval"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func TestCopyIsolatedPopulationExcludesForeignCorporaAndKeepsInternalSymlinkContent(t *testing.T) {
	source := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "docs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(source, "testdata", "search-quality", "heldout-new"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "docs", "kept.md"), []byte("kept"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "testdata", "search-quality", "heldout-new", "secret.md"), []byte("secret"), 0o644))
	require.NoError(t, os.Symlink("kept.md", filepath.Join(source, "docs", "alias.md")))

	destination := filepath.Join(t.TempDir(), "staged")
	allowed, err := copyIsolatedPopulation(source, destination, []string{"testdata"})
	require.NoError(t, err)
	require.Contains(t, allowed, "docs/alias.md")
	content, err := os.ReadFile(filepath.Join(destination, "docs", "alias.md"))
	require.NoError(t, err)
	require.Equal(t, "kept", string(content))
	_, err = os.Stat(filepath.Join(destination, "testdata"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestTypedFixtureCarriesCanonicalGoModuleIdentityIntoIsolation(t *testing.T) {
	source := filepath.Join("..", "..", "testdata", "search-quality", "typed-note-vault")
	staged := filepath.Join(t.TempDir(), "unrelated-parent", "typed-note-fixture")
	_, err := copyIsolatedPopulation(source, staged, nil)
	require.NoError(t, err)

	const rel = "internal/dispatch/queue_test.go"
	ref, err := paths.ResolveCodeRef(staged, rel)
	require.NoError(t, err)
	content, err := os.ReadFile(ref.Abs.String())
	require.NoError(t, err)
	summary, err := codeanchor.NewGoIndexer().IndexFile(content, ref)
	require.NoError(t, err)

	want := "github.com/atomicobject/rhizome/testdata/search-quality/typed-note-vault/internal/dispatch.TestQueueDrainQuarantinesTerminalFailureAndContinues"
	found := false
	for _, symbol := range summary.Symbols {
		if symbol.FQN == want {
			found = true
			break
		}
	}
	require.True(t, found, "staged fixture must retain its canonical module-derived FQN")

	// The authored ontology must survive relocation byte-for-byte and compile.
	authored, err := os.ReadFile(filepath.Join(source, ".rhizome", "ontology", "schema.graphql"))
	require.NoError(t, err)
	stagedSchema, err := os.ReadFile(filepath.Join(staged, ".rhizome", "ontology", "schema.graphql"))
	require.NoError(t, err)
	require.Equal(t, string(authored), string(stagedSchema))
	schema, err := ontology.LoadSchema(staged)
	require.NoError(t, err)
	for _, name := range []string{"Person", "Project", "Meeting", "ActionItem"} {
		require.Contains(t, schema.Types, name)
	}
	actionItem := schema.Types["ActionItem"]
	require.Equal(t, ontology.EmbeddedSourceShape("CHECKBOX_ITEM"), actionItem.SourceShape)
	require.Equal(t, "#action-item", actionItem.SourceMarker)
	require.Equal(t, []string{"meetings/*.md"}, actionItem.SourcePaths)
	require.Contains(t, actionItem.ByName, "assignee")
	require.Contains(t, actionItem.ByName, "due")
}

func TestCopyIsolatedPopulationKeepsAuthoredRhizomeInputsAndDropsRuntimeState(t *testing.T) {
	source := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(source, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	authored := map[string]string{
		".rhizome/config.yml":              "notes:\n  include: [\"**/*.md\"]\n",
		".rhizome/ignore":                  "archive/\n",
		".rhizome/ontology/schema.graphql": "type Person @node(paths: [\"people/*.md\"]) { name: String! }\n",
	}
	for rel, content := range authored {
		write(rel, content)
	}
	runtime := []string{".rhizome/index.sqlite", ".rhizome/cache/embeddings.bin", ".rhizome/sessions/abc.json", ".rhizome/runtime/manifest.json"}
	for _, rel := range runtime {
		write(rel, "generated")
	}

	destination := filepath.Join(t.TempDir(), "staged")
	_, err := copyIsolatedPopulation(source, destination, nil)
	require.NoError(t, err)
	for rel, content := range authored {
		got, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(rel)))
		require.NoError(t, err, rel)
		require.Equal(t, content, string(got), rel)
	}
	for _, rel := range runtime {
		_, err := os.Stat(filepath.Join(destination, filepath.FromSlash(rel)))
		require.ErrorIs(t, err, os.ErrNotExist, rel)
	}
}

func TestCopyIsolatedPopulationRejectsEscapingSymlink(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	require.NoError(t, os.MkdirAll(source, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "secret.md"), []byte("secret"), 0o644))
	require.NoError(t, os.Symlink("../secret.md", filepath.Join(source, "escape.md")))

	_, err := copyIsolatedPopulation(source, filepath.Join(parent, "staged"), nil)
	require.ErrorContains(t, err, "symlink escapes source population")
}

func TestCopyIsolatedPopulationRejectsEscapingOntologySymlink(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	require.NoError(t, os.MkdirAll(filepath.Join(source, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "secret.graphql"), []byte("type Secret { name: String }"), 0o644))
	require.NoError(t, os.Symlink("../../../secret.graphql", filepath.Join(source, ".rhizome", "ontology", "schema.graphql")))

	_, err := copyIsolatedPopulation(source, filepath.Join(parent, "staged"), nil)
	require.ErrorContains(t, err, "symlink escapes source population")
}

func TestCopyIsolatedPopulationRejectsSymlinkedOntologyDirectory(t *testing.T) {
	source := t.TempDir()
	schemas := filepath.Join(source, "schemas")
	require.NoError(t, os.MkdirAll(schemas, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemas, "schema.graphql"), []byte("type Person @node(paths: [\"people/*.md\"]) { name: String! }\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(source, ".rhizome"), 0o755))
	if err := os.Symlink(filepath.Join("..", "schemas"), filepath.Join(source, ".rhizome", "ontology")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := copyIsolatedPopulation(source, filepath.Join(t.TempDir(), "staged"), nil)
	require.ErrorContains(t, err, "symlinked ontology directory")
}

func TestCopyIsolatedPopulationRejectsSymlinkToExcludedPopulation(t *testing.T) {
	source := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "docs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(source, "testdata"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "testdata", "secret.md"), []byte("secret"), 0o644))
	require.NoError(t, os.Symlink("../testdata/secret.md", filepath.Join(source, "docs", "alias.md")))

	_, err := copyIsolatedPopulation(source, filepath.Join(t.TempDir(), "staged"), []string{"testdata"})
	require.ErrorContains(t, err, "symlink targets excluded source population")
}

func TestValidateInventoryPathsRejectsForbiddenAndEscapingPaths(t *testing.T) {
	allowed := map[string]struct{}{"pkg/search/search.go": {}}
	require.NoError(t, validateInventoryPaths("repo", []string{"pkg/search/search.go"}, []string{"testdata"}, allowed))
	require.ErrorContains(t, validateInventoryPaths("repo", []string{"testdata/search-quality/heldout-new/secret.md"}, []string{"testdata"}, allowed), "forbidden path")
	require.ErrorContains(t, validateInventoryPaths("repo", []string{"../outside.md"}, nil, allowed), "escaping path")
	require.ErrorContains(t, validateInventoryPaths("repo", []string{"docs/untracked.md"}, nil, allowed), "outside captured staged population")
}

func TestValidateCorpusDirectoryName(t *testing.T) {
	require.NoError(t, validateCorpusDirectoryName("rhizome-repository"))
	for _, name := range []string{"", ".", "..", "../escape", "nested/name", `nested\\name`} {
		require.Error(t, validateCorpusDirectoryName(name))
	}
}

func TestFileFingerprintBindsBinaryBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(path, []byte("first"), 0o755))
	first, err := fileFingerprint(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("second"), 0o755))
	second, err := fileFingerprint(path)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestConfigureIsolatedEmbeddingsPreservesProviderMode(t *testing.T) {
	// Live-provider isolation must not load or rewrite embedding configuration.
	require.NoError(t, configureIsolatedEmbeddings(filepath.Join(t.TempDir(), "missing-vault"), false, nil))
}

func TestConfigureIsolatedEmbeddingsAppliesExplicitLiveProviderToNotesAndCode(t *testing.T) {
	vault := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vault, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vault, ".rhizome", "config.yml"), []byte("noteEmbeddings:\n  provider: ollama\n"), 0o644))
	override := embeddings.Config{Enabled: true, Provider: "voyage", Model: "voyage-4-lite", Endpoint: "https://api.voyageai.com/v1/embeddings", Dimensions: 1024}

	require.NoError(t, configureIsolatedEmbeddings(vault, false, &override))
	note, err := obsidian.LoadEmbeddingsConfig(vault)
	require.NoError(t, err)
	code, explicit, err := obsidian.LoadCodeEmbeddingsConfig(vault)
	require.NoError(t, err)
	require.True(t, explicit)
	for _, got := range []embeddings.Config{note, code} {
		require.True(t, got.Enabled)
		require.Equal(t, "voyage", got.Provider)
		require.Equal(t, "voyage-4-lite", got.Model)
		require.Equal(t, 1024, got.Dimensions)
	}
}

func TestExcludedPopulationPathRejectsFutureSearchQualityArtifacts(t *testing.T) {
	require.True(t, excludedPopulationPath("docs/reference/analysis/search-quality-future-parent-review.md", nil))
	require.True(t, excludedPopulationPath("docs/reference/analysis/persistent-code-mode-evaluation.md", nil))
	require.True(t, excludedPopulationPath("docs/reference/analysis/persistent-code-mode-evaluation.json", nil))
	require.False(t, excludedPopulationPath("docs/reference/analysis/Search - Execution semantics.md", nil))
}

func TestRepositoryIsolationExcludesCurrentEvaluationContractAndFailsJudgedReference(t *testing.T) {
	source := t.TempDir()
	contract := "docs/specs/product/search-engine-quality.md"
	require.NoError(t, os.MkdirAll(filepath.Join(source, filepath.Dir(contract)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, contract), []byte("evaluation-derived"), 0o644))
	destination := filepath.Join(t.TempDir(), "staged")
	_, err := copyIsolatedPopulation(source, destination, repositoryIsolationExcludes)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(destination, contract))
	require.ErrorIs(t, err, os.ErrNotExist)

	corpus := qualityeval.Corpus{Cases: []qualityeval.Case{{ID: "dev-contract", Corpus: "rhizome-repository", Split: "development", Judgments: map[string]qualityeval.Judgment{contract: {Grade: 3}}}}}
	err = validateJudgedSourcesPresent(corpus, qualityeval.Selection{Split: "development"}, "rhizome-repository", corpusRoot{Root: destination})
	require.ErrorContains(t, err, "omits judged source")
}

func TestMissingRequiredIdentitiesChecksCanonicalEntityNotOnlyFile(t *testing.T) {
	inventory := semdb.IndexedPopulation{Identities: []string{"fqn\x00pkg.Present\x00src/shared.go"}, Paths: []string{"src/shared.go"}}
	caseDef := qualityeval.Case{RequiredSources: []string{"fqn\x00pkg.Missing\x00fixture/src/shared.go"}}
	require.Equal(t, []string{"fqn\x00pkg.Missing\x00fixture/src/shared.go"}, missingRequiredIdentities(caseDef, "fixture", inventory))
	caseDef.RequiredSources = []string{"fqn\x00pkg.Present\x00fixture/src/shared.go"}
	require.Empty(t, missingRequiredIdentities(caseDef, "fixture", inventory))
}

func TestFingerprintOriginalSourceDetectsFixtureChange(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "fixture")
	require.NoError(t, os.MkdirAll(source, 0o755))
	path := filepath.Join(source, "source.md")
	require.NoError(t, os.WriteFile(path, []byte("before"), 0o644))
	before, err := fingerprintOriginalSource(base, source)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("after"), 0o644))
	after, err := fingerprintOriginalSource(base, source)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}

func TestValidateUnresolvedGraphTargetsAllowsMissingReferenceButRejectsInternalOrForbiddenTarget(t *testing.T) {
	allowed := map[string]struct{}{"docs/ignored.md": {}}
	require.NoError(t, validateUnresolvedGraphTargets("repo", []string{"docs/not-authored.md"}, []string{"testdata"}, allowed))
	require.ErrorContains(t, validateUnresolvedGraphTargets("repo", []string{"docs/ignored.md"}, nil, allowed), "exists in staged population but is absent from index")
	require.ErrorContains(t, validateUnresolvedGraphTargets("repo", []string{"testdata/heldout/secret.md"}, []string{"testdata"}, allowed), "forbidden unresolved target")
}

func TestRecordMissingRequiredIdentityRetainsCorpusProvenance(t *testing.T) {
	run := qualityeval.Run{}
	provenance := qualityeval.CorpusRunProvenance{SourceFingerprint: "sha256:source", InventoryFingerprint: "sha256:inventory", IndexGeneration: "generation", IndexerBinaryFingerprint: "sha256:binary"}
	recordMissingRequiredIdentity(&run, "typed", provenance, "missing-case", []string{"fqn\x00pkg.Missing\x00source.go"})
	require.Equal(t, 1, run.FailureCount)
	require.Equal(t, provenance, run.Corpora["typed"])
	require.Equal(t, "missing_required_identity", run.Results[0].Status)
}
