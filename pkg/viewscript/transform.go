// Package viewscript turns one custom view source file into a browser module.
package viewscript

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// WHY: custom views must work with no Node toolchain. esbuild's Go API gives a
// per-file syntax transform only. There is no bundling, module resolution, or
// type checking here on purpose: the browser resolves imports (import map for
// the kit, relative URLs for siblings), so rzm never needs a node_modules. The
// one rewrite is a sibling stylesheet import, which a browser cannot load as a
// module; it is the only reason a plain JavaScript module is transformed.

// NeedsTransform reports whether a file must be transformed before a browser
// can load it as a module: TypeScript and JSX always, and plain JavaScript only
// when it imports a sibling stylesheet.
func NeedsTransform(path string, source []byte) bool {
	if _, ok := loaderFor(path); ok {
		return true
	}
	return isPlainModule(path) && authoredCSSImport.Match(source)
}

func isPlainModule(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".js" || ext == ".mjs"
}

// authoredCSSImport finds a stylesheet side-effect import as an author may
// write it. A match inside a string or comment costs only a needless transform.
var authoredCSSImport = regexp.MustCompile(`(?i)\bimport\s*["']\.\.?/[^"'\n]*\.css["']`)

func loaderFor(path string) (api.Loader, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx":
		return api.LoaderTSX, true
	case ".ts":
		return api.LoaderTS, true
	case ".jsx":
		return api.LoaderJSX, true
	default:
		return api.LoaderNone, false
	}
}

// Transform compiles source to an ES module. name labels the file in errors
// and source maps. The error lists every diagnostic as "file:line:column: text".
func Transform(name string, source []byte) ([]byte, error) {
	options := api.TransformOptions{
		JSX:        api.JSXAutomatic,
		Format:     api.FormatESModule,
		Target:     api.ES2022,
		Sourcefile: name,
		Sourcemap:  api.SourceMapInline,
	}
	if loader, ok := loaderFor(name); ok {
		options.Loader = loader
	} else if isPlainModule(name) {
		// Plain JavaScript keeps its syntax and module format; esbuild only
		// reprints it in the one form rewriteCSSImports matches.
		options.Loader, options.Format, options.Target = api.LoaderJS, api.FormatDefault, api.ESNext
	} else {
		return nil, fmt.Errorf("%s: not a transformable view script", name)
	}
	result := api.Transform(string(source), options)
	if len(result.Errors) == 0 {
		return rewriteCSSImports(result.Code), nil
	}
	lines := make([]string, 0, len(result.Errors))
	for _, message := range result.Errors {
		if location := message.Location; location != nil {
			lines = append(lines, fmt.Sprintf("%s:%d:%d: %s", location.File, location.Line, location.Column+1, message.Text))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", name, message.Text))
	}
	return nil, fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// CheckDir transforms every view script under dir in fsys and returns the
// diagnostics, labelled with their fsys paths. It is the whole-folder answer to
// "why did this view fail to load": a browser reports a broken import as a
// missing export, not as the syntax error behind it. Pass an fs.FS from os.Root
// so nothing it reads can leave the views folder.
// Scripts that cannot be read are reported separately as unreadable, since they
// were never checked.
func CheckDir(fsys fs.FS, dir string) (diagnostics, unreadable []string, err error) {
	err = viewconfig.WalkFolder(fsys, dir, func(rel string, _ fs.DirEntry) {
		if _, ok := loaderFor(rel); !ok && !isPlainModule(rel) {
			return
		}
		source, err := fs.ReadFile(fsys, rel)
		if err != nil {
			unreadable = append(unreadable, fmt.Sprintf("%s: cannot read view script: %v", rel, err))
			return
		}
		if !NeedsTransform(rel, source) {
			return
		}
		if _, err := Transform(rel, source); err != nil {
			diagnostics = append(diagnostics, err.Error())
		}
	})
	return diagnostics, unreadable, err
}
