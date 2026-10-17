package sourcetxn

import (
	"fmt"
	"os"

	"path/filepath"
	"runtime"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func RepairModeBits(mode os.FileMode) uint32 {
	return uint32(mode & (os.ModePerm | repairSpecialMode))
}

func RepairFileMode(bits uint32) os.FileMode {
	return os.FileMode(bits) & (os.ModePerm | repairSpecialMode)
}

func RepairModeMatches(actual os.FileMode, expected uint32) bool {
	if runtime.GOOS == "windows" {
		// Go exposes synthetic permission bits for regular Windows files and
		// Chmod only controls their owner-writable/read-only state.
		return actual.Perm()&0o200 != 0 == (os.FileMode(expected)&0o200 != 0)
	}
	return RepairModeBits(actual) == expected
}

func repairAbsPath(runCtx writerContext, rel string) (string, error) {
	vaultPaths, err := paths.NewVaultPaths(runCtx.VaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return "", fmt.Errorf("resolve vault root: %w", err)
	}
	clean, err := paths.CleanRelPath(rel)
	if err != nil || clean == "" {
		return "", fmt.Errorf("resolve repair path %q: %w", rel, err)
	}
	resolved := filepath.Join(vaultPaths.Root(), filepath.FromSlash(clean.String()))
	if err := verifyEditRecoveryPath(vaultPaths.Root(), resolved, true); err != nil {
		return "", &SourceConflictError{Path: rel, Err: err}
	}
	strict, err := vaultPaths.RelStrict(resolved)
	if err != nil {
		return "", fmt.Errorf("repair path %q escapes vault: %w", rel, err)
	}
	if strict == "" {
		return "", fmt.Errorf("repair path %q resolves to vault root", rel)
	}
	return resolved, nil
}

func repairArtifactPath(abs, shortID string, index int, kind string) string {
	return fmt.Sprintf("%s.rzm-repair-%s-%03d.%s", abs, shortID, index, kind)
}

func mustEntryAbs(states []fileState, rel string) string {
	for _, state := range states {
		if state.Path == rel {
			return state.Absolute
		}
	}
	panic("repair journal entry missing prepared State: " + rel)
}

func writeExclusiveSyncedFile(path string, content []byte, mode os.FileMode) (bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return false, err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return true, err
	}
	if err := file.Chmod(mode & (os.ModePerm | repairSpecialMode)); err != nil {
		_ = file.Close()
		return true, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return true, err
	}
	if err := file.Close(); err != nil {
		return true, err
	}
	return true, syncDirectory(filepath.Dir(path))
}

func writeSyncedFile(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode & (os.ModePerm | repairSpecialMode)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
