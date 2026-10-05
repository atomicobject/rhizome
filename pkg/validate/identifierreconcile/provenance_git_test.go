package identifierreconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestGitProvenanceUsesIntroductionAuthorDateForKeeperEvidence(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")

	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n# A\n")
	aOID := repo.commit(t, "2026-07-15T12:00:00Z", "add a")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n# B\n")
	bOID := repo.commit(t, "2026-07-15T10:00:00Z", "add b")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{a, b})
	require.NoError(t, err)
	require.True(t, result.Evidence[a.ID()].Complete, "%+v", result.Evidence[a.ID()])
	require.True(t, result.Evidence[b.ID()].Complete, "%+v", result.Evidence[b.ID()])
	require.Equal(t, aOID, result.Evidence[a.ID()].FullOID)
	require.Equal(t, bOID, result.Evidence[b.ID()].FullOID)
	require.Equal(t, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), result.Evidence[a.ID()].AuthorDate)
	require.Equal(t, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC), result.Evidence[b.ID()].AuthorDate)

	inventory, err := BuildInventory([]Claim{a, b})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, result.Evidence)
	require.NoError(t, err)
	require.Equal(t, "b.md", plan.Collisions[0].Keeper.Node.NotePath)
}

func TestGitProvenanceRejectsFutureProviderClaimBeforeReadingHistoricalBlob(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "future.html")
	repo.write(t, "future.html", "<h1>Future</h1>")
	repo.commit(t, "2026-07-15T12:00:00Z", "add future provider note")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{claimant})

	require.NoError(t, err)
	require.False(t, result.Evidence[claimant.ID()].Complete)
	require.Equal(t, "identifier provenance supports historical Markdown claims only", result.Evidence[claimant.ID()].Reason)
	require.Zero(t, result.Stats.HistoricalBlobsParsed)
}

func TestGitProvenanceFormattingAndBodyEditsDoNotResetIntroduction(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\n---\n# Initial\n")
	introduced := repo.commit(t, "2026-07-15T09:00:00Z", "introduce")
	repo.write(t, "spec.md", "---\nid: \"spec-0001\"\n---\n# Reformatted\n\nBody changed.\n")
	repo.commit(t, "2026-07-15T11:00:00Z", "format and edit")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.True(t, result.Evidence[claimant.ID()].Complete)
	require.Equal(t, introduced, result.Evidence[claimant.ID()].FullOID)
}

func TestGitProvenanceUsesFullOIDThenCanonicalKeyForRealHistoryTies(t *testing.T) {
	t.Run("equal timestamp uses full OID", func(t *testing.T) {
		repo := newGitFixture(t)
		pool := mustPool(t, "SPEC")
		a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
		b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
		repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
		aOID := repo.commit(t, "2026-07-15T09:00:00Z", "add a")
		repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
		bOID := repo.commit(t, "2026-07-15T09:00:00Z", "add b")

		result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{a, b})
		require.NoError(t, err)
		require.True(t, result.Evidence[a.ID()].Complete)
		require.True(t, result.Evidence[b.ID()].Complete)
		require.Equal(t, result.Evidence[a.ID()].AuthorDate, result.Evidence[b.ID()].AuthorDate, "the fixture must be a date tie")
		require.Equal(t, aOID, result.Evidence[a.ID()].FullOID)
		require.Equal(t, bOID, result.Evidence[b.ID()].FullOID)
		inventory, err := BuildInventory([]Claim{a, b})
		require.NoError(t, err)
		plan, err := BuildPlan(inventory, result.Evidence)
		require.NoError(t, err)
		require.Equal(t, KeeperByGit, plan.Collisions[0].KeeperBasis)
		want := "a.md"
		if bOID < aOID {
			want = "b.md"
		}
		require.Equal(t, want, plan.Collisions[0].Keeper.Node.NotePath)
	})

	t.Run("same commit uses canonical key", func(t *testing.T) {
		repo := newGitFixture(t)
		pool := mustPool(t, "SPEC")
		a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
		b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
		repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
		repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
		oid := repo.commit(t, "2026-07-15T09:00:00Z", "add both")

		result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{b, a})
		require.NoError(t, err)
		require.Equal(t, oid, result.Evidence[a.ID()].FullOID)
		require.Equal(t, oid, result.Evidence[b.ID()].FullOID, "the fixture must be a full-OID tie")
		inventory, err := BuildInventory([]Claim{b, a})
		require.NoError(t, err)
		plan, err := BuildPlan(inventory, result.Evidence)
		require.NoError(t, err)
		require.Equal(t, KeeperByGit, plan.Collisions[0].KeeperBasis)
		require.Equal(t, "a.md", plan.Collisions[0].Keeper.Node.NotePath)
	})
}

