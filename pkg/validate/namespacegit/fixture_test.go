package namespacegit_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/validate/namespacegit"
	"github.com/stretchr/testify/require"
)

type gitFixture struct {
	root, global string
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return root
}

func gitEnvironment(global, index string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			env = append(env, entry)
		}
	}
	if global == "" {
		global = os.DevNull
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+global, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")
	if index != "" {
		env = append(env, "GIT_INDEX_FILE="+index)
	}
	return env
}

func (f gitFixture) command(index string, args ...string) *exec.Cmd {
	// Setup commits must not leave detached maintenance racing live inventories.
	bound := []string{"-C", f.root, "--literal-pathspecs", "-c", "maintenance.auto=false", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.quotepath=false"}
	cmd := exec.Command("git", append(bound, args...)...)
	cmd.Env = gitEnvironment(f.global, index)
	return cmd
}

func (f gitFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	return f.indexGit(t, "", args...)
}

func (f gitFixture) indexGit(t *testing.T, index string, args ...string) string {
	t.Helper()
	cmd := f.command(index, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(t, err, "git %v: %s", args, stderr.String())
	return string(output)
}

func writeFile(t *testing.T, root, name, content string, mode os.FileMode) {
	t.Helper()
	name = filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
	require.NoError(t, os.WriteFile(name, []byte(content), mode))
	require.NoError(t, os.Chmod(name, mode))
}

func newFixture(t *testing.T) gitFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	f := gitFixture{root: canonicalTemp(t)}
	f.git(t, "init", "-q")
	for name, body := range map[string]string{
		"notes/Old.md": "base source\n", "notes/Existing.md": "base destination\n",
		"Other.md": "base other\n", "Assumed.md": "assumed\n", "Skipped.md": "skipped\n",
		"Conflict.md": "base conflict\n", "outside/Hidden.md": "outside\n",
	} {
		writeFile(t, f.root, name, body, 0o644)
	}
	f.git(t, "add", ".")
	f.git(t, "-c", "user.name=Synthetic", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture")
	writeFile(t, f.root, "Other.md", "unrelated staged\n", 0o644)
	f.git(t, "add", "Other.md")
	writeFile(t, f.root, "Other.md", "unrelated dirty\n", 0o644)
	return f
}

func cloneFixture(t *testing.T, f gitFixture) gitFixture {
	t.Helper()
	root := canonicalTemp(t)
	err := filepath.WalkDir(f.root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(f.root, name)
		if err != nil {
			return err
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), info.Mode().Perm())
		}
		body, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, rel), body, info.Mode().Perm())
	})
	require.NoError(t, err)
	return gitFixture{root: root, global: f.global}
}

func originalFiles(t *testing.T, root string, moves []namespacegit.Move) []namespacegit.File {
	t.Helper()
	seen := map[string]bool{}
	var files []namespacegit.File
	for _, move := range moves {
		for _, rel := range []string{move.Source, move.Destination} {
			if seen[rel] {
				continue
			}
			seen[rel] = true
			name := filepath.Join(root, filepath.FromSlash(rel))
			info, err := os.Lstat(name)
			if os.IsNotExist(err) {
				continue
			}
			require.NoError(t, err)
			body, err := os.ReadFile(name)
			require.NoError(t, err)
			files = append(files, namespacegit.File{Path: rel, Content: body, Mode: info.Mode()})
		}
	}
	return files
}

type fileEvidence struct {
	Mode os.FileMode
	Hash string
	Time time.Time
}

func inventory(t *testing.T, root string) map[string]fileEvidence {
	t.Helper()
	entries := map[string]fileEvidence{}
	require.NoError(t, filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Windows DirEntry.Info returns directory-enumeration metadata, which
		// can lag a fresh path query even with no intervening mutation.
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		evidence := fileEvidence{Mode: info.Mode(), Time: info.ModTime()}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			evidence.Hash = target
		} else if !entry.IsDir() {
			body, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			evidence.Hash = fmt.Sprintf("sha256:%x", sha256.Sum256(body))
		}
		entries[filepath.ToSlash(rel)] = evidence
		return nil
	}))
	return entries
}

func assertLiveUnchanged(t *testing.T, before, after map[string]fileEvidence) {
	t.Helper()
	require.Equal(t, len(before), len(after), "live file inventory changed")
	for name, old := range before {
		current, exists := after[name]
		require.True(t, exists, "live path disappeared: %s", name)
		require.Equal(t, old.Hash, current.Hash, "live bytes changed: %s", name)
		require.Equal(t, old.Mode, current.Mode, "live mode changed: %s", name)
		if old.Mode.IsRegular() && (strings.HasPrefix(name, ".git/objects/") || strings.HasPrefix(name, ".git/sharedindex.")) {
			continue // Git freshens existing immutable retention witnesses.
		}
		require.Equal(t, old.Time, current.Time, "mutable live metadata changed: %s", name)
	}
}

type indexSemantics struct {
	Stages, Flags, ResolveUndo string
	PersistentFlags            []string
}

var debugFlags = regexp.MustCompile(`(?m)flags: ([0-9a-f]+)$`)

func semantics(t *testing.T, f gitFixture, index string) indexSemantics {
	t.Helper()
	prefix := []string{"-c", "core.splitIndex=false"}
	run := func(args ...string) string { return f.indexGit(t, index, append(prefix, args...)...) }
	s := indexSemantics{Stages: run("ls-files", "--stage", "-z"), Flags: run("ls-files", "-v", "-z"), ResolveUndo: run("ls-files", "--resolve-undo", "-z")}
	for _, match := range debugFlags.FindAllStringSubmatch(run("ls-files", "--debug"), -1) {
		flags, err := strconv.ParseUint(match[1], 16, 32)
		require.NoError(t, err)
		// CE_UPDATE_IN_BASE is transient split-index bookkeeping; ITA and skip
		// flags are persistent and must survive. Debug output is test-only.
		s.PersistentFlags = append(s.PersistentFlags, fmt.Sprintf("%x", flags & ^uint64(0x08000000)))
	}
	return s
}

func installSnapshot(t *testing.T, original gitFixture, snapshot namespacegit.Snapshot) gitFixture {
	t.Helper()
	fresh := gitFixture{root: canonicalTemp(t)}
	fresh.git(t, "init", "-q")
	config, err := os.ReadFile(filepath.Join(original.root, ".git", "config"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(fresh.root, ".git", "config"), config, 0o600))
	writeFile(t, fresh.root, ".git/objects/info/alternates", filepath.Join(original.root, ".git", "objects")+"\n", 0o600)
	if sparse, err := os.ReadFile(filepath.Join(original.root, ".git", "info", "sparse-checkout")); err == nil {
		writeFile(t, fresh.root, ".git/info/sparse-checkout", string(sparse), 0o600)
	}
	writeFile(t, fresh.root, ".git/index", string(snapshot.Content), snapshot.Mode)
	require.Empty(t, fresh.git(t, "-c", "core.splitIndex=false", "rev-parse", "--shared-index-path"))
	return fresh
}
