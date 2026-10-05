package desktop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// fixtureRepository creates a configured main checkout with linked worktrees
// "feature" and "pruned"; the pruned worktree's directory is deleted.
func fixtureRepository(t *testing.T) (main, feature string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to build fixture repositories")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	main = filepath.Join(base, "project")
	require.NoError(t, os.Mkdir(main, 0o755))
	git(t, main, "init")
	require.NoError(t, obsidian.SaveLocalConfig(main, obsidian.LocalConfig{}))
	git(t, main, "add", ".")
	git(t, main, "commit", "-m", "fixture")
	feature = filepath.Join(base, "project-feature")
	git(t, main, "worktree", "add", "-b", "feature", feature)
	pruned := filepath.Join(base, "project-pruned")
	git(t, main, "worktree", "add", "-b", "pruned", pruned)
	require.NoError(t, os.RemoveAll(pruned))
	return main, feature
}

func paths(repo Repository) []string {
	var out []string
	for _, w := range repo.Worktrees {
		out = append(out, w.Path)
	}
	return out
}

func TestRepositoryIdentityIsSharedByEveryWorktree(t *testing.T) {
	s := testService(t)
	main, feature := fixtureRepository(t)
	fromMain, err := s.Repository(main, "")
	require.NoError(t, err)
	require.True(t, fromMain.Git)
	require.Equal(t, "project", fromMain.Name)
	require.Equal(t, filepath.Join(main, ".git"), fromMain.Root)
	require.Equal(t, []string{main, feature}, paths(fromMain))
	require.True(t, fromMain.Worktrees[0].Main)
	require.Equal(t, "main", fromMain.Worktrees[0].Branch)
	require.Equal(t, "feature", fromMain.Worktrees[1].Branch)
	require.True(t, fromMain.Worktrees[1].Configured)
	for _, path := range []string{feature, filepath.Join(feature, ".rhizome"), fromMain.Root} {
		other, err := s.Repository(path, "")
		require.NoError(t, err)
		require.Equal(t, fromMain.ID, other.ID, path)
		require.Equal(t, paths(fromMain), paths(other), path)
	}
}

func TestPrimaryWorktreeFollowsDefaultBranchThenOverride(t *testing.T) {
	s := testService(t)
	main, feature := fixtureRepository(t)
	repo, err := s.Repository(feature, "")
	require.NoError(t, err)
	require.Equal(t, main, repo.Primary)
	originHead := filepath.Join(main, ".git", "refs", "remotes", "origin", "HEAD")
	require.NoError(t, os.MkdirAll(filepath.Dir(originHead), 0o755))
	require.NoError(t, os.WriteFile(originHead, []byte("ref: refs/remotes/origin/feature\n"), 0o644))
	repo, err = s.Repository(main, "")
	require.NoError(t, err)
	require.Equal(t, "feature", repo.DefaultBranch)
	require.Equal(t, feature, repo.DefaultPrimary)
	require.Equal(t, feature, repo.Primary)
	repo, err = s.Repository(main, main)
	require.NoError(t, err)
	require.Equal(t, main, repo.Primary)
	repo, err = s.Repository(main, filepath.Join(t.TempDir(), "gone"))
	require.NoError(t, err)
	require.Equal(t, feature, repo.Primary)
}

func TestRepositoryReportsDatabasesAndPlainFolders(t *testing.T) {
	s := testService(t)
	main, feature := fixtureRepository(t)
	db, _ := databasePath(main)
	require.NoError(t, os.WriteFile(db, []byte("db"), 0o644))
	repo, err := s.Repository(main, "")
	require.NoError(t, err)
	require.True(t, repo.Worktrees[0].HasDatabase)
	require.False(t, repo.Worktrees[1].HasDatabase)
	require.NoError(t, os.RemoveAll(feature))
	repo, err = s.Repository(main, "")
	require.NoError(t, err)
	require.Equal(t, []string{main}, paths(repo))

	folder := config(t, obsidian.LocalRhizomeConfig{})
	plain, err := s.Repository(folder, "")
	require.NoError(t, err)
	require.False(t, plain.Git)
	require.Equal(t, folder, plain.Root)
	require.Equal(t, []string{folder}, paths(plain))
	require.Equal(t, folder, plain.Primary)
	info, err := s.Inspect(folder)
	require.NoError(t, err)
	require.Equal(t, info.ID, plain.ID)
	_, err = s.Repository(filepath.Join(folder, "missing"), "")
	code(t, err, "folder_missing")
}

func TestRepositoryDiscoveryRunsNoExecutable(t *testing.T) {
	s := testService(t)
	main, _ := fixtureRepository(t)
	require.NoError(t, obsidian.SaveLocalConfig(main, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{DevBinaryDir: "dev"}}))
	sentinel := filepath.Join(main, "executed")
	script(t, filepath.Join(main, "dev", runtime.GOOS, repoexec.ExecutableName()), "touch '"+sentinel+"'")
	repo, err := s.Repository(main, "")
	require.NoError(t, err)
	require.True(t, repo.Worktrees[0].TrustRequired)
	require.NoFileExists(t, sentinel)
}

