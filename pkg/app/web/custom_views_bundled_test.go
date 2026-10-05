package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func getCustomViewWith(srv *Server, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for name, values := range header {
		req.Header[name] = values
	}
	rec := httptest.NewRecorder()
	srv.handleCustomViews(rec, req)
	return rec
}

func TestCustomViewFilesRevalidateWithETags(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx": `export default function Board() { return <p>first</p>; }`,
		"poc/data.json": `{"n":1}`,
	})
	for _, target := range []string{"/views/_files/poc/board.tsx", "/views/_files/poc/data.json"} {
		first := getCustomView(srv, target)
		etag := first.Header().Get("ETag")
		if first.Code != http.StatusOK || etag == "" || first.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d etag=%q cache=%q", target, first.Code, etag, first.Header().Get("Cache-Control"))
		}
		again := getCustomViewWith(srv, target, http.Header{"If-None-Match": {etag}})
		if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Fatalf("%s unchanged: want 304, got %d with %d bytes", target, again.Code, again.Body.Len())
		}
	}

	before := getCustomView(srv, "/views/_files/poc/board.tsx")
	board := filepath.Join(srv.customViewsRoot(), "poc", "board.tsx")
	if err := os.WriteFile(board, []byte(`export default function Board() { return <p>second</p>; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	after := getCustomViewWith(srv, "/views/_files/poc/board.tsx", http.Header{"If-None-Match": {before.Header().Get("ETag")}})
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), "second") || after.Header().Get("ETag") == before.Header().Get("ETag") {
		t.Fatalf("edited module: %d etag %q -> %q\n%s", after.Code, before.Header().Get("ETag"), after.Header().Get("ETag"), after.Body.String())
	}
	if shell := getCustomView(srv, "/views/poc.board"); shell.Header().Get("Cache-Control") != "no-store" || shell.Header().Get("ETag") != "" {
		t.Fatalf("shell must stay uncached: %q %q", shell.Header().Get("Cache-Control"), shell.Header().Get("ETag"))
	}
}

// A hit returns the module the first transform stored, the same bytes rather
// than esbuild's fresh output; changed source or another file name misses.
func TestViewModuleCacheSkipsTransformForUnchangedSource(t *testing.T) {
	var cache viewModuleCache
	source := []byte(`export default function Board() { return <p>one</p>; }`)
	first, err := cache.transform("poc/board.tsx", source)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := cache.transform("poc/board.tsx", bytes.Clone(source)); &again[0] != &first[0] {
		t.Fatal("an unchanged module was transformed again")
	}
	edited, _ := cache.transform("poc/board.tsx", []byte(`export default function Board() { return <p>two</p>; }`))
	if &edited[0] == &first[0] || !bytes.Contains(edited, []byte("two")) {
		t.Fatalf("an edited module was served from the cache:\n%s", edited)
	}
	if renamed, _ := cache.transform("poc/other.tsx", source); &renamed[0] == &first[0] {
		t.Fatal("the source map names the file, so another name must miss")
	}
}

func TestCustomViewCSSSideEffectImportAppliesStylesheet(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx": "import \"./board.css\";\nimport '../shared/base.css';\nexport default function Board() { return <p className=\"x\">hi</p>; }",
		"poc/board.css": ".x{color:red}",
	})
	module := getCustomView(srv, "/views/_files/poc/board.tsx").Body.String()
	for _, want := range []string{`import "./board.css?rhizome-css";`, `import "../shared/base.css?rhizome-css";`} {
		if !strings.Contains(module, want) {
			t.Fatalf("module missing %s:\n%s", want, module)
		}
	}
	shim := getCustomView(srv, "/views/_files/poc/board.css?rhizome-css")
	if shim.Code != http.StatusOK || !strings.HasPrefix(shim.Header().Get("Content-Type"), "text/javascript") ||
		!strings.Contains(shim.Body.String(), `link.rel = "stylesheet"`) || !strings.Contains(shim.Body.String(), "import.meta.url") {
		t.Fatalf("css module: %d %s\n%s", shim.Code, shim.Header().Get("Content-Type"), shim.Body.String())
	}
	if css := getCustomView(srv, "/views/_files/poc/board.css"); !strings.HasPrefix(css.Header().Get("Content-Type"), "text/css") || css.Body.String() != ".x{color:red}" {
		t.Fatalf("the stylesheet itself: %s %q", css.Header().Get("Content-Type"), css.Body.String())
	}
	if missing := getCustomView(srv, "/views/_files/poc/missing.css?rhizome-css"); missing.Code != http.StatusNotFound {
		t.Fatalf("a missing stylesheet must fail its import: %d", missing.Code)
	}
}

func TestPlainJavaScriptModuleCSSImportAppliesStylesheet(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.js", map[string]string{
		"poc/board.js":  "import './board.css'\nimport { part } from \"./part.mjs\";\nexport default function Board() { return part ?? null; }\n",
		"poc/part.mjs":  "import \"../shared/base.css\";\nexport const part = 1;\n",
		"poc/plain.js":  "export const untouched   =   1 // as written\n",
		"poc/board.css": ".x{}",
	})
	for target, want := range map[string]string{
		"/views/_files/poc/board.js": `import "./board.css?rhizome-css";`,
		"/views/_files/poc/part.mjs": `import "../shared/base.css?rhizome-css";`,
	} {
		module := getCustomView(srv, target)
		body := module.Body.String()
		if module.Code != http.StatusOK || !strings.HasPrefix(module.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(body, want) {
			t.Fatalf("%s: %d %s, want %s:\n%s", target, module.Code, module.Header().Get("Content-Type"), want, body)
		}
		if target == "/views/_files/poc/board.js" && !strings.Contains(body, "part ?? null") {
			t.Fatalf("plain JavaScript must keep its syntax:\n%s", body)
		}
	}
	if plain := getCustomView(srv, "/views/_files/poc/plain.js").Body.String(); plain != "export const untouched   =   1 // as written\n" {
		t.Fatalf("a module without stylesheet imports is served as written: %q", plain)
	}
}

const bundledBriefingYAML = `apiVersion: rhizome.view.v1
id: group.briefing
name: Briefing
source: {kind: custom, entry: briefing.tsx}
mount: {kind: group, group: "*"}
`

func newBundledViewTestServer(t *testing.T) *Server {
	t.Helper()
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	srv.assets = fstest.MapFS{
		customViewKitBoot:                   &fstest.MapFile{Data: []byte("//")},
		"bundled-views/group/briefing.yaml": &fstest.MapFile{Data: []byte(bundledBriefingYAML)},
		"bundled-views/group/briefing.tsx":  &fstest.MapFile{Data: []byte(`import "./briefing.css"; export default function Briefing() { return <p>embedded</p>; }`)},
		"bundled-views/group/briefing.css":  &fstest.MapFile{Data: []byte("p{}")},
	}
	addCustomViewTestSchema(t, srv)
	return srv
}

func groupShellURL(id string) string {
	return "/views/" + id + "?" + url.Values{"context": {`{"kind":"group","group":"Delivery"}`}}.Encode()
}

func TestBundledViewIsServedFromEmbeddedAssets(t *testing.T) {
	srv := newBundledViewTestServer(t)
	shell := getCustomView(srv, groupShellURL("group.briefing"))
	body := shell.Body.String()
	if shell.Code != http.StatusOK {
		t.Fatalf("shell: %d %s", shell.Code, body)
	}
	for _, want := range []string{
		`import("/views/_bundled/group/briefing.tsx")`,
		`"view":{"id":"group.briefing","name":"Briefing","origin":"bundled"}`,
		`"context":{"kind":"group","group":"Delivery"}`,
		`"check":"/views/_bundled/_check/group"`,
		`"origin":"bundled"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, `"stamp"`) || strings.Contains(body, `"folder"`) {
		t.Fatalf("a bundled view must not poll or listen for folder changes:\n%s", body)
	}
	module := getCustomView(srv, "/views/_bundled/group/briefing.tsx")
	if module.Code != http.StatusOK || !strings.Contains(module.Body.String(), "embedded") || !strings.Contains(module.Body.String(), `"./briefing.css?rhizome-css"`) {
		t.Fatalf("bundled module: %d\n%s", module.Code, module.Body.String())
	}
	if again := getCustomViewWith(srv, "/views/_bundled/group/briefing.tsx", http.Header{"If-None-Match": {module.Header().Get("ETag")}}); again.Code != http.StatusNotModified {
		t.Fatalf("unchanged bundled module: %d", again.Code)
	}
	if check := getCustomView(srv, "/views/_bundled/_check/group"); check.Code != http.StatusOK || check.Body.String() != "" {
		t.Fatalf("bundled check: %d %q", check.Code, check.Body.String())
	}
	for _, target := range []string{"/views/_bundled/group/briefing.yaml", "/views/_bundled/../kit/v1/boot.js", "/views/_bundled/_check/.."} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", target, rec.Code)
		}
	}
}

