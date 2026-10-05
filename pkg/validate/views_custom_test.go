package validate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func TestRunViewsReportsCustomViewScriptErrors(t *testing.T) {
	root := t.TempDir()
	views := root + "/.rhizome/views/poc/"
	definition := "apiVersion: rhizome.view.v1\nid: ID\nname: Board\nsource:\n  kind: custom\n  entry: board.tsx\nmount:\n  kind: standalone\n"
	// Two views share the folder; its broken component is reported once.
	writeValidationTestFile(t, views+"board.yaml", strings.Replace(definition, "ID", "poc.board", 1))
	writeValidationTestFile(t, views+"radar.yaml", strings.Replace(definition, "ID", "poc.radar", 1))
	writeValidationTestFile(t, views+"board.tsx", `import { Card } from "./components/Card.tsx"; export default () => <Card />;`)
	writeValidationTestFile(t, views+"components/Card.tsx", "export const Card = () => <div>;")

	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})

	require.False(t, result.OK)
	require.Equal(t, 1, result.IssueCount)
	require.Equal(t, "custom_view_script_error", result.Issues[0].Code)
	require.Contains(t, result.Issues[0].Message, "poc/components/Card.tsx:1:")
}

func TestRunViewsReportsANestedBrokenScriptOnce(t *testing.T) {
	root := t.TempDir()
	views := root + "/.rhizome/views/"
	definition := "apiVersion: rhizome.view.v1\nid: ID\nname: View\nsource:\n  kind: custom\n  entry: main.tsx\nmount:\n  kind: standalone\n"
	writeValidationTestFile(t, views+"outer/outer.yaml", strings.Replace(definition, "ID", "a.outer", 1))
	writeValidationTestFile(t, views+"outer/main.tsx", "export default null")
	writeValidationTestFile(t, views+"outer/inner/inner.yaml", strings.Replace(definition, "ID", "z.inner", 1))
	writeValidationTestFile(t, views+"outer/inner/main.tsx", "export const = ;")

	result := RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})

	require.Equal(t, 1, result.IssueCount)
	require.Contains(t, result.Issues[0].Message, "outer/inner/main.tsx:1:")
	// The outer view sorts first; the finding must still belong to the inner one.
	require.Equal(t, "z.inner", result.Issues[0].Target, "the nearest view owns a nested script's finding")
}

func runViewsAt(root string) CheckResult {
	return RunViews(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  20,
	})
}

func issueMessages(result CheckResult, code string) []string {
	var messages []string
	for _, issue := range result.Issues {
		if issue.Code == code {
			messages = append(messages, issue.Message)
		}
	}
	return messages
}

func TestRunViewsReportsImportsABrowserCannotLoad(t *testing.T) {
	root := t.TempDir()
	views := root + "/.rhizome/views/poc/"
	writeValidationTestFile(t, views+"board.yaml", "apiVersion: rhizome.view.v1\nid: poc.board\nname: Board\nsource: {kind: custom, entry: board.tsx}\nmount: {kind: standalone}\n")
	writeValidationTestFile(t, views+"board.tsx", `import { useGraphQL } from "@rhizome/kit";
import { Card } from "./Card";
import { Row } from "./parts/row.tsx";
import { Gone } from "../shared/gone.ts";
import "./board.css";
import { chunk } from "lodash";
import { secret } from "./.env.js";
export default function Board() { return <Card>{Row}{Gone}{chunk}{secret}{useGraphQL}</Card>; }`)
	writeValidationTestFile(t, views+".env.js", "export const secret = 1;")
	writeValidationTestFile(t, views+"Card.tsx", "export const Card = () => null;")
	writeValidationTestFile(t, views+"parts/row.tsx", "export const Row = 1;")
	writeValidationTestFile(t, views+"board.css", "p{}")
	writeValidationTestFile(t, views+"board.test.tsx", `import { it } from "vitest"; it("is never loaded", () => {});`)
	writeValidationTestFile(t, views+"__fixtures__/harness.tsx", `import { vi } from "vitest"; import { fake } from "../../../web/src/test/fake.ts"; export const harness = { vi, fake };`)

	result := runViewsAt(root)

	require.False(t, result.OK)
	require.Equal(t, []string{
		`poc/board.tsx: import "./Card" does not resolve to a servable file in the views folder`,
		`poc/board.tsx: import "../shared/gone.ts" does not resolve to a servable file in the views folder`,
		`poc/board.tsx: import "./.env.js" does not resolve to a servable file in the views folder`,
	}, issueMessages(result, "custom_view_import_unresolved"))
	unmapped := issueMessages(result, "custom_view_import_unmapped")
	require.Len(t, unmapped, 1, "kit imports pass and test files and fixtures are not checked")
	require.Contains(t, unmapped[0], `poc/board.tsx: import "lodash" is not in the kit import map (`)
	require.Contains(t, unmapped[0], "@rhizome/kit")
	require.Equal(t, "poc.board", result.Issues[0].Target)
}

func TestRunViewsValidatesLiteralGraphQLAgainstTheQuerySchema(t *testing.T) {
	root := t.TempDir()
	writeValidationTestFile(t, root+"/.rhizome/ontology/schema.graphql", `type Doc @node(paths: ["*.md"]) { title: String @field }`)
	views := root + "/.rhizome/views/poc/"
	writeValidationTestFile(t, views+"board.yaml", "apiVersion: rhizome.view.v1\nid: poc.board\nname: Board\nsource: {kind: custom, entry: board.tsx}\nmount: {kind: standalone}\n")
	writeValidationTestFile(t, views+"board.tsx", "import { graphql, useGraphQL } from \"@rhizome/kit\";\n"+
		"const type = \"doc\";\n"+
		"export default function Board() {\n"+
		"  useGraphQL(`query { doc(first: 5) { title } }`);\n"+
		"  useGraphQL(`query Q($path: String!) { note(path: $path) { path } }`, { path: \"a.md\" });\n"+
		"  useGraphQL(`query Page($n: Int!, $find: String) { doc(first: $n, find: $find) { title } }`, { n: 5 });\n"+
		"  useGraphQL(`query { ${type}(first: 5) { nope } }`);\n"+
		"  void graphql(\"query { doc { subtitle } }\");\n"+
		"  return null;\n"+
		"}\n")

	result := runViewsAt(root)

	invalid := issueMessages(result, "custom_view_graphql_invalid")
	require.Len(t, invalid, 1, "valid, variable-bound, and interpolated documents pass: %v", result.Issues)
	require.Contains(t, invalid[0], "poc/board.tsx: GraphQL passed to graphql is invalid:")
	require.Contains(t, invalid[0], `"subtitle"`)
}

func TestBundledViewsUseOnlyTheKitAndTheirOwnFiles(t *testing.T) {
	bundled := os.DirFS(filepath.Join("..", "..", "web", "bundled-views"))
	problems := customViewReferenceProblems(bundled, ".", nil)
	require.Empty(t, problems.unresolved)
	require.Empty(t, problems.unmapped)
}
