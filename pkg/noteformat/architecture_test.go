package noteformat_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
)

const modulePrefix = "github.com/atomicobject/rhizome/"

func TestSyntaxNeutralConsumersDoNotSelectConcreteProviders(t *testing.T) {
	repo := repositoryRoot(t)
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	selectors := make(map[string]struct{})
	for _, id := range registry.IDs() {
		selectors[strings.ToLower(string(id))] = struct{}{}
		provider, ok := registry.Provider(id)
		if !ok {
			t.Fatalf("registered provider %q is unavailable", id)
		}
		for _, extension := range provider.Descriptor().Extensions {
			selectors[strings.ToLower(extension)] = struct{}{}
		}
	}

	var violations []string
	for _, path := range auditedGoFiles(t, repo) {
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			t.Fatal(err)
		}
		rel = filepath.ToSlash(rel)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		if !providerCompositionFile(rel) && importsConcreteProvider(file) {
			violations = append(violations, rel+": imports a concrete note provider")
		}
		violations = append(violations, legacyMarkdownResolverCalls(file, rel)...)
		violations = append(violations, directMarkdownParserCalls(file, rel)...)
		violations = append(violations, indirectMarkdownParserCalls(file, rel)...)
		violations = append(violations, providerSelectionControlFlow(file, rel, selectors)...)
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("syntax-neutral consumers must use registry descriptors, capabilities, and catalog ownership:\n%s", strings.Join(violations, "\n"))
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository go.mod not found")
		}
		dir = parent
	}
}

func auditedGoFiles(t *testing.T, root string) []string {
	t.Helper()
	files := make([]string, 0)
	for _, dir := range []string{
		"cmd", "pkg/anchors", "pkg/app", "pkg/notemeta", "pkg/ontology",
		"pkg/search", "pkg/validate", "pkg/vault/cache", "pkg/vault/coderefs",
	} {
		walkRoot := filepath.Join(root, dir)
		err := filepath.WalkDir(walkRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func providerCompositionFile(path string) bool {
	switch path {
	case "cmd/note_metadata.go", "pkg/app/bootstrap/codex.go", "pkg/app/bootstrap/live.go":
		return true
	default:
		return false
	}
}

func importsConcreteProvider(file *ast.File) bool {
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, "\"")
		if path == modulePrefix+"pkg/noteformat/builtin" ||
			path == modulePrefix+"pkg/noteformat/markdown" ||
			path == modulePrefix+"pkg/noteformat/html" {
			return true
		}
	}
	return false
}

func providerSelectionControlFlow(file *ast.File, path string, selectors map[string]struct{}) []string {
	constants := namedStringConstants(file)
	violations := make([]string, 0)
	ast.Inspect(file, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.IfStmt:
			if !providerSelectionBoundaryFunction(path, functionNameAt(file, current.Pos())) && expressionSelectsProvider(current.Cond, selectors, constants) {
				violations = append(violations, path+": control flow selects a concrete note provider")
			}
		case *ast.SwitchStmt:
			if !providerSelectionBoundaryFunction(path, functionNameAt(file, current.Pos())) {
				for _, statement := range current.Body.List {
					clause, ok := statement.(*ast.CaseClause)
					if ok && expressionsSelectProvider(clause.List, current.Tag, selectors, constants) {
						violations = append(violations, path+": control flow selects a concrete note provider")
					}
				}
			}
		}
		return true
	})
	return violations
}

func namedStringConstants(file *ast.File) map[string]string {
	constants := make(map[string]string)
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.GenDecl)
		if !ok || declaration.Tok != token.CONST {
			return true
		}
		for _, spec := range declaration.Specs {
			values := spec.(*ast.ValueSpec)
			for i, name := range values.Names {
				if i < len(values.Values) {
					if literal, ok := values.Values[i].(*ast.BasicLit); ok && literal.Kind == token.STRING {
						constants[name.Name] = strings.Trim(literal.Value, "\"`")
					}
				}
			}
		}
		return false
	})
	return constants
}

func legacyMarkdownResolverCalls(file *ast.File, path string) []string {
	pathAliases := importAliases(file, modulePrefix+"pkg/paths")
	if len(pathAliases) == 0 {
		return nil
	}
	legacy := map[string]struct{}{
		"ResolveNoteInput":               {},
		"ResolveNoteInputWithVaultPaths": {},
		"ResolveNoteRef":                 {},
		"ResolveNoteRefWithVaultPaths":   {},
	}
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !selectorUsesImportAlias(selector, pathAliases) {
			return true
		}
		if _, ok := legacy[selector.Sel.Name]; ok && !markdownPathCompatibilityFunction(path, functionNameAt(file, call.Pos())) {
			violations = append(violations, path+": uses deprecated Markdown resolver paths."+selector.Sel.Name)
		}
		return true
	})
	return violations
}