func seedFixture(t *testing.T, body string) (s *Service, main, feature, target string) {
	t.Helper()
	s = testService(t)
	main, feature = fixtureRepository(t)
	db, _ := databasePath(main)
	require.NoError(t, os.WriteFile(db, []byte("primary-db"), 0o644))
	target = script(t, filepath.Join(t.TempDir(), "rzm"), body)
	return s, main, feature, target
}

func TestSeedCopiesWithWorktreeExecutable(t *testing.T) {
	s, main, feature, target := seedFixture(t, `[ "$1 $2" = "new-worktree --help" ] && exit 0
[ "$1" = new-worktree ] || exit 3
cp "$2/.rhizome/db.sqlite" .rhizome/db.sqlite`)
	result, err := s.Seed(context.Background(), Request{Folder: feature, Primary: main, GlobalExecutable: target})
	require.NoError(t, err)
	require.True(t, result.Seeded)
	db, exists := databasePath(feature)
	require.True(t, exists)
	data, err := os.ReadFile(db)
	require.NoError(t, err)
	require.Equal(t, "primary-db", string(data))
}

func TestSeedSkipsWithoutExecuting(t *testing.T) {
	s, main, feature, target := seedFixture(t, "exit 9")
	result, err := s.Seed(context.Background(), Request{Folder: main, Primary: main, GlobalExecutable: target})
	require.NoError(t, err)
	require.Equal(t, SeedResult{Reason: "primary"}, result)

	primaryDB, _ := databasePath(main)
	require.NoError(t, os.Remove(primaryDB))
	result, err = s.Seed(context.Background(), Request{Folder: feature, Primary: main, GlobalExecutable: target})
	require.NoError(t, err)
	require.Equal(t, SeedResult{Reason: "primary_unindexed"}, result)

	require.NoError(t, os.WriteFile(primaryDB, []byte("db"), 0o644))
	featureDB, _ := databasePath(feature)
	require.NoError(t, os.WriteFile(featureDB, []byte("db"), 0o644))
	result, err = s.Seed(context.Background(), Request{Folder: feature, Primary: main, GlobalExecutable: target})
	require.NoError(t, err)
	require.Equal(t, SeedResult{Reason: "exists"}, result)
}

func TestSeedReportsUnsupportedAndFailedCopies(t *testing.T) {
	s, main, feature, unsupported := seedFixture(t, "exit 1")
	_, err := s.Seed(context.Background(), Request{Folder: feature, Primary: main, GlobalExecutable: unsupported})
	code(t, err, "seed_unsupported")

	failing := script(t, filepath.Join(t.TempDir(), "rzm"), `[ "$2" = --help ] && exit 0
echo "database is locked" >&2
exit 1`)
	_, err = s.Seed(context.Background(), Request{Folder: feature, Primary: main, GlobalExecutable: failing})
	code(t, err, "seed_error")
	require.ErrorContains(t, err, "database is locked")
	_, exists := databasePath(feature)
	require.False(t, exists)
}

func gitBase(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to build fixture repositories")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return base
}

func TestSeparateGitDirKeepsItsMainCheckout(t *testing.T) {
	s := testService(t)
	base := gitBase(t)
	store, work, linked := filepath.Join(base, "store.git"), filepath.Join(base, "work"), filepath.Join(base, "work-linked")
	git(t, base, "init", "--separate-git-dir", store, work)
	require.NoError(t, obsidian.SaveLocalConfig(work, obsidian.LocalConfig{}))
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "fixture")
	git(t, work, "worktree", "add", "-b", "linked", linked)

	repo, err := s.Repository(work, "")
	require.NoError(t, err)
	require.Equal(t, []string{work, linked}, paths(repo))
	require.True(t, repo.Worktrees[0].Main)
	require.Equal(t, "work", repo.Name)
	require.Equal(t, work, repo.Root, "discovery restarts from the checkout that names the Git directory")
	again, err := s.Repository(repo.Root, "")
	require.NoError(t, err)
	require.Equal(t, paths(repo), paths(again))

	// Git records nothing a linked worktree could use to find this main
	// checkout, until core.worktree names it.
	fromLinked, err := s.Repository(linked, "")
	require.NoError(t, err)
	require.Equal(t, repo.ID, fromLinked.ID)
	require.Equal(t, []string{linked}, paths(fromLinked))
	git(t, store, "config", "core.worktree", work)
	for _, path := range []string{linked, store} {
		found, err := s.Repository(path, "")
		require.NoError(t, err)
		require.Equal(t, repo.ID, found.ID, path)
		require.Equal(t, []string{work, linked}, paths(found), path)
	}
}