func TestGitProvenanceUsesSchemaSourcesAndEmbeddedIdentifierClaims(t *testing.T) {
	repo := newGitFixture(t)
	repo.writeIdentifierSchema(t)
	pool := mustPool(t, "SPEC")
	noteKey, err := NewCanonicalNodeKey("spec.md", "", "Spec", "specId")
	require.NoError(t, err)
	embeddedKey, err := NewCanonicalNodeKey("spec.md", "^SPEC-0001-US1", "UserStory", "id")
	require.NoError(t, err)
	noteClaim := Claim{Node: noteKey, Pool: pool, Value: "SPEC-0001", Kind: ClaimPreferred}
	embeddedClaim := Claim{Node: embeddedKey, Pool: pool, Value: "SPEC-0001-US1", Kind: ClaimPreferred}
	repo.write(t, "spec.md", "---\nidentifier: SPEC-0001\n---\n# Spec\n\n## Stories\n\n### First\n\n- id:: SPEC-0001-US1\n")
	introduced := repo.commit(t, "2026-07-15T09:00:00Z", "add identifiers")
	repo.write(t, "spec.md", "---\nidentifier: \"SPEC-0001\"\n---\n# Spec\n\n## Stories\n\n### First\n\n- id::   ^SPEC-0001-US1\n\nBody.\n")
	repo.commit(t, "2026-07-15T10:00:00Z", "reformat identifiers")

	schema, err := ontology.LoadSchema(repo.dir)
	require.NoError(t, err)
	result, err := ResolveGitProvenanceWithSchema(context.Background(), repo.dir, schema, []Claim{noteClaim, embeddedClaim})
	require.NoError(t, err)
	require.True(t, result.Evidence[noteClaim.ID()].Complete, "%+v", result.Evidence[noteClaim.ID()])
	require.True(t, result.Evidence[embeddedClaim.ID()].Complete, "%+v", result.Evidence[embeddedClaim.ID()])
	require.Equal(t, introduced, result.Evidence[noteClaim.ID()].FullOID)
	require.Equal(t, introduced, result.Evidence[embeddedClaim.ID()].FullOID)
}

func TestGitProvenanceStartsAtHistoricalNoteTypeTransition(t *testing.T) {
	repo := newGitFixture(t)
	repo.writeTypeTransitionSchema(t)
	pool := mustPool(t, "SPEC")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
	repo.write(t, "spec.md", "---\ntype: Other\nid: SPEC-0001\n---\n# Other\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "add other note")
	repo.write(t, "spec.md", "---\ntype: Spec\nid: SPEC-0001\n---\n# Spec\n")
	transition := repo.commit(t, "2026-07-15T09:00:00Z", "make spec")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.True(t, result.Evidence[claimant.ID()].Complete, "%+v", result.Evidence[claimant.ID()])
	require.Equal(t, transition, result.Evidence[claimant.ID()].FullOID)
}

func TestGitProvenanceUsesHistoricalPathForTypeAcrossRename(t *testing.T) {
	repo := newGitFixture(t)
	repo.writePathTransitionSchema(t)
	pool := mustPool(t, "SPEC")
	repo.write(t, "drafts/a.md", "---\nid: SPEC-0001\n---\n# Draft\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "add draft")
	require.NoError(t, os.MkdirAll(filepath.Join(repo.dir, "specs"), 0o755))
	repo.git(t, nil, "mv", "drafts/a.md", "specs/a.md")
	transition := repo.commit(t, "2026-07-15T09:00:00Z", "promote spec")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "specs/a.md")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.True(t, result.Evidence[claimant.ID()].Complete, "%+v", result.Evidence[claimant.ID()])
	require.Equal(t, transition, result.Evidence[claimant.ID()].FullOID)
}

func TestGitProvenanceFallsBackForDirtySemanticClaim(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add spec")
	repo.write(t, "spec.md", "---\nid: SPEC-0002\n---\n")
	dirty := claim(t, pool, "SPEC-0002", ClaimPreferred, "spec.md")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{dirty})
	require.NoError(t, err)
	require.False(t, result.Evidence[dirty.ID()].Complete)
	require.Contains(t, result.Evidence[dirty.ID()].Reason, "not committed")
}

