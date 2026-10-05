package validate

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/viewscript"
)

// customViewScriptIssues transforms the scripts in each custom view's folder so
// a broken view is reported here, without opening a browser, and checks what
// they import and query. Views that share a folder share its scripts, so each
// folder is checked once. A folder's scan also reaches view folders nested
// inside it; checking the deepest folders first leaves each script's finding
// with the nearest view that owns it. A nil execSchema skips GraphQL checks.
func customViewScriptIssues(vaultPath string, views []viewconfig.ViewDefinition, execSchema *ontologyquery.ExecutableSchema) []viewconfig.Issue {
	root := viewconfig.DefaultSourceRoot(vaultPath)
	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil
	}
	defer fsRoot.Close()
	custom := make([]viewconfig.ViewDefinition, 0, len(views))
	for _, view := range views {
		if view.SourceSpec.Kind == viewconfig.SourceKindCustom && view.Source.Path != "" {
			custom = append(custom, view)
		}
	}
	sort.SliceStable(custom, func(i, j int) bool {
		return folderDepth(custom[i].Source.Path) > folderDepth(custom[j].Source.Path)
	})
	checked := map[string]bool{}
	reported := map[string]bool{}
	var issues []viewconfig.Issue
	for _, view := range custom {
		folder, err := filepath.Rel(root, filepath.Dir(view.Source.Path))
		if err != nil || checked[folder] {
			continue
		}
		checked[folder] = true
		diagnostics, unreadable, err := viewscript.CheckDir(fsRoot.FS(), filepath.ToSlash(folder))
		if err != nil {
			unreadable = append(unreadable, fmt.Sprintf("%s: cannot read view folder: %v", filepath.ToSlash(folder), err))
		}
		issues = append(issues, customViewIssues(view, "custom_view_script_error", unseen(reported, diagnostics))...)
		// A script that could not be read was never checked; saying nothing
		// would pass a view that may not load.
		issues = append(issues, customViewIssues(view, "custom_view_read_error", unseen(reported, unreadable))...)
		refs := customViewReferenceProblems(fsRoot.FS(), filepath.ToSlash(folder), execSchema)
		issues = append(issues, customViewIssues(view, "custom_view_import_unresolved", unseen(reported, refs.unresolved))...)
		issues = append(issues, customViewIssues(view, "custom_view_import_unmapped", unseen(reported, refs.unmapped))...)
		issues = append(issues, customViewIssues(view, "custom_view_graphql_invalid", unseen(reported, refs.graphQL))...)
	}
	return issues
}

type viewReferenceProblems struct {
	unresolved, unmapped, graphQL []string
}

// customViewReferenceProblems checks each script a browser may load: relative
// imports must name a file (the browser adds no extension), bare imports must
// be in the kit's import map, and GraphQL documents passed literally to graphql
// or useGraphQL must prepare against the vault's query schema. Scripts that do
// not transform or cannot be read are CheckDir's to report, and test files and
// __fixtures__ folders are never loaded (the kit build leaves both out of the
// bundled views), so all of these are skipped here.
func customViewReferenceProblems(fsys fs.FS, dir string, execSchema *ontologyquery.ExecutableSchema) viewReferenceProblems {
	var problems viewReferenceProblems
	_ = viewconfig.WalkFolder(fsys, dir, func(rel string, _ fs.DirEntry) {
		if strings.Contains(path.Base(rel), ".test.") || slices.Contains(strings.Split(rel, "/"), "__fixtures__") {
			return
		}
		source, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return
		}
		refs, err := viewscript.Scan(rel, source)
		if err != nil {
			return
		}
		for _, specifier := range refs.Imports {
			switch viewscript.ClassifyImport(specifier) {
			case viewscript.ImportRelative:
				target := viewscript.ResolveRelative(rel, specifier)
				if info, err := fs.Stat(fsys, target); err != nil || !info.Mode().IsRegular() || !viewconfig.ServablePath(target) {
					problems.unresolved = append(problems.unresolved, fmt.Sprintf("%s: import %q does not resolve to a servable file in the views folder", rel, specifier))
				}
			case viewscript.ImportBare:
				if !viewscript.IsKitImport(specifier) {
					problems.unmapped = append(problems.unmapped, fmt.Sprintf("%s: import %q is not in the kit import map (%s)", rel, specifier, strings.Join(viewscript.KitImports(), ", ")))
				}
			}
		}
		if execSchema == nil {
			return
		}
		for _, document := range refs.GraphQL {
			if problem := graphQLDocumentProblem(execSchema, document.Query); problem != "" {
				problems.graphQL = append(problems.graphQL, fmt.Sprintf("%s: GraphQL passed to %s is invalid: %s", rel, document.Callee, problem))
			}
		}
	})
	return problems
}

// graphQLDocumentProblem prepares a view's query as the server would. Its
// variables arrive at run time, so each one without a default gets a sample
// that passes value checks (a positive first, a non-empty list); a document
// with a variable that cannot be sampled is not checked rather than reported
// falsely.
func graphQLDocumentProblem(execSchema *ontologyquery.ExecutableSchema, query string) string {
	document, err := parser.ParseQuery(&ast.Source{Input: query})
	if err != nil {
		return err.Error()
	}
	variables := map[string]any{}
	for _, operation := range document.Operations {
		for _, definition := range operation.VariableDefinitions {
			if definition.Type == nil || definition.DefaultValue != nil {
				continue
			}
			sample, ok := sampleGraphQLVariable(execSchema.Schema, definition.Type)
			if !ok {
				return ""
			}
			variables[definition.Variable] = sample
		}
	}
	_, errs := ontologyquery.PrepareWithVariables(execSchema, query, variables)
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		messages = append(messages, err.Message)
	}
	return strings.Join(messages, "; ")
}

func sampleGraphQLVariable(schema *ast.Schema, typ *ast.Type) (any, bool) {
	if typ.Elem != nil {
		element, ok := sampleGraphQLVariable(schema, typ.Elem)
		return []any{element}, ok
	}
	definition := schema.Types[typ.NamedType]
	switch {
	case definition == nil:
		return nil, false
	case definition.Kind == ast.Enum && len(definition.EnumValues) > 0:
		return definition.EnumValues[0].Name, true
	case definition.Kind != ast.Scalar:
		return nil, false
	}
	switch typ.NamedType {
	case "Int", "Float":
		return 1, true
	case "Boolean":
		return false, true
	default:
		return "sample", true
	}
}

func customViewIssues(view viewconfig.ViewDefinition, code string, messages []string) []viewconfig.Issue {
	issues := make([]viewconfig.Issue, 0, len(messages))
	for _, message := range messages {
		issues = append(issues, viewconfig.Issue{
			Code:     code,
			Severity: viewconfig.IssueFatal,
			View:     view.ID,
			Path:     view.Source.Path,
			Line:     view.Source.Line,
			Field:    "source.entry",
			Message:  message,
		})
	}
	return issues
}

func unseen(reported map[string]bool, messages []string) []string {
	var fresh []string
	for _, message := range messages {
		if !reported[message] {
			reported[message] = true
			fresh = append(fresh, message)
		}
	}
	return fresh
}

func folderDepth(definitionPath string) int {
	return strings.Count(filepath.ToSlash(filepath.Dir(definitionPath)), "/")
}
