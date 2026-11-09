package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// newExperienceTestServer has a Knowledge experience in the flat views folder
// whose panel presents the Knowledge group, a Claim type in that group, a
// Memo type outside it, and a Quote type embedded in both, in no group's tree.
func newExperienceTestServer(t *testing.T) *Server {
	t.Helper()
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx":       "export default function Board() { return null }",
		"site/experience.yml": "title: Knowledge\ngroup: Knowledge\npanel: site.panel\n",
		"site/panel.yaml":     customView("site.panel", "{kind: panel}", "panel.tsx"),
		"site/panel.tsx":      "export default function Panel() { return null }",
		"site/home.yaml":      customView("site.home", "{kind: group, group: Knowledge}", "home.tsx"),
		"site/home.tsx":       "export default function Home() { return null }",
		"site/about.yaml":     customView("site.about", "{kind: standalone}", "about.tsx"),
		"site/about.tsx":      "export default function About() { return null }",
	})
	for rel, body := range map[string]string{".rhizome/ontology/schema.graphql": `type Claim @node(paths: ["claims/*.md"]) @display(group: "Knowledge") { title: String @field }
type Memo @node(paths: ["memos/*.md"]) { title: String @field }
type Quote implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#quote", paths: ["claims/*.md", "memos/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`,
		"claims/a.md": "# A\n\n- [ ] quoted #quote ^q1\n", "memos/m.md": "# M\n\n- [ ] quoted #quote ^q2\n"} {
		full := filepath.Join(srv.cfg.VaultPath, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv.cfg.VaultDef = obsidian.VaultDefinition{Name: "test", Path: srv.cfg.VaultPath}
	srv.nodeProjectionCache = newNodeProjectionCache(8)
	return srv
}

func customView(id, mount, entry string) string {
	return "apiVersion: rhizome.view.v1\nid: " + id + "\nname: " + id + "\nsource: {kind: custom, entry: " + entry + "}\nmount: " + mount + "\n"
}

func getInvocation(srv *Server, id, context string) *httptest.ResponseRecorder {
	query := url.Values{"hosted": {"1"}}
	if context != "" {
		query.Set("context", context)
	}
	rec := httptest.NewRecorder()
	srv.handlePublicViewInvocation(rec, httptest.NewRequest(http.MethodGet, "/api/v1/views/"+id+"/invocation?"+query.Encode(), nil))
	return rec
}

func TestViewInvocationRouteChecksPagesAndPanels(t *testing.T) {
	srv := newExperienceTestServer(t)
	for _, tc := range []struct {
		name, view, context string
		status              int
	}{
		{"page on its mount", "site.home", `{"kind":"group","group":"Knowledge"}`, http.StatusOK},
		{"page on another subject", "site.home", `{"kind":"type","type":"Claim"}`, http.StatusBadRequest},
		{"view outside experiences", "poc.board", "", http.StatusOK},
		{"panel for the group", "site.panel", `{"kind":"group","group":"Knowledge"}`, http.StatusOK},
		{"panel for a collection in the group", "site.panel", `{"kind":"type","type":"Claim"}`, http.StatusOK},
		{"panel for a note in the group", "site.panel", `{"kind":"node","type":"Claim","ref":{"notePath":"claims/a.md","kind":"NOTE","typeName":"Claim"}}`, http.StatusOK},
		{"panel for an embedded node through its owner", "site.panel", `{"kind":"node","type":"Quote","ref":{"notePath":"claims/a.md","kind":"EMBEDDED","nodeId":"q1","fragment":"^q1","typeName":"Quote"}}`, http.StatusOK},
		{"panel for an embedded node whose owner shows Folder", "site.panel", `{"kind":"node","type":"Quote","ref":{"notePath":"memos/m.md","kind":"EMBEDDED","nodeId":"q2","fragment":"^q2","typeName":"Quote"}}`, http.StatusBadRequest},
		{"panel for a note Folder shows", "site.panel", `{"kind":"node","type":"Memo","ref":{"notePath":"memos/m.md","kind":"NOTE","typeName":"Memo"}}`, http.StatusBadRequest},
		{"panel for the experience's standalone view", "site.panel", `{"kind":"standalone","view":"site.about"}`, http.StatusOK},
		{"panel for another standalone view", "site.panel", `{"kind":"standalone","view":"poc.board"}`, http.StatusBadRequest},
		{"panel for an unnamed standalone view", "site.panel", `{"kind":"standalone"}`, http.StatusBadRequest},
		{"panel for the workspace", "site.panel", `{"kind":"workspace"}`, http.StatusBadRequest},
		{"panel without a context", "site.panel", "", http.StatusBadRequest},
		{"view named outside a standalone context", "site.home", `{"kind":"group","group":"Knowledge","view":"site.home"}`, http.StatusBadRequest},
		{"standalone context naming another view", "site.about", `{"kind":"standalone","view":"poc.board"}`, http.StatusBadRequest},
		{"unknown view", "missing", "", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getInvocation(srv, tc.view, tc.context)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestViewInvocationCarriesExperienceKeyAndGeneration(t *testing.T) {
	srv := newExperienceTestServer(t)
	previous := indexGeneration
	indexGeneration = func(*Server, context.Context) string { return "e.1.s.v.g.c" }
	t.Cleanup(func() { indexGeneration = previous })

	rec := getInvocation(srv, "site.home", `{"kind":"group","group":"Knowledge"}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Rhizome-Generation") != "e.1.s.v.g.c" {
		t.Fatalf("%d %q %s", rec.Code, rec.Header().Get("Rhizome-Generation"), rec.Body.String())
	}
	var page, panel customViewInvocation
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.View.ExperienceKey == "" || page.Context.Group != "Knowledge" {
		t.Fatalf("page invocation: %+v", page)
	}
	if page.Entry == "" || page.Check == "" || !strings.Contains(page.Check, customViewCheckPrefix) {
		t.Fatalf("an experience frame loads a page from its entry and check URLs: %+v", page)
	}
	rec = getInvocation(srv, "site.panel", `{"kind":"group","group":"Knowledge"}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if panel.View.ExperienceKey != page.View.ExperienceKey {
		t.Fatalf("panel and page name different experiences: %q %q", panel.View.ExperienceKey, page.View.ExperienceKey)
	}
	rec = getInvocation(srv, "poc.board", "")
	var outside customViewInvocation
	if err := json.Unmarshal(rec.Body.Bytes(), &outside); err != nil || outside.View.ExperienceKey != "" {
		t.Fatalf("a view outside experiences has no key: %+v %v", outside, err)
	}
}

func TestViewInvocationRefusesUntrustedViewsAndOtherMethods(t *testing.T) {
	srv := newExperienceTestServer(t)
	srv.cfg.RepositoryTrusted = func(string) (bool, error) { return false, nil }
	if rec := getInvocation(srv, "site.home", `{"kind":"group","group":"Knowledge"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("untrusted view served: %d %s", rec.Code, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	srv.handlePublicViewInvocation(rec, httptest.NewRequest(http.MethodPost, "/api/v1/views/site.home/invocation", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
}
