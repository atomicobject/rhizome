package cmd

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyQueryCommandRefreshesStaleStore(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	indexOntologyTestVault(t, vault.path)

	projectPath := filepath.Join(vault.path, "notes", "projects", "roadmap-refresh.md")
	body, err := os.ReadFile(projectPath)
	require.NoError(t, err)
	updated := strings.Replace(string(body), "Roadmap Refresh", "Updated Roadmap", 1)
	require.NoError(t, os.WriteFile(projectPath, []byte(updated), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"ontology", "query", "--vault", vault.name,
		"--query", `{ project(path: "notes/projects/roadmap-refresh.md") { name } }`,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var result ontologyquery.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Updated Roadmap", rows[0].(map[string]any)["name"])
}

func TestOntologyQueryCommandReportsRuntimeFailureAsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"ontology", "query", "--vault", vault.name,
		"--query", `{ project(semantic: "roadmap") { path } }`,
	})
	require.Error(t, err)
	require.Empty(t, stderr)

	var result ontologyquery.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "embedding")
}

func TestOntologyValidateCommandRefreshesStaleStore(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	indexOntologyTestVault(t, vault.path)

	projectPath := filepath.Join(vault.path, "notes", "projects", "roadmap-refresh.md")
	require.NoError(t, os.WriteFile(projectPath, []byte(`---
type: Project
owner: notes/people/alice.md
---
`), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "validate", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "missing_required_field")
	require.Contains(t, stdout, "Resolved notes by type:")
	require.Contains(t, stdout, "Decision (2)")
	require.Contains(t, stdout, "notes/projects/roadmap-refresh.md")
}

func TestOntologyValidateCommandListsNotesByType(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "validate", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Ontology valid.")
	require.Contains(t, stdout, "Resolved notes by type:")
	require.Contains(t, stdout, "Decision (2)")
	require.Contains(t, stdout, "Person (1)")
	require.Contains(t, stdout, "Project (1)")
	require.Contains(t, stdout, "Team (1)")
	require.Contains(t, stdout, "notes/decisions/backlink-decision.md")
	require.Contains(t, stdout, "notes/teams/platform-team.md")
}

func TestOntologyValidateCommandSkipsSectionTypesInInventory(t *testing.T) {
	files := maps.Clone(ontologyCommandFiles())
	delete(files, "notes/specs/search-rewrite.md")
	files[".rhizome/ontology/schema.graphql"] = `
enum ProjectStatus {
  ACTIVE
  PAUSED
}

enum DecisionStatus {
  PROPOSED
  ACCEPTED
  SUPERSEDED
}

type Team @node(paths: ["notes/teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
  role: String @field(source: "role", sourceKind: INLINE)
  team: Team @link(inverse: "members")
  projects: [Project!] @link(inverse: "owner")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
  status: DecisionStatus
}

type ARequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Project
  @node(paths: ["notes/projects/*.md"])
  @semantics(kind: BEHAVIORAL)
  @companionDocs(paths: ["docs/reference/guides/Project workflow.md"], purpose: "workflow")
  @retrieval(intents: ["search"], boost: 1.2) {
  name: String!
  status: ProjectStatus!
  owner: Person! @link(source: "owner", inverse: "projects")
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
  requirements: ARequirementsSection @contains(level: H2, heading: "Requirements")
}
`
	files["notes/projects/roadmap-refresh.md"] = `---
type: Project
name: Roadmap Refresh
status: ACTIVE
owner: notes/people/alice.md
---

Primary decision is [[structured-ontology-decision]].

## Requirements

Decision context also references [[backlink-decision]].
`

	vault := setupAgentTestVault(t, files)

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "validate", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Ontology valid.")
	require.Contains(t, stdout, "Resolved notes by type:")
	require.Contains(t, stdout, "Project (1)")
	require.NotContains(t, stdout, "ARequirementsSection (")
}

func TestOntologyValidateCommandListsEmbeddedNodeInstances(t *testing.T) {
	files := map[string]string{
		".rhizome/ontology/schema.graphql": `
type Story implements Section @node(locator: EMBEDDED) {
  summary: String! @field
  status: String @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`,
		"notes/projects/roadmap-refresh.md": `---
type: Project
name: Roadmap Refresh
---

## Stories

### Validation story
summary:: Validation story
status:: READY
^validation-story
`,
	}

	vault := setupAgentTestVault(t, files)

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "validate", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Resolved notes by type:")
	require.Contains(t, stdout, "Project (1)")
	require.Contains(t, stdout, "Story (1)")
	require.Contains(t, stdout, "notes/projects/roadmap-refresh.md#")
}

func TestOntologyWalkCommandRefreshesStaleStore(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	indexOntologyTestVault(t, vault.path)

	projectPath := filepath.Join(vault.path, "notes", "projects", "roadmap-refresh.md")
	body, err := os.ReadFile(projectPath)
	require.NoError(t, err)
	updated := string(body) + "\nSecondary decision is [[backlink-decision]].\n"
	require.NoError(t, os.WriteFile(projectPath, []byte(updated), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "walk", "--vault", vault.name, "notes/projects/roadmap-refresh.md"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "--decisions")
	require.Contains(t, stdout, "notes/decisions/backlink-decision.md")
}

func indexOntologyTestVault(t *testing.T, vaultPath string) {
	t.Helper()
	store, err := semdb.Open(filepath.Join(vaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	vaultDef := obsidian.VaultDefinition{Path: vaultPath}
	noteMetadata, err := newNoteMetadataIndexer()
	require.NoError(t, err)
	_, err = noteMetadata.EnsureIndexed(context.Background(), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ontology.EnsureIndexed(context.Background(), noteMetadata, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
}
