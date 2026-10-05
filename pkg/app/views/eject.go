package views

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// ErrEjectRefused means an eject wrote nothing because its copy would collide
// with the repository's views.
var ErrEjectRefused = errors.New("eject refused")

// EjectResult names the bundled folder an eject copied and every file it
// wrote, relative to the vault root.
type EjectResult struct {
	ID      string   `json:"id"`
	Folder  string   `json:"folder"`
	Written []string `json:"written"`
}

// Eject copies a bundled view's folder into .rhizome/views/<folder>/, where the
// copy replaces the bundled views it shares ids with. A folder is the unit
// views share components in, so its sibling views come along. Eject writes
// nothing when any destination file already exists.
func (s *Service) Eject(_ context.Context, id string) (EjectResult, error) {
	id = strings.TrimSpace(id)
	if s.opts.VaultPath == "" {
		return EjectResult{}, fmt.Errorf("%w: no vault to eject into", ErrEjectRefused)
	}
	bundled, _ := viewconfig.LoadFS(s.opts.Bundled)
	var def *viewconfig.ViewDefinition
	for i := range bundled {
		if bundled[i].ID == id {
			def = &bundled[i]
			break
		}
	}
	if def == nil {
		return EjectResult{}, fmt.Errorf("%w: %s is not a bundled view", ErrViewNotFound, id)
	}
	folder := path.Dir(def.Source.Path)
	// Every view in the folder comes along, so none of their ids may already
	// belong to a repository view.
	copied := map[string]bool{}
	for _, other := range bundled {
		if folder == "." || strings.HasPrefix(other.Source.Path, folder+"/") {
			copied[other.ID] = true
		}
	}
	repository, _ := s.loadDefinitions()
	viewsRoot := viewconfig.DefaultSourceRoot(s.opts.VaultPath)
	for _, existing := range repository {
		if copied[existing.ID] {
			// Typically an eject from an earlier release, before the folder
			// gained views: only a fresh copy of the whole folder fits together.
			holder := filepath.Dir(existing.Source.Path)
			if rel, err := filepath.Rel(viewsRoot, holder); err == nil && filepath.IsLocal(rel) {
				holder = vaultViewPath(filepath.ToSlash(rel))
			}
			return EjectResult{}, fmt.Errorf("%w: view %s already comes from %s; move %s out of .rhizome/views, eject %s again, and reapply your changes to the new copy",
				ErrEjectRefused, existing.ID, existing.Source.Path, holder, id)
		}
	}
	var files []string
	if err := viewconfig.WalkFolder(s.opts.Bundled, folder, func(rel string, _ fs.DirEntry) {
		if !developmentOnly(rel) {
			files = append(files, rel)
		}
	}); err != nil {
		return EjectResult{}, fmt.Errorf("read bundled folder %s: %w", folder, err)
	}
	if err := os.MkdirAll(viewsRoot, 0o755); err != nil {
		return EjectResult{}, err
	}
	// os.Root keeps every write inside .rhizome/views, even through a symlink.
	root, err := os.OpenRoot(viewsRoot)
	if err != nil {
		return EjectResult{}, err
	}
	defer root.Close()
	var conflicts []string
	for _, rel := range files {
		if _, err := root.Lstat(filepath.FromSlash(rel)); !errors.Is(err, fs.ErrNotExist) {
			conflicts = append(conflicts, vaultViewPath(rel))
		}
	}
	if len(conflicts) > 0 {
		return EjectResult{}, fmt.Errorf("%w: %s already exists", ErrEjectRefused, strings.Join(conflicts, ", "))
	}
	result := EjectResult{ID: id, Folder: vaultViewPath(folder)}
	var made []string
	for _, rel := range files {
		if err := ejectFile(root, s.opts.Bundled, rel, &made); err != nil {
			// Remove this attempt's files and folders, newest first, so a
			// retry is not refused by a partial copy.
			for i := len(made) - 1; i >= 0; i-- {
				_ = root.Remove(made[i])
			}
			return EjectResult{}, err
		}
		result.Written = append(result.Written, vaultViewPath(rel))
	}
	return result, nil
}

// developmentOnly reports whether a bundled file is a test or fixture, which
// the kit build leaves out of the shipped copy (web/vite.kit.config.ts) and an
// eject from RHIZOME_BUNDLED_VIEWS_DIR's source folder leaves out too.
func developmentOnly(rel string) bool {
	return slices.ContainsFunc(strings.Split(rel, "/"), func(name string) bool {
		return strings.Contains(name, ".test.") || name == "__fixtures__" || name == "fixtures"
	})
}

// ejectFile copies one file, appending each folder and file it creates to made.
func ejectFile(root *os.Root, bundled fs.FS, rel string, made *[]string) error {
	data, err := fs.ReadFile(bundled, rel)
	if err != nil {
		return err
	}
	dir := ""
	for _, part := range strings.Split(path.Dir(rel), "/") {
		if part == "." {
			continue
		}
		dir = path.Join(dir, part)
		err := root.Mkdir(filepath.FromSlash(dir), 0o755)
		if err == nil {
			*made = append(*made, filepath.FromSlash(dir))
		} else if !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	file, err := root.OpenFile(filepath.FromSlash(rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	*made = append(*made, filepath.FromSlash(rel))
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func vaultViewPath(rel string) string {
	return path.Join(".rhizome", "views", rel)
}
