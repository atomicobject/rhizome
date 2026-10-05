package namespacegit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func refuseGitSelectors() error {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE"} {
		if value, _ := os.LookupEnv(name); value != "" {
			return fmt.Errorf("Git namespace preparation refuses inherited %s", name)
		}
	}
	return nil
}

func bindGitDir(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, ".git")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%w: directory-backed repository required", ErrFallback)
	}
	if err != nil {
		return "", err
	}
	if info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: directory-backed repository required", ErrFallback)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Git metadata directory is redirected or nonregular")
	}
	if _, err := os.Lstat(filepath.Join(dir, "commondir")); err == nil {
		return "", fmt.Errorf("%w: shared Git directory is unsupported", ErrFallback)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return dir, nil
}

type gitCommand struct{ dir, work string }

func (git gitCommand) run(ctx context.Context, index string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bound := []string{"--git-dir=" + git.dir, "--work-tree=" + git.work, "--literal-pathspecs", "-c", "core.splitIndex=false", "-c", "index.sparse=false", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + filepath.Join(filepath.Dir(git.work), "hooks")}
	cmd := exec.CommandContext(ctx, "git", append(bound, args...)...)
	cmd.Dir = git.work
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v: %s", ErrFallback, args[0], err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}
