package identifierreconcile

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestGitCommandEnvironmentOverridesLazyFetchDeterministically(t *testing.T) {
	env := gitCommandEnvironment(
		[]string{"PATH=/bin", "GIT_NO_LAZY_FETCH=0", "git_no_lazy_fetch=false", "LC_ALL=fr_FR"},
		[]string{"GIT_NO_LAZY_FETCH=1", "LC_ALL=C"},
	)
	require.Equal(t, []string{"PATH=/bin", "GIT_NO_LAZY_FETCH=1", "LC_ALL=C"}, env)
}

func TestGitProvenanceIsolatesBlobLimitsToAffectedCollision(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claims := []Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "c.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "d.md"),
	}
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n# "+strings.Repeat("x", 256)+"\n")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
	repo.write(t, "c.md", "---\nid: SPEC-0002\n---\n")
	repo.write(t, "d.md", "---\nid: SPEC-0002\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add collision fixtures")
	schema, err := ontology.LoadSchema(repo.dir)
	require.NoError(t, err)
	resolver := GitProvenanceResolver{runner: execGitCommandRunner{}, schema: schema, maxBlobBytes: 128, maxBatchBytes: 512}

	result, err := resolver.Resolve(context.Background(), repo.dir, claims)
	require.NoError(t, err)
	require.False(t, result.Evidence[claims[0].ID()].Complete)
	for _, claimant := range claims[1:] {
		require.True(t, result.Evidence[claimant.ID()].Complete, "%s: %+v", claimant.Node.NotePath, result.Evidence[claimant.ID()])
	}
	require.LessOrEqual(t, result.Stats.Commands, 3*result.Stats.UniquePaths+2)

	inventory, err := BuildInventory(claims)
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, result.Evidence)
	require.NoError(t, err)
	require.Equal(t, KeeperByCanonicalKey, plan.Collisions[0].KeeperBasis)
	require.Equal(t, KeeperByGit, plan.Collisions[1].KeeperBasis)
}

func TestGitProvenancePinsRenameDetectionAndLiteralPathspecs(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "[magic].md")
	repo.write(t, "[magic].md", "---\nid: SPEC-0001\n---\n")
	repo.write(t, "m.md", "# Git glob control\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add literal path")
	globMatches := strings.Split(repo.git(t, nil, "--glob-pathspecs", "ls-files", "--", "[magic].md"), "\n")
	require.Contains(t, globMatches, "m.md",
		"the Windows-legal fixture must retain Git glob semantics when literal handling is absent")
	require.Equal(t, "[magic].md", repo.git(t, nil, "--literal-pathspecs", "ls-files", "--", "[magic].md"))
	runner := &recordingGitRunner{delegate: execGitCommandRunner{}}
	resolver := GitProvenanceResolver{runner: runner}

	result, err := resolver.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.True(t, result.Evidence[claimant.ID()].Complete, "%+v", result.Evidence[claimant.ID()])
	joined := make([]string, 0, len(runner.calls))
	for _, args := range runner.calls {
		joined = append(joined, strings.Join(args, " "))
	}
	require.Contains(t, strings.Join(joined, "\n"), "--literal-pathspecs ls-files")
	require.Contains(t, strings.Join(joined, "\n"), "diff.renameLimit=10000")
	require.Contains(t, strings.Join(joined, "\n"), "--find-renames=50%")
}

func TestGitProvenanceIsolatesSkippedRenameDetectionToPath(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0002", ClaimPreferred, "b.md")
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	repo.write(t, "b.md", "---\nid: SPEC-0002\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add notes")

	for _, tt := range []struct {
		name, stderr string
		skipped      bool
	}{
		{name: "exhaustive", stderr: "warning: exhaustive rename detection was skipped due to too many files", skipped: true},
		{name: "inexact", stderr: "warning: inexact rename detection was skipped", skipped: true},
		{name: "rename limit hint", stderr: "you may want to set your diff.renameLimit variable", skipped: true},
		{name: "ordinary stderr", stderr: "warning: unrelated advisory message", skipped: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := GitProvenanceResolver{runner: warningGitRunner{delegate: execGitCommandRunner{}, path: "a.md", warning: tt.stderr}}
			result, err := resolver.Resolve(context.Background(), repo.dir, []Claim{a, b})
			require.NoError(t, err)
			require.True(t, result.Evidence[b.ID()].Complete, "an unaffected claimant keeps complete evidence: %+v", result.Evidence[b.ID()])
			if !tt.skipped {
				require.True(t, result.Evidence[a.ID()].Complete, "ordinary stderr must not mark history incomplete: %+v", result.Evidence[a.ID()])
				return
			}
			require.False(t, result.Evidence[a.ID()].Complete)
			require.Equal(t, "git rename detection limit reached", result.Evidence[a.ID()].Reason)
		})
	}
}

func TestGitProvenancePreflightsBlobLimitsBeforeReadingBatch(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claimant := claim(t, pool, "SPEC-0001", ClaimPreferred, "spec.md")
	repo.write(t, "spec.md", "---\nid: SPEC-0001\n---\n# "+strings.Repeat("x", 256)+"\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "introduce")
	runner := &recordingGitRunner{delegate: execGitCommandRunner{}}
	resolver := GitProvenanceResolver{runner: runner, maxBlobBytes: 64, maxBatchBytes: 128}

	result, err := resolver.Resolve(context.Background(), repo.dir, []Claim{claimant})
	require.NoError(t, err)
	require.False(t, result.Evidence[claimant.ID()].Complete)
	require.Contains(t, result.Evidence[claimant.ID()].Reason, "exceeds limit")
	for _, args := range runner.calls {
		require.NotEqual(t, []string{"cat-file", "--batch"}, args)
	}
}

type recordingGitRunner struct {
	delegate gitCommandRunner
	calls    [][]string
	envs     [][]string
}

func (r *recordingGitRunner) Run(ctx context.Context, dir string, stdin []byte, env []string, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	r.envs = append(r.envs, append([]string(nil), env...))
	return r.delegate.Run(ctx, dir, stdin, env, args...)
}

type warningGitRunner struct {
	delegate gitCommandRunner
	path     string
	warning  string
}

func (r warningGitRunner) Run(ctx context.Context, dir string, stdin []byte, env []string, args ...string) ([]byte, []byte, error) {
	stdout, stderr, err := r.delegate.Run(ctx, dir, stdin, env, args...)
	if slices.Contains(args, "log") && len(args) > 0 && args[len(args)-1] == r.path {
		stderr = append(stderr, []byte(r.warning+"\n")...)
	}
	return stdout, stderr, err
}