func directMarkdownParserCalls(file *ast.File, path string) []string {
	obsidianAliases := importAliases(file, modulePrefix+"pkg/vault/obsidian")
	if len(obsidianAliases) == 0 {
		return nil
	}
	parsers := map[string]struct{}{
		"EnumerateHeadings":                {},
		"EnumerateMarkdownTargets":         {},
		"ExtractFrontmatter":               {},
		"ExtractHashtags":                  {},
		"ExtractInlineProperties":          {},
		"ExtractInlinePropertyOccurrences": {},
		"ExtractMdLinks":                   {},
		"FirstMarkdownH1Title":             {},
		"FrontmatterRegex":                 {},
		"NormalizePath":                    {},
		"NormalizeWithDefaultExt":          {},
		"ScanAllLinks":                     {},
		"ScanStructuredLinks":              {},
		"ScanStructuredLinkSnapshot":       {},
		"ScanWikilinks":                    {},
		"SingleMarkdownH1Title":            {},
	}
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !selectorUsesImportAlias(selector, obsidianAliases) {
			return true
		}
		if _, ok := parsers[selector.Sel.Name]; ok && !markdownParserBoundaryFunction(path, functionNameAt(file, call.Pos())) {
			violations = append(violations, path+": calls Markdown parser obsidian."+selector.Sel.Name)
		}
		return true
	})
	return violations
}

func indirectMarkdownParserCalls(file *ast.File, path string) []string {
	codeAnchorAliases := importAliases(file, modulePrefix+"pkg/anchors")
	sectionAliases := importAliases(file, modulePrefix+"pkg/ontology/sectionparse")
	ontologyAliases := importAliases(file, modulePrefix+"pkg/ontology")
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		function := functionNameAt(file, call.Pos())
		if indirectMarkdownParserBoundaryFunction(path, function) {
			return true
		}
		switch callee := call.Fun.(type) {
		case *ast.SelectorExpr:
			switch {
			case callee.Sel.Name == "ParseNote" && selectorUsesImportAlias(callee, codeAnchorAliases):
				violations = append(violations, path+": calls Markdown parser codeanchor.ParseNote")
			case callee.Sel.Name == "Parse" && selectorUsesImportAlias(callee, sectionAliases):
				violations = append(violations, path+": calls Markdown parser sectionparse.Parse")
			case callee.Sel.Name == "BuildDocumentSnapshot" && selectorUsesImportAlias(callee, ontologyAliases):
				violations = append(violations, path+": calls Markdown parser ontology.BuildDocumentSnapshot")
			}
		case *ast.Ident:
			if strings.HasPrefix(path, "pkg/ontology/") && callee.Name == "BuildDocumentSnapshot" {
				violations = append(violations, path+": calls Markdown parser BuildDocumentSnapshot")
			}
		}
		return true
	})
	return violations
}

func functionNameAt(file *ast.File, position token.Pos) string {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Pos() <= position && position <= function.End() {
			return function.Name.Name
		}
	}
	return ""
}

func markdownPathCompatibilityFunction(path, function string) bool {
	// These functions preserve legacy Markdown CLI behavior while it remains an
	// explicit authoring boundary. Generic readers must use the neutral path
	// resolver and are intentionally not listed here.
	switch path + ":" + function {
	case "pkg/app/cli/properties_mutate.go:processSetProperty",
		"pkg/app/cli/properties_mutate.go:processDeleteProperties",
		"pkg/app/cli/properties_mutate.go:processRenameProperties",
		"pkg/app/cli/delete.go:DeleteNote",
		"pkg/app/cli/tags_mutate.go:processDeleteFile",
		"pkg/app/cli/tags_mutate.go:processRenameFile",
		"pkg/app/cli/tags_mutate.go:processAddFile",
		"pkg/ontology/edit_session.go:writeStatesAtomically":
		return true
	default:
		return false
	}
}