func TestGitProvenanceTracesRealAliasMembershipAndReintroduction(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	alias := claim(t, pool, "LEGACY-1", ClaimAlias, "spec.md")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: [LEGACY-1]\n---\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "introduce alias")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: []\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "remove alias")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: [LEGACY-1]\n---\n")
	reintroduced := repo.commit(t, "2026-07-15T10:00:00Z", "reintroduce alias")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{alias})
	require.NoError(t, err)
	require.True(t, result.Evidence[alias.ID()].Complete, "%+v", result.Evidence[alias.ID()])
	require.Equal(t, reintroduced, result.Evidence[alias.ID()].FullOID)
}

func TestGitProvenanceFallsBackWhenIntermediateHistoryIsMalformed(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "introduce")
	repo.write(t, "spec.md", "---\nid: [unterminated\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "malform frontmatter")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T10:00:00Z", "restore")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.False(t, result.Evidence[claimant.ID()].Complete)
	require.Contains(t, result.Evidence[claimant.ID()].Reason, "malformed")
}

func TestGitProvenanceFallsBackForCaseAmbiguousAliasField(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	alias := claim(t, pool, "LEGACY-1", ClaimAlias, "spec.md")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: [LEGACY-1]\n---\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "introduce")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: [LEGACY-1]\nAliases: [OTHER]\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "make aliases ambiguous")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: [LEGACY-1]\n---\n")
	repo.commit(t, "2026-07-15T10:00:00Z", "restore")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{alias})
	require.NoError(t, err)
	require.False(t, result.Evidence[alias.ID()].Complete)
	require.Contains(t, result.Evidence[alias.ID()].Reason, "ambiguous")
}

func TestGitProvenanceFollowsGitMove(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	repo.write(t, "old.md", "---\nid: SPEC-0001\n---\n# Original\n")
	introduced := repo.commit(t, "2026-07-15T09:00:00Z", "introduce")
	repo.git(t, nil, "mv", "old.md", "renamed.md")
	repo.commit(t, "2026-07-15T10:00:00Z", "rename")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "renamed.md")

	result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.True(t, result.Evidence[claimant.ID()].Complete)
	require.Equal(t, introduced, result.Evidence[claimant.ID()].FullOID)
}

func TestGitProvenanceFallsBackForUntrackedShallowAndNoRepository(t *testing.T) {
	pool := mustPool(t, "SPEC")
	t.Run("untracked", func(t *testing.T) {
		repo := newGitFixture(t)
		repo.write(t, "tracked.md", "---\nid: SPEC-0001\n---\n")
		repo.commit(t, "2026-07-15T09:00:00Z", "tracked")
		repo.write(t, "untracked.md", "---\nid: SPEC-0001\n---\n")
		tracked := claim(t, pool, "SPEC-0001", ClaimPreferred, "tracked.md")
		untracked := claim(t, pool, "SPEC-0001", ClaimPreferred, "untracked.md")
		result, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{tracked, untracked})
		require.NoError(t, err)
		require.True(t, result.Evidence[tracked.ID()].Complete)
		require.False(t, result.Evidence[untracked.ID()].Complete)
		require.Contains(t, result.Evidence[untracked.ID()].Reason, "untracked")
	})

	t.Run("shallow", func(t *testing.T) {
		source := newGitFixture(t)
		source.write(t, "spec.md", "---\nid: SPEC-0001\n---\n# One\n")
		source.commit(t, "2026-07-15T09:00:00Z", "one")
		source.write(t, "spec.md", "---\nid: SPEC-0001\n---\n# Two\n")
		source.commit(t, "2026-07-15T10:00:00Z", "two")
		clone := filepath.Join(t.TempDir(), "clone")
		cmd := exec.Command("git", "clone", "--depth", "1", "file://"+source.dir, clone)
		require.NoError(t, cmd.Run())
		claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
		result, err := GitProvenanceResolver{}.Resolve(context.Background(), clone, []Claim{claimant})
		require.NoError(t, err)
		require.False(t, result.Evidence[claimant.ID()].Complete)
		require.Contains(t, result.Evidence[claimant.ID()].Reason, "shallow")
	})

	t.Run("no repository", func(t *testing.T) {
		dir := t.TempDir()
		claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
		result, err := GitProvenanceResolver{}.Resolve(context.Background(), dir, []Claim{claimant})
		require.NoError(t, err)
		require.False(t, result.Evidence[claimant.ID()].Complete)
		require.Contains(t, result.Evidence[claimant.ID()].Reason, "repository")
	})
}

