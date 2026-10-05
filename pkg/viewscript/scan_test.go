package viewscript

import (
	"reflect"
	"strings"
	"testing"
)

func TestScanFindsImportsAndLiteralGraphQL(t *testing.T) {
	source := "import { useGraphQL, graphql } from '@rhizome/kit';\n" +
		"import {\n  Alpha, Beta,\n  Gamma,\n} from \"./parts/index.tsx\";\n" +
		"import \"./view.css\";\n" +
		"import type { Row } from \"./types\";\n" +
		"export { helper } from '../shared/helper.ts';\n" +
		"const lazy = () => import('./lazy.tsx');\n" +
		"const name = 'Spec';\n" +
		"export default function View() {\n" +
		"  const a = useGraphQL<{ x: Row }>(`query Q($id: ID!) { node(id: $id) { id } }`, { id: '1' });\n" +
		"  const b = useGraphQL(`query { ${name} { id } }`);\n" +
		"  void graphql(\"query { notes { path } }\");\n" +
		"  return <div>{Alpha}{Beta}{Gamma}{lazy}{a}{b}</div>;\n" +
		"}\n"
	refs, err := Scan("poc/view.tsx", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	wantImports := []string{"react/jsx-runtime", "@rhizome/kit", "./parts/index.tsx", "../shared/helper.ts", "./view.css", "./lazy.tsx"}
	if !reflect.DeepEqual(refs.Imports, wantImports) {
		t.Fatalf("imports\n got %q\nwant %q", refs.Imports, wantImports)
	}
	wantGraphQL := []GraphQLDocument{
		{Callee: "useGraphQL", Query: "query Q($id: ID!) { node(id: $id) { id } }"},
		{Callee: "graphql", Query: "query { notes { path } }"},
	}
	if !reflect.DeepEqual(refs.GraphQL, wantGraphQL) {
		t.Fatalf("graphql\n got %#v\nwant %#v", refs.GraphQL, wantGraphQL)
	}
}

func TestScanReadsPlainJavaScriptModules(t *testing.T) {
	refs, err := Scan("poc/lib.js", []byte(`import x from "./x.js"; export default x;`))
	if err != nil || !reflect.DeepEqual(refs.Imports, []string{"./x.js"}) {
		t.Fatalf("%v %q", err, refs.Imports)
	}
}

func TestClassifyAndResolveImports(t *testing.T) {
	for specifier, want := range map[string]ImportKind{
		"./a.tsx": ImportRelative, "../b.ts": ImportRelative, "@rhizome/kit": ImportBare, "lodash": ImportBare,
		"/views/_files/x.js": ImportURL, "https://esm.sh/x": ImportURL,
	} {
		if got := ClassifyImport(specifier); got != want {
			t.Errorf("%s: %v, want %v", specifier, got, want)
		}
	}
	if got := ResolveRelative("poc/sub/view.tsx", "../style.css?rhizome-css"); got != "poc/style.css" {
		t.Fatalf("resolved %q", got)
	}
	if !IsKitImport("@rhizome/kit") || !IsKitImport("react/jsx-runtime") || IsKitImport("lodash") {
		t.Fatal("kit import map")
	}
}

func TestTransformRewritesOnlyRelativeStylesheetSideEffects(t *testing.T) {
	out, err := Transform("poc/view.tsx", []byte(`import "./a.css"; import "pkg/b.css"; import c from "./c.css"; export default c;`))
	if err != nil {
		t.Fatal(err)
	}
	code := string(out)
	for want, present := range map[string]bool{
		`import "./a.css?rhizome-css";`: true,
		`import "pkg/b.css";`:           true,
		`from "./c.css";`:               true,
		`"./c.css?rhizome-css"`:         false,
	} {
		if contains := strings.Contains(code, want); contains != present {
			t.Fatalf("%s present=%v in:\n%s", want, contains, code)
		}
	}
	if !IsCSSModuleRequest("poc/a.css", "rhizome-css") || IsCSSModuleRequest("poc/a.css", "") || IsCSSModuleRequest("poc/a.tsx", "rhizome-css") {
		t.Fatal("css module request detection")
	}
}