func markdownParserBoundaryFunction(path, function string) bool {
	switch path + ":" + function {
	case "pkg/notemeta/index.go:readNoteEntry",
		"pkg/notemeta/index.go:singleMarkdownH1Title",
		"pkg/notemeta/index.go:extractAllTags",
		"pkg/ontology/projection.go:BuildDocumentSnapshot", "pkg/ontology/projection.go:newProjectionResolver",
		"pkg/ontology/projection_inline.go:SectionInlineProperties", "pkg/ontology/projection_bindings.go:frontmatterRange",
		"pkg/app/cli/note_move_plan.go:planNoteMoves",
		"pkg/app/cli/note_namespace_plan.go:namespaceHeadingPointers", "pkg/app/cli/heading_rename.go:RenameHeading",
		"pkg/app/cli/heading_rename.go:replaceExactWikilinkTarget",
		"pkg/app/cli/properties_mutate.go:processDeleteProperties", "pkg/app/cli/properties_mutate.go:processRenameProperties",
		"pkg/app/cli/tags_mutate.go:hasTag",
		"pkg/app/cli/init/migration.go:referencedLegacyProcessDocs",
		"pkg/app/cli/init/migration.go:archiveActiveLegacyProcessSpec",
		"pkg/app/web/markdown_compat.go:resolveMarkdownLinksCompat",
		"pkg/app/web/markdown_compat.go:markdownEmbedsCompat",
		"pkg/app/web/markdown_compat.go:markdownAliasesForNotePathCompat",
		"pkg/app/web/markdown_compat.go:scanStructuredMarkdownLinksCompat",
		"pkg/ontology/markdown_links.go:ScanMarkdownBodyLinks",
		"pkg/ontology/markdown_links.go:MarkdownProjectionStructuredLinks",
		"pkg/vault/cache/note_projection.go:markdownCompatibilityEntry",
		"pkg/validate/alias_cache.go:aliasMapFromNotes", "pkg/validate/apply_edits.go:ApplyAliasAppendInMemory",
		"pkg/validate/check_code_frontmatter.go:RunCodeFrontmatter",
		"pkg/validate/check_fragile_external.go:RunFragileExternal", "pkg/validate/check_fragile_external.go:normalizeScopePath",
		"pkg/validate/check_link_hygiene.go:RunLinkHygiene", "pkg/validate/check_link_hygiene.go:buildLinkHygieneIndex",
		"pkg/validate/check_orphan_block_ids.go:RunOrphanBlockIDs",
		"pkg/validate/fix_code_anchors.go:replaceCodeAnchorTargetInMemory",
		"pkg/validate/identifier_block_ids.go:rewriteBlockIDReferenceEdits",
		"pkg/validate/identifierreconcile/link_discovery_build.go:DiscoverIdentifierLinks",
		"pkg/validate/identifierreconcile/link_provenance_git.go:indexHistoricalBareLinks",
		"pkg/validate/identifierreconcile/provenance_claim.go:parseHistoricalMarkdownDocument",
		"pkg/validate/lifecycle_policy.go:ClassifyLifecycleEdit", "pkg/validate/lifecycle_policy.go:lifecycleHistorySectionEnd",
		"pkg/vault/coderefs/scanner.go:ScanFile":
		return true
	default:
		return false
	}
}

func indirectMarkdownParserBoundaryFunction(path, function string) bool {
	switch path + ":" + function {
	case "cmd/code.go:markdownCodeAnchorParseCompat",
		"pkg/app/web/markdown_compat.go:markdownDocumentSnapshotCompat",
		"pkg/app/web/markdown_compat.go:buildMarkdownDocumentSnapshotCompat",
		"pkg/app/codeintel/ingest.go:parseMarkdownCodeAnchorValidationSource",
		"pkg/app/codeintel/ingest.go:validateMarkdownCodeAnchorSyntax",
		"pkg/ontology/projection.go:LoadDocumentSnapshot",
		"pkg/ontology/projection.go:BuildDocumentSnapshot",
		"pkg/ontology/sections.go:ParseSections",
		"pkg/anchors/intel_extract_notes.go:extractMarkdownIntelDocSections",
		"pkg/anchors/note_source.go:ExtractAnchorDeclarations",
		"pkg/app/codeintel/ingest.go:ingestMarkdownCandidateRun",
		"pkg/ontology/index.go:projectMarkdownNoteSource",
		"pkg/ontology/index.go:documentSnapshotForDoc",
		"pkg/ontology/edit_session.go:documentSnapshot",
		"pkg/ontology/noderead/records.go:snapshotForPathLocked",
		"pkg/ontology/noderead/records.go:snapshot",
		"pkg/ontology/noderead/overlay_index.go:addOverlayPath",
		"pkg/ontology/query/loaders.go:snapshot",
		"pkg/ontology/query/execute.go:projectNoteRecord",
		"pkg/ontology/sync.go:sectionBodyByHeading",
		"pkg/validate/check_code_frontmatter.go:RunCodeFrontmatter",
		"pkg/validate/check_fragile_external.go:RunFragileExternal",
		"pkg/validate/check_orphan_block_ids.go:RunOrphanBlockIDs",
		"pkg/validate/identifier_block_ids.go:RunIdentifierBlockIDMigration",
		"pkg/validate/identifierreconcile/provenance_claim.go:parseHistoricalMarkdownDocument",
		"pkg/validate/identifierreconcile/source_discovery.go:discoverIdentifierRepairSource":
		return true
	default:
		return false
	}
}

