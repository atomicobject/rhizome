package obsidian

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type Note struct {
}

type NoteReader interface {
	GetContents(VaultDefinition, string) (string, error)
	GetNotesList(VaultDefinition) ([]string, error)
	GetModTime(VaultDefinition, string) (time.Time, error)
	Title(string) (string, bool)
}

func (m *Note) GetContents(cfg VaultDefinition, noteName string) (string, error) {
	vaultPath := cfg.BasePath()
	if vaultPath == "" {
		return "", errors.New(RhizomeVaultPathInvalidError)
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return "", errors.New(RhizomeVaultPathInvalidError)
	}

	abs, err := resolveNoteInput(vaultPaths, cfg, noteName)
	if err != nil || abs == "" {
		return "", errors.New(NoteDoesNotExistError)
	}

	content, err := os.ReadFile(abs.String())
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New(NoteDoesNotExistError)
		}
		return "", errors.New(VaultReadError)
	}

	return string(content), nil
}

// GetContentsContext provides the context-aware NoteReader extension used by
// cancellable health scans. The filesystem read itself is still performed by
// GetContents, with cancellation checked before and after it.
func (m *Note) GetContentsContext(ctx context.Context, cfg VaultDefinition, noteName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	content, err := m.GetContents(cfg, noteName)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	return content, err
}

func (m *Note) GetNotesList(cfg VaultDefinition) ([]string, error) {
	return DiscoverFiles(cfg)
}

// GetNotesListContext provides the context-aware NoteReader extension used by
// cancellable health scans.
func (m *Note) GetNotesListContext(ctx context.Context, cfg VaultDefinition) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	notes, err := m.GetNotesList(cfg)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return notes, err
}

// GetModTime returns the filesystem modification time for the specified note.
func (m *Note) GetModTime(cfg VaultDefinition, notePath string) (time.Time, error) {
	vaultPath := cfg.BasePath()
	if vaultPath == "" {
		return time.Time{}, errors.New(RhizomeVaultPathInvalidError)
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return time.Time{}, errors.New(RhizomeVaultPathInvalidError)
	}

	abs, err := resolveNoteInput(vaultPaths, cfg, notePath)
	if err != nil || abs == "" {
		return time.Time{}, errors.New(NoteDoesNotExistError)
	}
	info, err := os.Stat(abs.String())
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, errors.New(NoteDoesNotExistError)
		}
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// resolveNoteInput preserves an explicit admitted note path while retaining
// legacy Markdown normalization for extensionless inputs. Explicit Markdown
// remains readable for compatibility even when a narrow collection include
// omits it; other formats must be admitted by the collection configuration.
func resolveNoteInput(vaultPaths paths.VaultPaths, cfg VaultDefinition, input string) (paths.AbsPath, error) {
	rel, err := vaultPaths.RelNotePathStrict(input)
	if err == nil && rel != "" && (strings.EqualFold(filepath.Ext(rel.String()), ".md") || (cfg.IsCollection() && NotePathMatchesIncludes(cfg, rel.String()))) {
		return vaultPaths.AbsNotePath(rel)
	}

	legacyRel, legacyErr := vaultPaths.RelNoteStrict(input)
	if legacyErr != nil || legacyRel == "" {
		return "", legacyErr
	}
	return vaultPaths.AbsNote(legacyRel)
}

// Title returns a best-effort display title for a note path (filename sans extension).
func (m *Note) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}
