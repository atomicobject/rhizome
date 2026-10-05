package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const customViewTestYAML = `apiVersion: rhizome.view.v1
id: poc.board
name: Board <1>
source:
  kind: custom
  entry: ENTRY
mount:
  kind: standalone
`

func newCustomViewTestServer(t *testing.T, entry string, files map[string]string) *Server {
	t.Helper()
	root := t.TempDir()
	files["poc/board.yaml"] = strings.Replace(customViewTestYAML, "ENTRY", entry, 1)
	for rel, body := range files {
		full := filepath.Join(root, ".rhizome", "views", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Server{
		cfg:    Config{VaultPath: root},
		assets: fstest.MapFS{customViewKitBoot: &fstest.MapFile{Data: []byte("//")}},
	}
}

func getCustomView(srv *Server, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	srv.handleCustomViews(rec, req)
	return rec
}

func TestCustomViewShellAndTransform(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":  `export default function Board() { return <p>hi</p>; }`,
		"poc/broken.tsx": "export default () => <div>;",
		"poc/style.css":  "p{color:red}",
	})

	shell := getCustomView(srv, "/views/poc.board")
	body := shell.Body.String()
	for _, want := range []string{"<title>Board &lt;1&gt;</title>", `src="/kit/v1/boot.js"`, `import("/views/_files/poc/board.tsx")`, `"stamp":"/views/_stamp/poc"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %q:\n%s", want, body)
		}
	}
	if shell.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("shell must not be cached")
	}

	module := getCustomView(srv, "/views/_files/poc/board.tsx")
	if module.Code != http.StatusOK || !strings.HasPrefix(module.Header().Get("Content-Type"), "text/javascript") ||
		!strings.Contains(module.Body.String(), "react/jsx-runtime") {
		t.Fatalf("module: %d %s\n%s", module.Code, module.Header().Get("Content-Type"), module.Body.String())
	}

	broken := getCustomView(srv, "/views/_files/poc/broken.tsx")
	if broken.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(broken.Body.String(), "poc/broken.tsx:1:") {
		t.Fatalf("broken: %d %q", broken.Code, broken.Body.String())
	}
	if check := getCustomView(srv, "/views/_check/poc"); check.Code != http.StatusOK || !strings.HasPrefix(check.Body.String(), "poc/broken.tsx:1:") {
		t.Fatalf("check: %d %q", check.Code, check.Body.String())
	}

	if css := getCustomView(srv, "/views/_files/poc/style.css"); css.Code != http.StatusOK || !strings.HasPrefix(css.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css: %d %s", css.Code, css.Header().Get("Content-Type"))
	}
}

func TestCustomViewFilesStayInsideViewsFolder(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	link := filepath.Join(srv.cfg.VaultPath, ".rhizome", "views", "poc", "leak.txt")
	if err := os.Symlink(filepath.Join(srv.cfg.VaultPath, "secret.txt"), link); err != nil {
		t.Skip("symlinks unavailable")
	}
	for _, target := range []string{"/views/_files/../../secret.txt", "/views/_files/poc/../../../secret.txt", "/views/_files/poc/leak.txt", "/views/_files/poc"} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d body %q", target, rec.Code, rec.Body.String())
		}
	}
	for _, target := range []string{"/views/_stamp/../..", "/views/_check/../.."} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", target, rec.Code)
		}
	}
}

func TestCustomViewHTMLEntryRedirectsAndStampChanges(t *testing.T) {
	srv := newCustomViewTestServer(t, "index.html", map[string]string{"poc/index.html": "<p>tool</p>"})
	redirect := getCustomView(srv, "/views/poc.board?tab=2")
	if redirect.Code != http.StatusFound || redirect.Header().Get("Location") != "/views/_files/poc/index.html?tab=2&viewId=poc.board" {
		t.Fatalf("redirect: %d %q", redirect.Code, redirect.Header().Get("Location"))
	}
	if missing := getCustomView(srv, "/views/nope"); missing.Code != http.StatusNotFound {
		t.Fatalf("missing view: %d", missing.Code)
	}

	before := getCustomView(srv, "/views/_stamp/poc").Body.String()
	page := filepath.Join(srv.cfg.VaultPath, ".rhizome", "views", "poc", "index.html")
	if err := os.WriteFile(page, []byte("<p>tool, edited</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := getCustomView(srv, "/views/_stamp/poc").Body.String(); after == before || after == "" {
		t.Fatalf("stamp did not change: %q -> %q", before, after)
	}
}

func TestCustomViewShellNeedsKit(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	srv.assets = fstest.MapFS{}
	if rec := getCustomView(srv, "/views/poc.board"); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "NO_WEB") {
		t.Fatalf("no kit: %d %q", rec.Code, rec.Body.String())
	}
}

func TestCustomViewNestedEntryWatchesTheDefinitionFolder(t *testing.T) {
	srv := newCustomViewTestServer(t, "src/main.tsx", map[string]string{"poc/src/main.tsx": "export default null"})
	shell := getCustomView(srv, "/views/poc.board")
	body := shell.Body.String()
	for _, want := range []string{`import("/views/_files/poc/src/main.tsx")`, `"stamp":"/views/_stamp/poc"`, `"check":"/views/_check/poc"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %q:\n%s", want, body)
		}
	}
	if got := shell.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
		t.Fatalf("frame-ancestors header = %q", got)
	}
}

