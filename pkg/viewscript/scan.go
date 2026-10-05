package viewscript

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// ScriptReferences is what `rzm validate views` checks in one view script:
// the modules it imports and the GraphQL documents it passes literally to the
// kit's graphql or useGraphQL.
type ScriptReferences struct {
	Imports []string
	GraphQL []GraphQLDocument
}

type GraphQLDocument struct {
	Callee string
	Query  string
}

// esbuild prints imports and string literals in one canonical form whatever the
// author wrote: double-quoted specifiers, `from "x"` closing multi-line import
// lists, and template literals kept as written. Matching its output instead of
// the source keeps these patterns small. Unused TypeScript imports are already
// gone, as they are in the module the browser loads.
var (
	staticImport  = regexp.MustCompile(`(?m)^(?:import|export)\b[^;"'` + "`" + `]*?\bfrom\s*"((?:[^"\\\n]|\\.)*)"`)
	bareImport    = regexp.MustCompile(`(?m)^import\s*"((?:[^"\\\n]|\\.)*)"`)
	dynamicImport = regexp.MustCompile(`\bimport\(\s*"((?:[^"\\\n]|\\.)*)"\s*\)`)
	graphQLCall   = regexp.MustCompile(`\b(graphql|useGraphQL)\(\s*(` + "`" + `(?:[^` + "`" + `\\$]|\\[\s\S]|\$[^{` + "`" + `])*` + "`" + `|"(?:[^"\\\n]|\\.)*")`)
)

// Scan reports the imports and literal GraphQL documents of a view script.
// Documents built with interpolation are not literal and are skipped.
func Scan(name string, source []byte) (ScriptReferences, error) {
	loader, ok := loaderFor(name)
	if ext := strings.ToLower(path.Ext(name)); !ok && (ext == ".js" || ext == ".mjs") {
		loader, ok = api.LoaderJS, true
	}
	if !ok {
		return ScriptReferences{}, fmt.Errorf("%s: not a view script", name)
	}
	result := api.Transform(string(source), api.TransformOptions{
		Loader: loader, JSX: api.JSXAutomatic, Format: api.FormatESModule, Target: api.ES2022, Sourcefile: name,
	})
	if len(result.Errors) > 0 {
		return ScriptReferences{}, fmt.Errorf("%s: %s", name, result.Errors[0].Text)
	}
	code := string(result.Code)
	var refs ScriptReferences
	seen := map[string]bool{}
	for _, pattern := range []*regexp.Regexp{staticImport, bareImport, dynamicImport} {
		for _, match := range pattern.FindAllStringSubmatch(code, -1) {
			specifier, err := strconv.Unquote(`"` + match[1] + `"`)
			if err != nil || seen[specifier] {
				continue
			}
			seen[specifier] = true
			refs.Imports = append(refs.Imports, specifier)
		}
	}
	for _, match := range graphQLCall.FindAllStringSubmatch(code, -1) {
		literal := match[2]
		var query string
		if strings.HasPrefix(literal, "`") {
			query = templateLiteralReplacer.Replace(literal[1 : len(literal)-1])
		} else if unquoted, err := strconv.Unquote(literal); err == nil {
			query = unquoted
		} else {
			continue
		}
		refs.GraphQL = append(refs.GraphQL, GraphQLDocument{Callee: match[1], Query: query})
	}
	return refs, nil
}

var templateLiteralReplacer = strings.NewReplacer("\\`", "`", "\\$", "$", "\\\\", "\\", "\\n", "\n", "\\t", "\t")

// ImportKind classifies a specifier the way the browser resolves it.
type ImportKind int

const (
	ImportRelative ImportKind = iota // ./ or ../, a file beside the importer
	ImportBare                       // resolved only through the kit's import map
	ImportURL                        // an absolute path or URL, resolved by the browser as written
)

func ClassifyImport(specifier string) ImportKind {
	switch {
	case strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../"):
		return ImportRelative
	case strings.HasPrefix(specifier, "/") || strings.Contains(specifier, ":"):
		return ImportURL
	default:
		return ImportBare
	}
}

// ResolveRelative returns the slash path a relative specifier names from the
// importing file, without its query or fragment.
func ResolveRelative(importer, specifier string) string {
	if cut := strings.IndexAny(specifier, "?#"); cut >= 0 {
		specifier = specifier[:cut]
	}
	return path.Join(path.Dir(importer), specifier)
}