func TestGitProvenanceBatchesSamePathAndIgnoresFilesystemMtime(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	preferred := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
	alias := claim(t, pool, "SPEC-0001", ClaimAlias, "spec.md")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "introduce")

	before, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{preferred, alias})
	require.NoError(t, err)
	require.Equal(t, 1, before.Stats.UniquePaths)
	require.LessOrEqual(t, before.Stats.Commands, before.Stats.UniquePaths+4)
	require.Equal(t, 1, before.Stats.HistoricalBlobsParsed)
	require.Equal(t, before.Evidence[preferred.ID()], before.Evidence[alias.ID()])

	path := filepath.Join(repo.dir, "spec.md")
	require.NoError(t, os.Chtimes(path, time.Unix(1, 0), time.Unix(2_000_000_000, 0)))
	after, err := GitProvenanceResolver{}.Resolve(context.Background(), repo.dir, []Claim{preferred, alias})
	require.NoError(t, err)
	require.Equal(t, before.Evidence, after.Evidence)
}

func TestGitProvenanceNeverInvokesFetch(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "introduce")
	runner := &recordingGitRunner{delegate: execGitCommandRunner{}}
	resolver := GitProvenanceResolver{runner: runner}

	result, err := resolver.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.True(t, result.Evidence[claimant.ID()].Complete)
	for _, args := range runner.calls {
		require.NotContains(t, args, "fetch")
	}
	for _, env := range runner.envs {
		require.Contains(t, env, "GIT_NO_LAZY_FETCH=1")
		require.Contains(t, env, "LC_ALL=C")
	}
}

type gitFixture struct{ dir string }

func newGitFixture(t *testing.T) gitFixture {
	t.Helper()
	repo := gitFixture{dir: t.TempDir()}
	repo.git(t, nil, "init", "-q")
	repo.git(t, nil, "config", "user.name", "Rhizome Test")
	repo.git(t, nil, "config", "user.email", "rhizome@example.com")
	repo.writeIdentifierSchema(t)
	return repo
}

func (r gitFixture) write(t *testing.T, path, content string) {
	t.Helper()
	abs := filepath.Join(r.dir, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

func (r gitFixture) writeIdentifierSchema(t *testing.T) {
	t.Helper()
	r.write(t, ".rhizome/ontology/identifiers.graphql", `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
}

type StorySection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["*.md"]) {
  specId: String! @field(sources: ["id", "identifier"]) @identifier(preferred: true, prefix: "SPEC")
  stories: StorySection @contains(level: H2, heading: "Stories")
}
`)
}

func (r gitFixture) writeTypeTransitionSchema(t *testing.T) {
	t.Helper()
	r.write(t, ".rhizome/ontology/identifiers.graphql", `
type Spec @node(matches: ["type:Spec"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
}

type Other @node(matches: ["type:Other"]) {
  id: String! @field
}
`)
}

func (r gitFixture) writePathTransitionSchema(t *testing.T) {
	t.Helper()
	r.write(t, ".rhizome/ontology/identifiers.graphql", `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
}

type Other @node(paths: ["drafts/*.md"]) {
  id: String! @field
}
`)
}

func (r gitFixture) commit(t *testing.T, date, message string) string {
	t.Helper()
	r.git(t, nil, "add", "-A")
	env := append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	r.git(t, env, "commit", "-q", "-m", message)
	return r.git(t, nil, "rev-parse", "HEAD")
}

func (r gitFixture) git(t *testing.T, env []string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", r.dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return string(bytesTrimSpace(out))
}

func bytesTrimSpace(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\n' || value[start] == '\r' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\n' || value[end-1] == '\r' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}