func TestCustomViewFilesRefuseDefinitionsDotfilesAndPackages(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":                 "export default null",
		"poc/data.json":                 `{"ok":true}`,
		"poc/.env":                      "TOKEN=secret",
		"poc/.git/config":               "[core]",
		"poc/node_modules/pkg/index.js": "export {}",
	})
	if rec := getCustomView(srv, "/views/_files/poc/data.json"); rec.Code != http.StatusOK {
		t.Fatalf("an ordinary asset must be served: %d", rec.Code)
	}
	for _, target := range []string{
		"/views/_files/poc/board.yaml",
		"/views/_files/poc/.env",
		"/views/_files/poc/.git/config",
		"/views/_files/poc/node_modules/pkg/index.js",
	} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d body %q", target, rec.Code, rec.Body.String())
		}
	}
	for _, target := range []string{"/views/_stamp/poc/node_modules", "/views/_check/poc/.git"} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d", target, rec.Code)
		}
	}
}

func TestCustomViewStampAndCheckDoNotFollowSymlinksOut(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	outside := t.TempDir()
	broken := filepath.Join(outside, "broken.tsx")
	if err := os.WriteFile(broken, []byte("export const = ;"), 0o644); err != nil {
		t.Fatal(err)
	}
	views := filepath.Join(srv.cfg.VaultPath, ".rhizome", "views")
	links := map[string]string{
		filepath.Join(views, "linked"):          outside,
		filepath.Join(views, "poc", "escape"):   outside,
		filepath.Join(views, "poc", "leak.tsx"): broken,
	}
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Skip("symlinks unavailable")
		}
	}

	for _, target := range []string{"/views/_stamp/linked", "/views/_check/linked"} {
		if rec := getCustomView(srv, target); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d body %q", target, rec.Code, rec.Body.String())
		}
	}
	if check := getCustomView(srv, "/views/_check/poc"); check.Code != http.StatusOK || check.Body.String() != "" {
		t.Fatalf("check must not read through symlinks: %d %q", check.Code, check.Body.String())
	}
	before := getCustomView(srv, "/views/_stamp/poc").Body.String()
	if err := os.WriteFile(broken, []byte("export const changed = 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := getCustomView(srv, "/views/_stamp/poc").Body.String(); after != before {
		t.Fatalf("stamp followed a symlink out of the views folder: %q -> %q", before, after)
	}
}

