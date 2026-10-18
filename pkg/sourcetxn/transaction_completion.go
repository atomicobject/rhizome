package sourcetxn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func prepareRepairCompletionState(runCtx writerContext, artifact *CompletionArtifact) (*fileState, error) {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return nil, err
	}
	if artifact == nil {
		return nil, nil
	}
	target, rel, err := repairCompletionTarget(runCtx, artifact.Path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(target); err == nil {
		return nil, fmt.Errorf("repair completion artifact already exists: %s", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect repair completion artifact %s: %w", rel, err)
	}
	return &fileState{
		Path:         rel,
		Absolute:     target,
		OriginalPath: rel,
		Internal:     true,
		FinalExists:  true,
		FinalContent: append([]byte(nil), artifact.Content...),
		FinalMode:    0o600,
	}, nil
}

func repairCompletionTarget(runCtx writerContext, raw string) (string, string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", "", fmt.Errorf("repair completion artifact target is required")
	}
	if !filepath.IsAbs(raw) {
		return "", "", fmt.Errorf("repair completion artifact target must be absolute")
	}
	root := strings.TrimSpace(runCtx.VaultPath)
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil || vaultPaths.Root() == "" {
		if err == nil {
			err = fmt.Errorf("vault root is required")
		}
		return "", "", fmt.Errorf("resolve repair completion vault: %w", err)
	}
	canonicalRel, err := completionRelativePath(vaultPaths, filepath.Clean(raw))
	if err != nil {
		return "", "", fmt.Errorf("repair completion artifact escaped vault: %w", err)
	}
	rel := canonicalRel.String()
	prefix := repairCompletionDirectory + "/"
	if rel == repairCompletionDirectory || !strings.HasPrefix(filepath.ToSlash(rel), prefix) {
		return "", "", fmt.Errorf("repair completion artifact must be beneath %s", repairCompletionDirectory)
	}
	canonicalCtx := runCtx
	canonicalCtx.VaultPath = vaultPaths.Root()
	target, err := repairAbsPath(canonicalCtx, rel)
	if err != nil {
		return "", "", fmt.Errorf("resolve repair completion artifact %s: %w", rel, err)
	}
	allowed := filepath.Join(vaultPaths.Root(), filepath.FromSlash(repairCompletionDirectory))
	relToAllowed, err := filepath.Rel(allowed, target)
	if err != nil || relToAllowed == "." || filepath.IsAbs(relToAllowed) || strings.HasPrefix(relToAllowed, ".."+string(filepath.Separator)) || relToAllowed == ".." {
		return "", "", fmt.Errorf("repair completion artifact must be beneath %s", repairCompletionDirectory)
	}
	return target, rel, nil
}

// Match the caller's spelling of the vault root by inode, then preserve the
// lexical suffix. Resolving the entire target would hide symlinks below the
// vault root and expand receipt publication authority.
func completionRelativePath(vault paths.VaultPaths, raw string) (paths.RelPath, error) {
	root := filepath.FromSlash(vault.Root())
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	for ancestor := filepath.Dir(raw); ; ancestor = filepath.Dir(ancestor) {
		if candidate, statErr := os.Stat(ancestor); statErr == nil && os.SameFile(rootInfo, candidate) {
			within, relErr := filepath.Rel(root, ancestor)
			inside := relErr == nil && within != "." && within != ".." && !strings.HasPrefix(within, ".."+string(filepath.Separator)) && !filepath.IsAbs(within)
			if !inside {
				rel, err := filepath.Rel(ancestor, raw)
				if err != nil {
					return "", err
				}
				return paths.CleanRelPath(filepath.ToSlash(rel))
			}
		}
		if filepath.Dir(ancestor) == ancestor {
			return "", fmt.Errorf("target is outside the bound vault root")
		}
	}
}

func ensureRepairCompletionParent(runCtx writerContext, target, rel string) error {
	root := strings.TrimSpace(runCtx.VaultPath)
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil || vaultPaths.Root() == "" {
		if err == nil {
			err = fmt.Errorf("vault root is required")
		}
		return fmt.Errorf("resolve repair completion vault: %w", err)
	}
	base := filepath.Join(vaultPaths.Root(), filepath.FromSlash(repairCompletionDirectory))
	if err := ensureRepairDirectDirectory(filepath.Dir(base), 0o755); err != nil {
		return err
	}
	if err := ensureRepairDirectDirectory(base, 0o700); err != nil {
		return err
	}
	parent := filepath.Dir(target)
	relParent := filepath.Dir(filepath.FromSlash(rel))
	extra := strings.TrimPrefix(filepath.ToSlash(relParent), repairCompletionDirectory)
	extra = strings.TrimPrefix(extra, "/")
	current := base
	for _, component := range strings.Split(extra, "/") {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := ensureRepairDirectDirectory(current, 0o700); err != nil {
			return err
		}
	}
	if filepath.Clean(current) != filepath.Clean(parent) {
		return fmt.Errorf("repair completion artifact parent changed during preparation")
	}
	return nil
}

func ensureRepairDirectDirectory(dir string, mode os.FileMode) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create repair completion directory %s: %w", dir, err)
		}
		info, err = os.Lstat(dir)
	}
	if err != nil {
		return fmt.Errorf("inspect repair completion directory %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("repair completion path must be a direct directory: %s", dir)
	}
	return nil
}

func ensureDirectory(run writerContext, dir string, mode os.FileMode) error {
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return err
	}
	if err := verifyEditRecoveryPath(run.VaultPath, filepath.Join(dir, ".probe"), true); err != nil {
		return err
	}
	rel, err := filepath.Rel(run.VaultPath, dir)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("repair parent escaped vault: %s", dir)
	}
	current := run.VaultPath
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := run.Lease.Validate(run.VaultPath); err != nil {
			return err
		}
		if err := verifyEditRecoveryPath(run.VaultPath, filepath.Join(current, ".probe"), true); err != nil {
			return err
		}
		if err := ensureRepairDirectDirectory(current, mode); err != nil {
			return err
		}
	}
	return nil
}