func TestLockedMissingWorktreeStaysListedAsUnavailable(t *testing.T) {
	s := testService(t)
	main, feature := fixtureRepository(t)
	locked := filepath.Join(filepath.Dir(main), "project-locked")
	git(t, main, "worktree", "add", "-b", "locked", locked)
	git(t, main, "worktree", "lock", locked)
	require.NoError(t, os.RemoveAll(locked))

	repo, err := s.Repository(main, "")
	require.NoError(t, err)
	require.Equal(t, []string{main, feature, locked}, paths(repo), "the unlocked missing worktree is pruned")
	missing := repo.Worktrees[2]
	require.Equal(t, "locked", missing.Branch)
	require.Contains(t, missing.Error, "missing")
	require.False(t, missing.Configured)
	repo, err = s.Repository(main, locked)
	require.NoError(t, err)
	require.Equal(t, main, repo.Primary, "an unavailable worktree cannot be primary")
}

func TestVaultBelowTheGitRootIsARepositoryOfItsSubfolder(t *testing.T) {
	s := testService(t)
	base := gitBase(t)
	main := filepath.Join(base, "project")
	require.NoError(t, os.Mkdir(main, 0o755))
	git(t, main, "init")
	for _, vault := range []string{"docs", "notes"} {
		require.NoError(t, os.Mkdir(filepath.Join(main, vault), 0o755))
		require.NoError(t, obsidian.SaveLocalConfig(filepath.Join(main, vault), obsidian.LocalConfig{}))
	}
	git(t, main, "add", ".")
	git(t, main, "commit", "-m", "fixture")
	feature, bare := filepath.Join(base, "project-feature"), filepath.Join(base, "project-old")
	git(t, main, "worktree", "add", "-b", "feature", feature)
	git(t, main, "worktree", "add", "-b", "bare", bare)
	require.NoError(t, os.RemoveAll(filepath.Join(bare, "docs")))

	docs, err := s.Repository(filepath.Join(main, "docs"), "")
	require.NoError(t, err)
	require.True(t, docs.Git)
	require.Equal(t, "project/docs", docs.Name)
	require.Equal(t, "docs", docs.Subpath)
	require.Equal(t, filepath.Join(main, "docs"), docs.Root)
	require.Equal(t, []string{filepath.Join(main, "docs"), filepath.Join(feature, "docs"), filepath.Join(bare, "docs")}, paths(docs))
	require.True(t, docs.Worktrees[1].Configured)
	require.Contains(t, docs.Worktrees[2].Error, "no docs folder")

	fromFeature, err := s.Repository(filepath.Join(feature, "docs", ".rhizome"), "")
	require.NoError(t, err)
	require.Equal(t, docs.ID, fromFeature.ID)
	require.Equal(t, paths(docs), paths(fromFeature))

	notes, err := s.Repository(filepath.Join(main, "notes"), "")
	require.NoError(t, err)
	whole, err := s.Repository(main, "")
	require.NoError(t, err)
	require.NotEqual(t, docs.ID, notes.ID)
	require.NotEqual(t, docs.ID, whole.ID)
	require.Equal(t, []string{main, feature, bare}, paths(whole))
}

func TestSubfolderRepositoryRootLeadsBackToItsIdentity(t *testing.T) {
	s := testService(t)
	base := gitBase(t)
	main := filepath.Join(base, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(main, "docs"), 0o755))
	git(t, main, "init")
	require.NoError(t, os.WriteFile(filepath.Join(main, "docs", "index.md"), []byte("# Docs\n"), 0o644))
	git(t, main, "add", ".")
	git(t, main, "commit", "-m", "fixture")
	feature, bare := filepath.Join(base, "project-feature"), filepath.Join(base, "project-old")
	git(t, main, "worktree", "add", "-b", "feature", feature)
	git(t, main, "worktree", "add", "-b", "bare", bare)
	require.NoError(t, os.RemoveAll(filepath.Join(bare, "docs")))
	require.NoError(t, obsidian.SaveLocalConfig(filepath.Join(feature, "docs"), obsidian.LocalConfig{}))

	docs, err := s.Repository(filepath.Join(feature, "docs"), "")
	require.NoError(t, err)
	require.Equal(t, "docs", docs.Subpath)
	require.Equal(t, []string{filepath.Join(main, "docs"), filepath.Join(feature, "docs"), filepath.Join(bare, "docs")}, paths(docs))
	require.False(t, docs.Worktrees[0].Configured)
	require.Contains(t, docs.Worktrees[2].Error, "no docs folder")
	require.Equal(t, filepath.Join(feature, "docs"), docs.Root, "the unconfigured main docs folder would resolve to the whole repository")

	again, err := s.Repository(docs.Root, docs.Primary)
	require.NoError(t, err)
	require.Equal(t, docs.ID, again.ID)
	require.Equal(t, docs.Root, again.Root)
	require.Equal(t, paths(docs), paths(again))
}