func TestRepositoryViewReplacesBundledViewWhenServed(t *testing.T) {
	srv := newBundledViewTestServer(t)
	folder := filepath.Join(srv.customViewsRoot(), "group")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"briefing.yaml": bundledBriefingYAML, "briefing.tsx": "export default null"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	body := getCustomView(srv, groupShellURL("group.briefing")).Body.String()
	for _, want := range []string{`import("/views/_files/group/briefing.tsx")`, `"origin":"repository"`, `"folder":"group"`, `"stamp":"/views/_stamp/group"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %s:\n%s", want, body)
		}
	}
}

func TestBundledViewsDirectoryOverride(t *testing.T) {
	srv := newBundledViewTestServer(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "group"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"briefing.yaml": bundledBriefingYAML, "briefing.tsx": "export default function Briefing() { return <p>from source</p>; }"} {
		if err := os.WriteFile(filepath.Join(dir, "group", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(bundledViewsDirEnv, dir)
	if module := getCustomView(srv, "/views/_bundled/group/briefing.tsx"); !strings.Contains(module.Body.String(), "from source") {
		t.Fatalf("override not served: %d\n%s", module.Code, module.Body.String())
	}
}

func TestBundledViewsDirectoryOverrideStaysInsideTheFolder(t *testing.T) {
	srv := newBundledViewTestServer(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "group"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "group", "inside.js"), []byte("export const inside = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(srv.cfg.VaultPath, "secret.txt"), filepath.Join(dir, "group", "secret.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv(bundledViewsDirEnv, dir)
	if inside := getCustomView(srv, "/views/_bundled/group/inside.js"); inside.Code != http.StatusOK {
		t.Fatalf("a file inside the folder: %d", inside.Code)
	}
	if escape := getCustomView(srv, "/views/_bundled/group/secret.txt"); escape.Code != http.StatusNotFound || strings.Contains(escape.Body.String(), "outside") {
		t.Fatalf("a symlink out of the bundled folder was followed: %d %q", escape.Code, escape.Body.String())
	}
}

func TestBundledStandaloneViewWhileOntologyCannotLoad(t *testing.T) {
	srv := newBundledViewTestServer(t)
	assets := srv.assets.(fstest.MapFS)
	assets["bundled-views/tools/clock.yaml"] = &fstest.MapFile{Data: []byte("apiVersion: rhizome.view.v1\nid: tools.clock\nname: Clock\nsource: {kind: custom, entry: clock.tsx}\nmount: {kind: standalone}\n")}
	assets["bundled-views/tools/clock.tsx"] = &fstest.MapFile{Data: []byte("export default null")}
	schema := filepath.Join(srv.cfg.VaultPath, ".rhizome", "ontology", "schema.graphql")
	if err := os.WriteFile(schema, []byte(`type Doc @node(paths: ["*.md"]) {`), 0o600); err != nil {
		t.Fatal(err)
	}
	shell := getCustomView(srv, "/views/tools.clock")
	for _, want := range []string{`import("/views/_bundled/tools/clock.tsx")`, `"origin":"bundled"`} {
		if shell.Code != http.StatusOK || !strings.Contains(shell.Body.String(), want) {
			t.Fatalf("shell missing %s: %d\n%s", want, shell.Code, shell.Body.String())
		}
	}
	if repository := getCustomView(srv, "/views/poc.board"); !strings.Contains(repository.Body.String(), `"origin":"repository"`) {
		t.Fatalf("repository shell without the ontology:\n%s", repository.Body.String())
	}
}