func TestCustomViewAtTheViewsRootGetsCleanFolderURLs(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	views := filepath.Join(srv.cfg.VaultPath, ".rhizome", "views")
	definition := strings.Replace(strings.Replace(customViewTestYAML, "ENTRY", "root.tsx", 1), "poc.board", "root.board", 1)
	for name, body := range map[string]string{"root.yaml": definition, "root.tsx": "export default null"} {
		if err := os.WriteFile(filepath.Join(views, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	body := getCustomView(srv, "/views/root.board").Body.String()
	for _, want := range []string{`"stamp":"/views/_stamp/"`, `"check":"/views/_check/"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %q:\n%s", want, body)
		}
	}
	if rec := getCustomView(srv, "/views/_stamp/"); rec.Code != http.StatusOK || rec.Body.String() == "" {
		t.Fatalf("root stamp: %d %q", rec.Code, rec.Body.String())
	}
}

func TestCustomViewHTMLAndScriptContextsMatch(t *testing.T) {
	for _, entry := range []string{"index.html", "board.tsx"} {
		t.Run(entry, func(t *testing.T) {
			srv := newCustomViewTestServer(t, entry, map[string]string{"poc/" + entry: "<html><head><script>window.loaded=true</script></head><body>hi</body></html>"})
			definition := strings.Replace(strings.Replace(customViewTestYAML, "ENTRY", entry, 1), "kind: standalone", "kind: type\n  type: Doc", 1) + "configuration:\n  label: Authored\n"
			if err := os.WriteFile(filepath.Join(srv.customViewsRoot(), "poc", "board.yaml"), []byte(definition), 0600); err != nil {
				t.Fatal(err)
			}
			addCustomViewTestSchema(t, srv)
			response := getCustomView(srv, "/views/poc.board?configuration=ignored")
			if entry == "index.html" {
				response = getCustomView(srv, response.Header().Get("Location"))
			}
			if response.Code != http.StatusOK {
				t.Fatalf("response %d: %s", response.Code, response.Body.String())
			}
			for _, want := range []string{`id="rhizome-view-invocation"`, `"context":{"kind":"type","type":"Doc"}`, `"configuration":{"label":"Authored"}`} {
				if !strings.Contains(response.Body.String(), want) {
					t.Fatalf("missing %s: %s", want, response.Body.String())
				}
			}
			bad := url.Values{"context": {`{"kind":"type","type":"Missing"}`}}
			if response := getCustomView(srv, "/views/poc.board?"+bad.Encode()); response.Code != http.StatusBadRequest {
				t.Fatalf("invalid subject accepted: %d", response.Code)
			}
		})
	}
}

func TestDuplicateDefaultCustomViewsStayServable(t *testing.T) {
	typeMount := strings.Replace(customViewTestYAML, "kind: standalone", "kind: type\n  type: Doc\n  default: true", 1)
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":  "export default null",
		"poc/other.yaml": strings.Replace(strings.Replace(typeMount, "ENTRY", "board.tsx", 1), "poc.board", "poc.other", 1),
	})
	board := filepath.Join(srv.cfg.VaultPath, ".rhizome", "views", "poc", "board.yaml")
	if err := os.WriteFile(board, []byte(strings.Replace(typeMount, "ENTRY", "board.tsx", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"poc.board", "poc.other"} {
		if _, err := srv.findCustomView(t.Context(), id); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
}

func TestCatalogInvalidCustomViewIsNotServed(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	addCustomViewTestSchema(t, srv)
	board := filepath.Join(srv.cfg.VaultPath, ".rhizome", "views", "poc", "board.yaml")
	body := strings.Replace(strings.Replace(customViewTestYAML, "ENTRY", "board.tsx", 1), "kind: standalone", "kind: type\n  type: Missing", 1)
	if err := os.WriteFile(board, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.findCustomView(t.Context(), "poc.board"); err == nil {
		t.Fatal("catalog-invalid view resolved")
	}
	if response := getCustomView(srv, "/views/poc.board"); response.Code != http.StatusNotFound {
		t.Fatalf("catalog-invalid view served: %d", response.Code)
	}
}

func TestCustomViewsWhileOntologyCannotLoad(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx": "export default null",
		"poc/group.yaml": strings.Replace(strings.Replace(strings.Replace(customViewTestYAML, "ENTRY", "board.tsx", 1),
			"kind: standalone", "kind: group\n  group: Delivery", 1), "poc.board", "poc.group", 1),
	})
	addCustomViewTestSchema(t, srv)
	schema := filepath.Join(srv.cfg.VaultPath, ".rhizome", "ontology", "schema.graphql")
	if err := os.WriteFile(schema, []byte(`type Doc @node(paths: ["*.md"]) {`), 0o600); err != nil {
		t.Fatal(err)
	}
	if response := getCustomView(srv, "/views/poc.board"); response.Code != http.StatusOK {
		t.Fatalf("standalone link broke while ontology is invalid: %d %s", response.Code, response.Body.String())
	}
	if response := getCustomView(srv, "/views/poc.group"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("subject-mounted view: want 503, got %d %s", response.Code, response.Body.String())
	}
}
