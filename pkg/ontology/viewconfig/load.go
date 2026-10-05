package viewconfig

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func DefaultSourceRoot(vaultPath string) string {
	return filepath.Join(vaultPath, ".rhizome", "views")
}

func LoadDefaultSource(vaultPath string) ([]ViewDefinition, []Issue) {
	root := DefaultSourceRoot(vaultPath)
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, []Issue{{Code: "view_path_error", Path: root, Message: err.Error()}}
	}
	return LoadPath(root)
}

func LoadRoots(roots []string) ([]ViewDefinition, []Issue) {
	var views []ViewDefinition
	var issues []Issue
	for _, root := range roots {
		rootViews, rootIssues := LoadPath(root)
		views = append(views, rootViews...)
		issues = append(issues, rootIssues...)
	}
	sortViews(views)
	return views, issues
}

func LoadPath(path string) ([]ViewDefinition, []Issue) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, []Issue{{Code: "view_path_error", Path: path, Message: err.Error()}}
	}
	if !info.IsDir() {
		return loadFile(path)
	}
	var views []ViewDefinition
	var issues []Issue
	err = filepath.WalkDir(path, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			issues = append(issues, Issue{Code: "view_path_error", Path: current, Message: walkErr.Error()})
			return nil
		}
		if d.IsDir() {
			if current != path && privateName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isViewFile(current) {
			return nil
		}
		fileViews, fileIssues := loadFile(current)
		views = append(views, fileViews...)
		issues = append(issues, fileIssues...)
		return nil
	})
	if err != nil {
		issues = append(issues, Issue{Code: "view_path_error", Path: path, Message: err.Error()})
	}
	sortViews(views)
	return views, issues
}

// LoadFS loads every definition in fsys, skipping private folders like the
// disk loader. Source paths are slash paths inside fsys. A missing root is an
// empty source, so a build without bundled views simply has none.
func LoadFS(fsys fs.FS) ([]ViewDefinition, []Issue) {
	if fsys == nil {
		return nil, nil
	}
	if _, err := fs.Stat(fsys, "."); err != nil {
		return nil, nil
	}
	var views []ViewDefinition
	var issues []Issue
	err := WalkFolder(fsys, ".", func(rel string, _ fs.DirEntry) {
		if !isViewFile(rel) {
			return
		}
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			issues = append(issues, Issue{Code: "view_read_error", Path: rel, Message: err.Error()})
			return
		}
		fileViews, fileIssues := Decode(data, rel)
		views = append(views, fileViews...)
		issues = append(issues, fileIssues...)
	})
	if err != nil {
		issues = append(issues, Issue{Code: "view_path_error", Path: ".", Message: err.Error()})
	}
	sortViews(views)
	return views, issues
}

func loadFile(path string) ([]ViewDefinition, []Issue) {
	if !isViewFile(path) {
		return nil, []Issue{{Code: "unsupported_view_file", Path: path, Message: "expected .yaml or .yml view file"}}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []Issue{{Code: "view_read_error", Path: path, Message: err.Error()}}
	}
	return Decode(data, path)
}

// Decode reads one view file's YAML as the loader does, recording path as its
// source; an empty document yields no view.
func Decode(data []byte, path string) ([]ViewDefinition, []Issue) {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	var view ViewDefinition
	if err := dec.Decode(&view); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, []Issue{{Code: "view_yaml_decode_error", Path: path, Line: 1, Message: err.Error()}}
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, []Issue{{Code: "multiple_documents_unsupported", Path: path, Line: 1, Message: "configured view files support one YAML document per file"}}
	} else if !errors.Is(err, io.EOF) {
		return nil, []Issue{{Code: "view_yaml_decode_error", Path: path, Line: 1, Message: err.Error()}}
	}
	view.Source = Source{Path: path, Line: 1}
	return []ViewDefinition{view}, nil
}

func isViewFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func sortViews(views []ViewDefinition) {
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].ID < views[j].ID
	})
}
