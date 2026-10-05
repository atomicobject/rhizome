package validate

import (
	"fmt"
	"os"
	stdpath "path"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/atomicobject/rhizome/pkg/paths"
)

const repairSpecialMode = os.ModeSetuid | os.ModeSetgid | os.ModeSticky

func repairModeBits(mode os.FileMode) uint32 {
	return uint32(mode & (os.ModePerm | repairSpecialMode))
}

func repairFileMode(bits uint32) os.FileMode {
	return os.FileMode(bits) & (os.ModePerm | repairSpecialMode)
}

func repairModeMatches(actual os.FileMode, expected uint32) bool {
	if runtime.GOOS == "windows" {
		// Go exposes synthetic permission bits for regular Windows files and
		// Chmod only controls their owner-writable/read-only state.
		return actual.Perm()&0o200 != 0 == (os.FileMode(expected)&0o200 != 0)
	}
	return repairModeBits(actual) == expected
}

func repairAbsPath(runCtx RunContext, rel string) (string, error) {
	vaultPaths, err := paths.NewVaultPaths(runCtx.VaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return "", fmt.Errorf("resolve vault root: %w", err)
	}
	clean, err := paths.CleanRelPath(rel)
	if err != nil || clean == "" {
		return "", fmt.Errorf("resolve repair path %q: %w", rel, err)
	}
	parentRel, err := paths.CleanRelPath(stdpath.Dir(clean.String()))
	if err != nil {
		return "", fmt.Errorf("resolve repair path parent %q: %w", rel, err)
	}
	parentAbs := vaultPaths.Root()
	if parentRel != "" {
		abs, absErr := vaultPaths.Abs(parentRel)
		if absErr != nil {
			return "", absErr
		}
		parentAbs = filepath.FromSlash(abs.String())
	}
	resolvedParent, err := resolveRepairPath(parentAbs)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(resolvedParent, filepath.FromSlash(stdpath.Base(clean.String())))
	strict, err := vaultPaths.RelStrict(resolved)
	if err != nil {
		return "", fmt.Errorf("repair path %q escapes vault: %w", rel, err)
	}
	if strict == "" {
		return "", fmt.Errorf("repair path %q resolves to vault root", rel)
	}
	return resolved, nil
}

func resolveRepairPath(abs string) (string, error) {
	probe := filepath.Clean(abs)
	var suffix []string
	for {
		_, err := os.Lstat(probe)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", err
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
	resolved, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return resolved, nil
}

func repairArtifactPath(abs, shortID string, index int, kind string) string {
	return fmt.Sprintf("%s.rzm-repair-%s-%03d.%s", abs, shortID, index, kind)
}

func mustEntryAbs(states []repairFileState, rel string) string {
	for _, state := range states {
		if state.rel == rel {
			return state.abs
		}
	}
	panic("repair journal entry missing prepared state: " + rel)
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

func repairOperationDelta(operations []RepairOperation) ([]string, []PathRename, []string) {
	var changed, deleted []string
	var renamed []PathRename
	renameDestinations := make(map[string]string)
	for _, operation := range operations {
		if operation.Kind == RepairOperationRename {
			renameDestinations[operation.Path] = operation.DestinationPath
		}
	}
	for _, operation := range operations {
		switch operation.Kind {
		case RepairOperationWrite:
			path := operation.Path
			if destination := renameDestinations[path]; destination != "" {
				path = destination
			}
			changed = append(changed, path)
		case RepairOperationRename:
			renamed = append(renamed, PathRename{From: operation.Path, To: operation.DestinationPath})
			if operation.Content != nil && SourceHash(operation.Content) != operation.SourceHash {
				changed = append(changed, operation.DestinationPath)
			}
		case RepairOperationDelete:
			deleted = append(deleted, operation.Path)
		}
	}
	sort.Slice(renamed, func(i, j int) bool {
		if renamed[i].From != renamed[j].From {
			return renamed[i].From < renamed[j].From
		}
		return renamed[i].To < renamed[j].To
	})
	return sortedUnique(changed), renamed, sortedUnique(deleted)
}
