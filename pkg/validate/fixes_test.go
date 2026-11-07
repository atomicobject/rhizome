package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunOntology_IdentifierBlockIDMigrationFix(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"specs/one.md": `---
type: Spec
summary: One
---

# One

## Stories

### Story
id:: SPEC-0023.US1
status:: TODO
^spec-0023-us1
`,
		"notes/ref.md": `# Ref

See [[one#^spec-0023-us1]].
See [story](../specs/one.md#^spec-0023-us1).
`,
	}))

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}
	result := preparedCheck(t, context.Background(), runCtx, ScanOptions{}, CheckOntology)
	require.Empty(t, result.Error)
	var fix *FixAction
	for i := range result.Fixes {
		if result.Fixes[i].IssueCode == "identifier_block_id_migration" {
			fix = &result.Fixes[i]
			break
		}
	}
	require.NotNil(t, fix)
	applyCheckFixThroughPlan(t, runCtx, result, *fix)

	spec, err := os.ReadFile(filepath.Join(root, "specs/one.md"))
	require.NoError(t, err)
	assert.Contains(t, string(spec), "id:: ^SPEC-0023-US1")
	assert.NotContains(t, string(spec), "\n^spec-0023-us1\n")

	ref, err := os.ReadFile(filepath.Join(root, "notes/ref.md"))
	require.NoError(t, err)
	assert.Contains(t, string(ref), "[[one#^SPEC-0023-US1]]")
	assert.Contains(t, string(ref), "](../specs/one.md#^SPEC-0023-US1)")
	assert.NotContains(t, string(ref), "spec-0023-us1")
}

func TestRunOntology_IdentifierBlockIDMigrationAcceptsBulletMetadataBlockID(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"specs/one.md": `---
type: Spec
summary: One
---

# One

## Stories

### Story

- id:: ^SPEC-0023-US1
- status:: ready
`,
	}))

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}
	result := preparedCheck(t, context.Background(), runCtx, ScanOptions{}, CheckOntology)
	require.Empty(t, result.Error)
	for _, issue := range result.Issues {
		require.NotEqual(t, "identifier_block_id_migration", issue.Code)
	}
	for _, fix := range result.Fixes {
		require.NotEqual(t, "identifier_block_id_migration", fix.IssueCode)
	}
}

func TestRunOntology_IdentifierBlockIDMigrationPreservesCanonicalPackedInlineProperties(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  summary: String @field
  status: String @field
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"docs/specs/product/access-administration.md": `---
type: Spec
summary: Access administration
---

# Access administration

## Stories

### Superadmin access
id:: ^SPEC-0016-US1 summary:: A SxanPro superadmin can provision and revise crash-cart access across customer
organizations from one app-owned workflow. status:: ready
`,
		"docs/efforts/access.md": `# Access effort

See [[access-administration#^SPEC-0016-US1]].
`,
	}))

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}
	result := preparedCheck(t, context.Background(), runCtx, ScanOptions{}, CheckOntology)
	require.Empty(t, result.Error)
	for _, issue := range result.Issues {
		require.NotEqual(t, "identifier_block_id_migration", issue.Code)
	}
	for _, fix := range result.Fixes {
		require.NotEqual(t, "identifier_block_id_migration", fix.IssueCode)
	}

	spec, err := os.ReadFile(filepath.Join(root, "docs/specs/product/access-administration.md"))
	require.NoError(t, err)
	assert.Contains(t, string(spec), "id:: ^SPEC-0016-US1 summary:: A SxanPro superadmin can provision and revise crash-cart access across customer\norganizations from one app-owned workflow. status:: ready")
	assert.Contains(t, string(spec), "status:: ready")
	assert.NotContains(t, string(spec), "customer-summary")

	effort, err := os.ReadFile(filepath.Join(root, "docs/efforts/access.md"))
	require.NoError(t, err)
	assert.Contains(t, string(effort), "[[access-administration#^SPEC-0016-US1]]")
}

func TestResolveChecksRejectsRemovedCardIndexCheck(t *testing.T) {
	_, err := ResolveChecks([]string{"card-index"})
	require.ErrorContains(t, err, `unknown check "card-index"`)
}

// --- helpers ---

func openTestStore(t *testing.T, root string) *semdb.Store {
	t.Helper()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func writeFixtureFiles(root string, files map[string]string) error {
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// A block cited only from a code comment is not an orphan, and renaming it
// would strand the comment, so neither fix may apply without confirmation.
func TestBlockIDFixesRespectCodeCommentReferences(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "code:\n  enabled: true\n",
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"specs/one.md": `---
type: Spec
summary: One
---

# One

## Stories

### Story
id:: SPEC-0023.US1
^spec-0023-us1
`,
		"pkg/impl.go": "package impl\n\n// Coderefs: [[one#^spec-0023-us1]]\nfunc F() {}\n",
	}))
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}

	orphans := RunOrphanBlockIDs(context.Background(), runCtx)
	require.True(t, orphans.OK, "%+v", orphans)
	for _, fix := range orphans.Fixes {
		assert.NotEqual(t, "spec-0023-us1", fix.Edits[0].BlockID, "a block cited from code must not be offered for removal")
	}

	ontologyResult := preparedCheck(t, context.Background(), runCtx, ScanOptions{}, CheckOntology)
	require.Empty(t, ontologyResult.Error)
	var migration *FixAction
	for i := range ontologyResult.Fixes {
		if ontologyResult.Fixes[i].IssueCode == "identifier_block_id_migration" {
			migration = &ontologyResult.Fixes[i]
		}
	}
	require.NotNil(t, migration)
	assert.Equal(t, FixSafetyConfirm, migration.Safety, "renaming a block that code cites must need confirmation")
}

// When the configured code files cannot all be listed, nothing proves a block
// is uncited, so its removal needs confirmation.
func TestOrphanBlockIDRemovalNeedsConfirmationWhenCodeScanIsIncomplete(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "code:\n  enabled: true\n  scan: [\"pkg/[\"]\n",
		"notes/two.md":        "# Two\n\nA paragraph.\n^orphan-one\n",
	}))
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}

	orphans := RunOrphanBlockIDs(context.Background(), runCtx)
	var removal *FixAction
	for i := range orphans.Fixes {
		if orphans.Fixes[i].Edits[0].BlockID == "orphan-one" {
			removal = &orphans.Fixes[i]
		}
	}
	require.NotNil(t, removal, "%+v", orphans)
	assert.Equal(t, FixSafetyConfirm, removal.Safety)
}
