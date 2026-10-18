package repositorytrust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

const storeDirectoryName = "trusted-repositories"

// Resolve the operating-system account home independently of HOME and related
// environment variables. main loads repository dotenv files before dispatch,
// and repository contents must not be able to redirect the trust store.
var processUserHome, processUserHomeErr = operatingSystemUserHome()

func operatingSystemUserHome() (string, error) {
	account, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolve operating-system user: %w", err)
	}
	home := strings.TrimSpace(account.HomeDir)
	if home == "" || !filepath.IsAbs(home) {
		return "", errors.New("operating-system user home is unavailable")
	}
	return filepath.Clean(home), nil
}

// Store keeps local repository trust outside the repository being trusted.
// Each canonical checkout gets an independent record. A linked Git worktree
// without its own record inherits its main checkout's trust.
type Store struct {
	Dir string
}

// DefaultStore returns the user-local Rhizome repository trust store.
func DefaultStore() (Store, error) {
	if processUserHomeErr != nil {
		return Store{}, processUserHomeErr
	}
	if strings.TrimSpace(processUserHome) == "" {
		return Store{}, errors.New("user home directory is required for repository trust")
	}
	return Store{Dir: filepath.Join(processUserHome, ".config", "rhizome", storeDirectoryName)}, nil
}

// CanonicalCheckout resolves aliases so one checkout has one trust identity.
func CanonicalCheckout(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("repository checkout path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repository checkout: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve repository checkout: %w", err)
	}
	return filepath.Clean(canonical), nil
}

// Trusted reports whether checkout, or the main checkout of the linked Git
// worktree it lives in, has a local trust record.
func (s Store) Trusted(checkout string) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	canonical, err := CanonicalCheckout(checkout)
	if err != nil {
		return false, err
	}
	trusted, err := s.recorded(canonical)
	if trusted || err != nil {
		return trusted, err
	}
	if main, ok := MainCheckout(canonical); ok {
		return s.recorded(main)
	}
	return false, nil
}

func (s Store) recorded(canonical string) (bool, error) {
	record, err := os.ReadFile(s.recordPath(canonical))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read repository trust: %w", err)
	}
	return strings.TrimSpace(string(record)) == canonical, nil
}

// Trust records explicit local trust for checkout and returns its canonical path.
func (s Store) Trust(checkout string) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	canonical, err := CanonicalCheckout(checkout)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", fmt.Errorf("create repository trust store: %w", err)
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return "", fmt.Errorf("secure repository trust store: %w", err)
	}
	tmp, err := os.CreateTemp(s.Dir, ".trust-*")
	if err != nil {
		return "", fmt.Errorf("create repository trust record: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("secure repository trust record: %w", err)
	}
	if _, err := tmp.WriteString(canonical + "\n"); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write repository trust record: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("sync repository trust record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close repository trust record: %w", err)
	}
	if err := os.Rename(tmpPath, s.recordPath(canonical)); err != nil {
		return "", fmt.Errorf("publish repository trust record: %w", err)
	}
	return canonical, nil
}

// Untrust removes checkout's local trust record.
func (s Store) Untrust(checkout string) (string, bool, error) {
	if err := s.validate(); err != nil {
		return "", false, err
	}
	canonical, err := CanonicalCheckout(checkout)
	if err != nil {
		return "", false, err
	}
	err = os.Remove(s.recordPath(canonical))
	if errors.Is(err, os.ErrNotExist) {
		return canonical, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("remove repository trust: %w", err)
	}
	return canonical, true, nil
}

func (s Store) validate() error {
	if strings.TrimSpace(s.Dir) == "" || !filepath.IsAbs(s.Dir) {
		return errors.New("repository trust store directory must be absolute")
	}
	return nil
}

func (s Store) recordPath(canonical string) string {
	digest := sha256.Sum256([]byte(canonical))
	return filepath.Join(s.Dir, hex.EncodeToString(digest[:])+".trust")
}

// MainCheckout maps a checkout inside a linked Git worktree to the same path
// in the repository's main working tree. A worktree's .git file is checkout
// content anyone could write, so the repository must vouch for it: the Git
// directory it names must be an entry in the main repository's worktrees
// directory, and that entry's gitdir record must name this worktree's .git file.
// ponytail: main trees found only as the parent of a ".git" common directory;
// core.worktree and --separate-git-dir layouts keep per-checkout trust.
func MainCheckout(canonical string) (string, bool) {
	root := canonical
	for {
		if _, err := os.Lstat(filepath.Join(root, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", false
		}
		root = parent
	}
	dotGit := filepath.Join(root, ".git")
	stat, err := os.Lstat(dotGit)
	if err != nil || !stat.Mode().IsRegular() {
		return "", false
	}
	target, ok := strings.CutPrefix(readLine(dotGit), "gitdir:")
	if !ok {
		return "", false
	}
	gitDir := resolveFrom(root, strings.TrimSpace(target))
	if resolveFrom(gitDir, readLine(filepath.Join(gitDir, "gitdir"))) != resolveFrom(root, ".git") {
		return "", false
	}
	relative := readLine(filepath.Join(gitDir, "commondir"))
	if relative == "" {
		return "", false
	}
	common := resolveFrom(gitDir, relative)
	if filepath.Base(common) != ".git" || filepath.Dir(gitDir) != filepath.Join(common, "worktrees") {
		return "", false
	}
	rest, err := filepath.Rel(root, canonical)
	if err != nil {
		return "", false
	}
	main := filepath.Join(filepath.Dir(common), rest)
	if main == canonical {
		return "", false
	}
	return main, true
}

func readLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func resolveFrom(base, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		return canonical
	}
	return filepath.Clean(path)
}
