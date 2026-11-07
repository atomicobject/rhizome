package init

import (
	"os"
	"path/filepath"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

var commandCleanupDirs = []string{
	".claude/commands", ".claude/prompts", ".codex/commands", ".codex/prompts", ".cursor/commands",
}

func staleCommandRepo(t *testing.T) (string, vaultconfig.Env) {
	t.Helper()
	root := firstRunRepo(t)
	env := initTestEnv(t)
	_, err := runInit(t, RunOptions{Env: env, Dir: root, Workflow: "none", Addons: "none", Agents: "claude,codex,cursor", Search: "off"})
	require.NoError(t, err)
	for _, dir := range commandCleanupDirs {
		writeTestFile(t, root, dir+"/rhizome-my-custom.md", "custom command\n")
		writeTestFile(t, root, dir+"/rhizome.retired.md", "old generated command\n")
		setWritten(t, root, dir+"/rhizome.retired.md", "old generated command\n")
		writeTestFile(t, root, dir+"/rhizome-edited.md", "old generated command\n")
		setWritten(t, root, dir+"/rhizome-edited.md", "old generated command\n")
		writeTestFile(t, root, dir+"/rhizome-edited.md", "locally edited command\n")
	}
	// Force an ordinary generated-file refresh to reach the old cleanup sweep.
	require.NoError(t, os.Remove(filepath.Join(root, ".agents/skills/rhizome/SKILL.md")))
	return root, env
}

func TestRunStaleCommandCleanupPreservesOwnership(t *testing.T) {
	t.Run("check previews removals without writing", func(t *testing.T) {
		root, env := staleCommandRepo(t)
		record := readTestFile(t, root, ".rhizome/generated-files.yml")
		out, err := runInit(t, RunOptions{Env: env, Dir: root, Check: true})
		require.ErrorIs(t, err, ErrChangesPending)
		for _, dir := range commandCleanupDirs {
			require.Equal(t, "custom command\n", readTestFile(t, root, dir+"/rhizome-my-custom.md"))
			require.Equal(t, "old generated command\n", readTestFile(t, root, dir+"/rhizome.retired.md"))
			require.Equal(t, "locally edited command\n", readTestFile(t, root, dir+"/rhizome-edited.md"))
			require.Contains(t, out, dir+"/rhizome.retired.md")
		}
		require.Equal(t, record, readTestFile(t, root, ".rhizome/generated-files.yml"))
		require.NoFileExists(t, filepath.Join(root, ".agents/skills/rhizome/SKILL.md"))
	})

	t.Run("apply removes only recorded unedited commands and reports them", func(t *testing.T) {
		root, env := staleCommandRepo(t)
		out, err := runInit(t, RunOptions{Env: env, Dir: root})
		require.NoError(t, err)
		for _, dir := range commandCleanupDirs {
			require.Equal(t, "custom command\n", readTestFile(t, root, dir+"/rhizome-my-custom.md"))
			require.Equal(t, "locally edited command\n", readTestFile(t, root, dir+"/rhizome-edited.md"))
			require.NoFileExists(t, filepath.Join(root, dir, "rhizome.retired.md"))
		}
		require.Contains(t, out, "Removed 5 files Rhizome no longer ships")
		record := readTestFile(t, root, ".rhizome/generated-files.yml")
		out, err = runInit(t, RunOptions{Env: env, Dir: root})
		require.NoError(t, err)
		require.NotContains(t, out, "Removed")
		require.Equal(t, record, readTestFile(t, root, ".rhizome/generated-files.yml"))
		for _, dir := range commandCleanupDirs {
			require.Equal(t, "custom command\n", readTestFile(t, root, dir+"/rhizome-my-custom.md"))
			require.Equal(t, "locally edited command\n", readTestFile(t, root, dir+"/rhizome-edited.md"))
		}
	})
}

func TestSyncStaleCommandsHonorsEditedDecisions(t *testing.T) {
	for _, take := range []bool{false, true} {
		name := "keep"
		if take {
			name = "remove"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			syncForTest(t, root, nil, failUI{t})
			paths := []string{".claude/commands/rhizome-edited.md", ".claude/prompts/rhizome-edited.md"}
			for _, rel := range paths {
				writeTestFile(t, root, rel, "old generated command\n")
				setWritten(t, root, rel, "old generated command\n")
				writeTestFile(t, root, rel, "edited command\n")
			}
			ui := &scriptedUI{take: take}
			report := syncForTest(t, root, nil, ui)
			require.Len(t, ui.asked, 1)
			require.Equal(t, []int{1}, ui.mirrors)
			record, ok := loadGeneratedFiles(root)
			require.True(t, ok)
			for _, rel := range paths {
				if take {
					require.NoFileExists(t, filepath.Join(root, rel))
					require.Contains(t, report.Removed, rel)
					require.NotContains(t, record.Written, rel)
				} else {
					require.Equal(t, "edited command\n", readTestFile(t, root, rel))
					require.Contains(t, report.Declined, rel)
					require.Equal(t, declinedRemoval, record.Declined[rel])
				}
			}
			report = syncForTest(t, root, nil, failUI{t})
			require.Empty(t, report.Removed)
			require.Empty(t, report.Declined)
		})
	}
}

func TestSyncStaleCommandsPreservesSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	syncForTest(t, root, nil, failUI{t})
	outside := t.TempDir()
	writeTestFile(t, outside, "target.md", "target content\n")
	target := filepath.Join(outside, "target.md")
	commandDir := ".claude/commands"
	require.NoError(t, os.MkdirAll(filepath.Join(root, commandDir), 0o755))
	custom := commandDir + "/rhizome-custom.md"
	recorded := commandDir + "/rhizome-retired.md"
	require.NoError(t, os.Symlink(target, filepath.Join(root, custom)))
	require.NoError(t, os.Symlink(target, filepath.Join(root, recorded)))
	setWritten(t, root, recorded, "target content\n")
	report := syncForTest(t, root, nil, failUI{t})
	require.Equal(t, "target content\n", readTestFile(t, outside, "target.md"))
	link, err := os.Readlink(filepath.Join(root, custom))
	require.NoError(t, err)
	require.Equal(t, target, link)
	require.NoFileExists(t, filepath.Join(root, recorded))
	require.Equal(t, []string{recorded}, report.Removed)
}
