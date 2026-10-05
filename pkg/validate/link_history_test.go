package validate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// NUL-separated fields keep names that git would otherwise quote, and a file
// named like a header is still read as a path.
func TestParseLinkHistoryReadsFieldsByPosition(t *testing.T) {
	out := "commit aaa\x00\nD\x00Notes/Say \"hi\".md\x00D\x00commit b.md\x00R090\x00Old\tName.md\x00New Name.md\x00" +
		"commit bbb\x00\nR100\x00Old\tName.md\x00Older.md\x00"
	history := linkHistory{removed: map[string]struct{}{}, renamedTo: map[string]renameEvent{}}

	parseLinkHistory([]byte(out), &history)

	for _, target := range []string{`Say "hi"`, "commit b", "Old\tName"} {
		require.True(t, history.broke(target), target)
	}
	require.False(t, history.broke("New Name"))
	require.Equal(t, renameEvent{to: "New Name.md", commit: "aaa"}, history.renamedTo["old\tname"], "the newest rename wins")
}

func TestLoadLinkHistoryExplainsUnavailableHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	plain := t.TempDir()
	require.Equal(t, "the vault is not a git repository", loadLinkHistory(context.Background(), plain).unavailable)

	empty := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", empty).Run())
	require.Equal(t, "the git repository has no commits", loadLinkHistory(context.Background(), empty).unavailable)

	partial := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", partial).Run())
	require.NoError(t, writeFixtureFiles(partial, map[string]string{"a.md": "# A\n"}))
	gitCommitAll(t, partial, "initial")
	require.NoError(t, exec.Command("git", "-C", partial, "config", "extensions.partialClone", "origin").Run())
	history := loadLinkHistory(context.Background(), partial)
	require.False(t, history.available)
	require.Contains(t, history.unavailable, "partial")
}

// One suite run selecting both link checks splits unresolved links by git
// history: the deleted note broke, the never-created one is a placeholder.
func TestRunSuiteOnceSplitsBrokenAndPlaceholderLinks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	require.NoError(t, writeFixtureFiles(root, map[string]string{"Doomed.md": "# Doomed\n", "Source.md": "[[Doomed]] [[Never Existed]]\n"}))
	gitCommitAll(t, root, "initial")
	require.NoError(t, os.Remove(filepath.Join(root, "Doomed.md")))
	gitCommitAll(t, root, "delete")
	runCtx := brokenLinkRunContext(root)
	runCtx.NoteMetadata = testNoteMetadata(t)

	result, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckBrokenLinks, CheckPlaceholderLinks}, RunContext: &runCtx,
	})
	require.NoError(t, err)

	byName := map[string]CheckResult{}
	for _, check := range result.Checks {
		byName[check.Name] = check
	}
	require.Empty(t, byName[CheckBrokenLinks].Error)
	require.Empty(t, byName[CheckPlaceholderLinks].Error)
	require.Equal(t, []string{"Doomed"}, sortedIssueTargets(byName[CheckBrokenLinks].Issues))
	require.Equal(t, []string{"Never Existed"}, sortedIssueTargets(byName[CheckPlaceholderLinks].Issues))
}

// The shared scan memoizes its first result: a link added after it is not
// seen by a later check in the same run, while a fresh run rescans.
func TestSharedUnresolvedLinkScanMemoizesFirstScan(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{"Source.md": "[[First Missing]]\n"}))
	runCtx := brokenLinkRunContext(root)
	runCtx.unresolvedLinks = &sharedUnresolvedLinkScan{}

	first := RunBrokenLinks(runCtx, Options{})
	require.NoError(t, os.WriteFile(filepath.Join(root, "Later.md"), []byte("[[Second Missing]]\n"), 0o644))
	second := RunBrokenLinks(runCtx, Options{})

	require.Equal(t, 1, first.IssueCount)
	require.Equal(t, first.IssueCount, second.IssueCount)
	require.False(t, strings.Contains(second.Issues[0].Target, "Second"))
	require.Equal(t, 2, RunBrokenLinks(brokenLinkRunContext(root), Options{}).IssueCount, "a fresh run rescans")
}

// A note deleted from the working tree but not yet committed still broke the
// links to it, so a pre-commit validation run must report them.
func TestBrokenLinksCountUncommittedDeletions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	require.NoError(t, writeFixtureFiles(root, map[string]string{"Doomed.md": "# Doomed\n", "Source.md": "[[Doomed]] [[Never Existed]]\n"}))
	gitCommitAll(t, root, "initial")
	require.NoError(t, os.Remove(filepath.Join(root, "Doomed.md")))

	result := RunBrokenLinks(brokenLinkRunContext(root), Options{})

	require.Equal(t, []string{"Doomed"}, sortedIssueTargets(result.Issues))
}

func TestLinkPredatesRenameRequiresAnActualMatchingLink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	cases := []struct {
		name, content string
		want          bool
	}{
		{"exact", "[[Old]]", true},
		{"alias", "[[old|Label]]", true},
		{"embed", "![[Old]]", true},
		{"heading", "[[Old#Heading]]", true},
		{"block", "[[Old#^block-id]]", true},
		{"prefix", "[[Older]]", false},
		{"inline", "`[[Old]]`", false},
		{"fenced", "```md\n[[Old]]\n```", false},
	}
	files := map[string]string{"Old.md": "# Original\n"}
	for _, tc := range cases {
		files[tc.name+".md"] = tc.content
	}
	require.NoError(t, writeFixtureFiles(root, files))
	gitCommitAll(t, root, "initial")
	require.NoError(t, exec.Command("git", "-C", root, "mv", "Old.md", "New.md").Run())
	gitCommitAll(t, root, "rename")
	history := loadLinkHistory(context.Background(), root)
	_, commit, ok := history.renameDestination("Old", func(p string) bool { return p == "New.md" })
	require.True(t, ok)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			linkedBefore, err := history.linkPredatesRename(context.Background(), testNoteMetadata(t), commit, tc.name+".md", "Old")
			require.NoError(t, err)
			require.Equal(t, tc.want, linkedBefore)
		})
	}
}
