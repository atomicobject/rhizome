package viewconfig

import (
	"io/fs"
	"path"
	"strings"
)

// WHY: the views folder mixes code that a browser may load with material it
// must not: definitions, dotfiles such as .env, and installed packages. One rule
// decides which is which, so the loader, the file route, reload, and diagnostics
// cannot disagree about what belongs to a view.

// privateName reports whether a file or folder name is kept away from browsers
// and from the loader: dot-prefixed names and installed packages.
func privateName(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// ServablePath reports whether a slash-separated path relative to the views
// folder may be served to a browser. Every YAML file there loads as a view
// definition, so no YAML file is served.
func ServablePath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "" || privateName(part) {
			return false
		}
	}
	return !isViewFile(rel)
}

// WalkFolder visits the regular files under dir in fsys, skipping private
// folders. It never follows a symlink; with an fs.FS from os.Root, nothing it
// reads can leave the root either.
func WalkFolder(fsys fs.FS, dir string, visit func(rel string, entry fs.DirEntry)) error {
	return fs.WalkDir(fsys, dir, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			if current == dir {
				return err
			}
			return nil
		}
		if entry.IsDir() {
			if current != dir && privateName(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			visit(path.Clean(current), entry)
		}
		return nil
	})
}