func providerSelectionBoundaryFunction(path, function string) bool {
	// A short set of named compatibility functions still select Markdown
	// authoring syntax. They are deliberately separate from parser exemptions:
	// adding an extension literal elsewhere remains a neutral-consumer failure.
	switch path + ":" + function {
	case "pkg/app/cli/init/detect.go:hasMarkdown", "pkg/app/cli/init/detect_notes.go:scanMarkdownDirCandidate",
		"pkg/app/cli/init/migration.go:retireLegacyProcessDocsWithCatalog",
		"pkg/app/cli/init/migration.go:referencedLegacyProcessDocs", "pkg/app/cli/init/migration.go:canonicalCachedNotePath",
		"pkg/app/cli/init/helper_templates.go:readSkillTemplate", "pkg/app/cli/init/helper_templates.go:loadStarterTemplates",
		"pkg/app/cli/init/helper_templates.go:normalizeEmbedTarget", "pkg/app/cli/init/template_includes.go:ensureIncludesCoverTemplatePaths",
		"pkg/app/cli/file_context.go:indexedAliasLookupValue",
		"pkg/app/cli/note_namespace_plan.go:PlanNamespaceMutation",
		"pkg/app/cli/note_namespace_plan.go:setNamespaceBasenameUniqueness",
		"pkg/app/cli/note_namespace_plan.go:namespaceHeadingPointers",
		"pkg/app/codeintel/ingest.go:ingestNotes", "pkg/app/codeintel/ingest.go:ValidateNotesWithWriter",
		"pkg/app/codeintel/ingest.go:ValidateMarkdownCodeAnchorSyntaxWithWriter",
		"pkg/app/codeintel/ingest.go:validateMarkdownCodeAnchorSyntax",
		"pkg/notemeta/source_snapshot_adapt.go:normalizeContentOnlyNotePath",
		"pkg/ontology/query/execute.go:projectNoteRecord",
		"pkg/ontology/queryrecipe/load.go:loadFile", "pkg/ontology/queryrecipe/load.go:isRecipeFile",
		"pkg/ontology/queryrecipe/load.go:isRecipeFileForScan", "pkg/ontology/reference/link_rewrite.go:stripMarkdownExtension",
		"pkg/search/embeddings/indexer.go:scanVaultByWalk",
		"pkg/validate/check_link_hygiene.go:wikilinkReplacement", "pkg/validate/check_link_hygiene.go:isMarkdownNoteLink",
		"pkg/validate/check_fragile_external.go:normalizeScopePath",
		"pkg/validate/identifierreconcile/repair_intents_validation.go:stripRepairMarkdownExtension":
		return true
	case "pkg/app/agentchat/tools.go:isWorkspaceDocPath",
		"pkg/app/mcp/semantic_query_mode.go:isMarkdownDocQueryCompatibilityPath",
		"pkg/app/unifiedsearch/run.go:isExplicitMarkdownRawSeedCompatibilityPath",
		"pkg/search/locality.go:isExplicitMarkdownRawSeedCompatibilityPath",
		"pkg/app/views/execute.go:markdownLinkFilterValuesEqualCompat",
		"pkg/vault/cache/selection_policy.go:MarkdownCompatibilityAdmission",
		"pkg/validate/identifierreconcile/provenance_git.go:isHistoricalMarkdownCompatibilityPath":
		return true
	default:
		return false
	}
}

func importAliases(file *ast.File, importPath string) map[string]struct{} {
	aliases := make(map[string]struct{})
	for _, spec := range file.Imports {
		if strings.Trim(spec.Path.Value, "\"") != importPath {
			continue
		}
		if spec.Name == nil {
			aliases[filepath.Base(importPath)] = struct{}{}
			continue
		}
		if spec.Name.Name != "_" && spec.Name.Name != "." {
			aliases[spec.Name.Name] = struct{}{}
		}
	}
	return aliases
}

func selectorUsesImportAlias(selector *ast.SelectorExpr, aliases map[string]struct{}) bool {
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = aliases[ident.Name]
	return ok
}
