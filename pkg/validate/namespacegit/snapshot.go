package namespacegit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func contentHash(content []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(content)) }

func readOriginal(name string) ([]byte, Witness, error) {
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return nil, Witness{}, nil
	}
	if err != nil {
		return nil, Witness{}, err
	}
	if !info.Mode().IsRegular() {
		return nil, Witness{}, fmt.Errorf("Git index is not a regular bound file")
	}
	content, err := os.ReadFile(name)
	if err != nil {
		return nil, Witness{}, err
	}
	return content, Witness{Exists: true, Hash: contentHash(content), Mode: info.Mode()}, nil
}

func readSnapshot(name string, mode os.FileMode) (Snapshot, error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Content: content, Hash: contentHash(content), Mode: mode}, nil
}

func validateScratch(scratch, gitDir string) error {
	if !filepath.IsAbs(scratch) {
		return fmt.Errorf("Git scratch must be an absolute owned directory")
	}
	canonical, err := filepath.EvalSymlinks(scratch)
	if err != nil {
		return err
	}
	if filepath.Clean(scratch) != canonical {
		return fmt.Errorf("Git scratch must be a canonical owned directory")
	}
	if rel, err := filepath.Rel(gitDir, scratch); err != nil || rel == "." || !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Git scratch cannot be inside live Git metadata")
	}
	return nil
}

func createShadow(ctx context.Context, scratch string, files map[string]File, moves []Move) (string, error) {
	work := filepath.Join(scratch, "git-preparation")
	if err := os.Mkdir(work, 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(filepath.Join(work, "hooks"), 0o700); err != nil {
		return "", err
	}
	tree := filepath.Join(work, "tree")
	if err := os.Mkdir(tree, 0o700); err != nil {
		return "", err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		name := filepath.Join(tree, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(name, file.Content, 0o600); err != nil {
			return "", err
		}
		if err := os.Chmod(name, file.Mode); err != nil {
			return "", err
		}
	}
	for _, move := range moves {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(tree, filepath.FromSlash(move.Destination))), 0o700); err != nil {
			return "", err
		}
	}
	return work, nil
}
