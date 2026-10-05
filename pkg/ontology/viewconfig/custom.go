package viewconfig

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
)

// WHY: a custom view is code beside its definition, not a declarative table.
// It stays a view definition (one loader, one catalog, one mount model) and
// only swaps the source for an entry file that the web server serves.

// IsScriptEntry reports whether an entry is a module the server renders inside
// the view shell, as opposed to an HTML document served as written.
func IsScriptEntry(entry string) bool {
	switch strings.ToLower(filepath.Ext(entry)) {
	case ".tsx", ".jsx", ".ts", ".js":
		return true
	default:
		return false
	}
}

// EntryPath returns the absolute entry file for a file-backed custom view.
func (def ViewDefinition) EntryPath() string {
	if def.SourceSpec.Kind != SourceKindCustom || def.Source.Path == "" || def.SourceSpec.Entry == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(def.Source.Path), filepath.FromSlash(def.SourceSpec.Entry))
}

func validateCustom(raw, def ViewDefinition, entryFS fs.FS) []Issue {
	var issues []Issue
	entry := def.SourceSpec.Entry
	switch {
	case entry == "":
		issues = append(issues, issue(def, "missing_required_field", "source.entry", "source.entry is required for custom sources"))
	case !filepath.IsLocal(filepath.FromSlash(entry)):
		issues = append(issues, issue(def, "invalid_custom_entry", "source.entry", "source.entry must be a relative path inside the view's folder"))
	case !ServablePath(filepath.ToSlash(entry)):
		issues = append(issues, issue(def, "invalid_custom_entry", "source.entry", "source.entry must not be inside a dot-prefixed folder or node_modules"))
	case !IsScriptEntry(entry) && strings.ToLower(filepath.Ext(entry)) != ".html":
		issues = append(issues, issue(def, "invalid_custom_entry", "source.entry", "source.entry must end in .tsx, .jsx, .ts, .js, or .html"))
	default:
		if def.Source.Path != "" && !customEntryReadable(entryFS, filepath.Dir(def.Source.Path), entry) {
			issues = append(issues, issue(def, "custom_entry_not_found", "source.entry",
				fmt.Sprintf("entry file %s does not exist inside the view's folder", def.EntryPath())))
		}
	}
	issues = append(issues, unexpectedSourceFields(def, "source.type", def.SourceSpec.Type, "source.interface", def.SourceSpec.Interface,
		"source.queryRecipe", def.SourceSpec.QueryRecipe, "source.resultPath", def.SourceSpec.ResultPath)...)
	if len(def.SourceSpec.Inputs) > 0 {
		issues = append(issues, issue(def, "unexpected_field", "source.inputs", "source.inputs is only supported for query_recipe sources"))
	}
	for field, value := range map[string]any{"defaults": raw.Defaults, "variants": raw.Variants, "filterPresets": raw.FilterPresets} {
		if !reflect.ValueOf(value).IsZero() {
			issues = append(issues, issue(def, "unexpected_field", field, field+" is not supported for custom sources"))
		}
	}
	return issues
}

// customEntryReadable resolves the entry through os.Root, as the server does, so
// an entry that is a symlink out of the view's folder fails validation instead
// of passing here and then 404ing in the browser. With entryFS, folder is a
// slash path inside it.
func customEntryReadable(entryFS fs.FS, folder, entry string) bool {
	if entryFS != nil {
		info, err := fs.Stat(entryFS, path.Join(filepath.ToSlash(folder), entry))
		return err == nil && info.Mode().IsRegular()
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Stat(filepath.FromSlash(entry))
	return err == nil && info.Mode().IsRegular()
}
