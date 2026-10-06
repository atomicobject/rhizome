package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func addCustomViewTestSchema(t *testing.T, srv *Server) {
	t.Helper()
	dir := filepath.Join(srv.cfg.VaultPath, ".rhizome", "ontology")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	schema := `type Doc @node(paths: ["*.md"]) @display(group: "Delivery") { title: String @field }`
	if err := os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	srv.cfg.VaultDef = obsidian.VaultDefinition{Name: "test", Path: srv.cfg.VaultPath}
	srv.nodeProjectionCache = newNodeProjectionCache(8)
}

func TestCustomViewContextValidation(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	addCustomViewTestSchema(t, srv)
	def := viewconfig.ViewDefinition{ID: "group", Name: "Group", Mount: viewconfig.MountSpec{Kind: viewconfig.MountKindGroup, Group: "*"}}
	for _, raw := range []string{"", `{"kind":"group","group":"*"}`, `{"kind":"group","group":"Missing"}`, `{"kind":"group","group":"Delivery","sessionId":"secret"}`, `{"kind":"group","group":"Delivery"} {}`} {
		if _, err := srv.customViewInvocation(t.Context(), def, raw, false); err == nil {
			t.Fatalf("invalid context accepted: %s", raw)
		}
	}
	invocation, err := srv.customViewInvocation(t.Context(), def, `{"kind":"group","group":"Delivery"}`, false)
	if err != nil || invocation.Context.Group != "Delivery" {
		t.Fatalf("valid group rejected: %#v %v", invocation, err)
	}
	def.Mount.Group = "Delivery"
	if _, err := srv.customViewInvocation(t.Context(), def, "", false); err != nil {
		t.Fatalf("exact mount should infer context: %v", err)
	}
}

func TestWildcardCollectionViewsNeedAConcreteSubject(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	addCustomViewTestSchema(t, srv)
	def := viewconfig.ViewDefinition{ID: "type.briefing", Name: "Briefing", Mount: viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "*"}}
	for _, raw := range []string{"", `{"kind":"type","type":"*"}`, `{"kind":"type","type":"Missing"}`, `{"kind":"interface","interface":"Doc"}`} {
		_, err := srv.customViewInvocation(t.Context(), def, raw, false)
		if err == nil {
			t.Fatalf("invalid context accepted: %q", raw)
		}
		if raw == "" && !strings.Contains(err.Error(), "concrete type") {
			t.Fatalf("a launch without a subject should ask for one: %v", err)
		}
	}
	invocation, err := srv.customViewInvocation(t.Context(), def, `{"kind":"type","type":"Doc"}`, false)
	if err != nil || invocation.Context.Type != "Doc" {
		t.Fatalf("valid type rejected: %#v %v", invocation, err)
	}
	def.Mount = viewconfig.MountSpec{Kind: viewconfig.MountKindInterface, Interface: "*"}
	if _, err := srv.customViewInvocation(t.Context(), def, `{"kind":"interface","interface":"*"}`, false); err == nil || !strings.Contains(err.Error(), "concrete interface") {
		t.Fatalf("a wildcard interface context should be refused: %v", err)
	}
}

func TestCustomViewNodeContextUsesResolvedIdentity(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	addCustomViewTestSchema(t, srv)
	if err := os.WriteFile(filepath.Join(srv.cfg.VaultPath, "note.md"), []byte("# Note\n"), 0600); err != nil {
		t.Fatal(err)
	}
	def := viewconfig.ViewDefinition{ID: "node", Name: "Node", Mount: viewconfig.MountSpec{Kind: viewconfig.MountKindNode, Type: "Doc"}}
	for _, raw := range []string{"", `{"kind":"node","type":"Doc"}`, `{"kind":"node","ref":{"notePath":"note.md","kind":"note"}}`, `{"kind":"node","type":"Doc","ref":{"notePath":"note.md"}}`, `{"kind":"node","type":"Other","ref":{"notePath":"note.md","kind":"note"}}`, `{"kind":"node","type":"Doc","ref":{"notePath":"note.md","kind":"note","typeName":"Pretend"}}`} {
		if _, err := srv.customViewInvocation(t.Context(), def, raw, false); err == nil {
			t.Fatalf("invalid node context accepted: %s", raw)
		}
	}
	invocation, err := srv.customViewInvocation(t.Context(), def, `{"kind":"node","type":"Doc","ref":{"notePath":"note.md","kind":"note","startByte":99,"endByte":120,"parentId":"forged"}}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Context.Type != "Doc" || invocation.Context.Ref.TypeName != "Doc" || invocation.Context.Ref.StartByte != 0 || invocation.Context.Ref.EndByte != 0 || invocation.Context.Ref.ParentID != "" {
		t.Fatalf("caller identity trusted: %#v", invocation.Context.Ref)
	}
}

func TestCustomViewHTMLAssetCannotBypassMountContext(t *testing.T) {
	srv := newCustomViewTestServer(t, "index.html", map[string]string{"poc/index.html": "<!doctype html><!-- <head> --><html><head></head><body></body></html>"})
	invalid := url.Values{"viewId": {"poc.board"}, "context": {`{"kind":"node","type":"Doc","ref":{"notePath":"note.md","kind":"note"}}`}}
	response := getCustomView(srv, "/views/_files/poc/index.html?"+invalid.Encode())
	if response.Code != http.StatusBadRequest {
		t.Fatalf("asset context bypass: %d", response.Code)
	}
	valid := getCustomView(srv, "/views/_files/poc/index.html?viewId=poc.board")
	if !strings.Contains(valid.Body.String(), `--><html><head><script type="application/json"`) || !strings.Contains(valid.Body.String(), `"kind":"standalone"`) {
		t.Fatal(valid.Body.String())
	}
	// JSON marshaling keeps authored strings from breaking out of the inert data script.
	encoded := invocationScript(customViewInvocation{View: customViewIdentity{Name: "</script><script>bad()</script>"}, Context: ViewContext{Kind: viewconfig.MountKindStandalone}})
	if strings.Contains(string(encoded), "<script>bad") {
		t.Fatal("script injection")
	}
	var decoded map[string]any
	data := strings.TrimSuffix(strings.SplitN(string(encoded), ">", 2)[1], "</script>")
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestHostedNodeContextPreservesStagedIdentityWithoutSessionURL(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	addCustomViewTestSchema(t, srv)
	def := viewconfig.ViewDefinition{ID: "node", Name: "Node", Mount: viewconfig.MountSpec{Kind: viewconfig.MountKindNode, Type: "Doc"}}
	raw := `{"kind":"node","type":"Doc","ref":{"notePath":"staged.md","fragment":"^new-node","nodeId":"new-node","typeName":"Doc","kind":"embedded","startByte":5,"endByte":9,"parentId":"forged","structuralFingerprint":"fp"}}`
	invocation, err := srv.customViewInvocation(t.Context(), def, raw, true)
	if err != nil {
		t.Fatalf("staged identity rejected: %v", err)
	}
	ref := invocation.Context.Ref
	if ref.NodeID != "new-node" || ref.Fragment != "^new-node" || ref.Structural != "fp" || ref.StartByte != 0 || ref.EndByte != 0 || ref.ParentID != "" {
		t.Fatalf("hosted ref kept internal positions or lost identity: %#v", ref)
	}
	if _, err := srv.customViewInvocation(t.Context(), def, raw, false); err == nil {
		t.Fatal("unresolvable standalone node accepted")
	}
	if _, err := srv.customViewInvocation(t.Context(), def, strings.Replace(raw, `"staged.md"`, `"notes/10:30 sync.md"`, 1), true); err != nil {
		t.Fatalf("colon inside a filename rejected: %v", err)
	}
	if _, err := srv.customViewInvocation(t.Context(), def, strings.Replace(raw, `"staged.md"`, `"Q:A review.md"`, 1), true); err != nil {
		t.Fatalf("root filename with a letter and colon rejected: %v", err)
	}
	// hosted=1 is a URL parameter, so hosted contexts get the same shape checks.
	for name, bad := range map[string]string{
		"mount type":     strings.ReplaceAll(raw, `"Doc"`, `"Other"`),
		"ref type":       strings.Replace(raw, `"typeName":"Doc"`, `"typeName":"Other"`, 1),
		"traversal":      strings.Replace(raw, `"staged.md"`, `"../../x.md"`, 1),
		"absolute path":  strings.Replace(raw, `"staged.md"`, `"/etc/passwd"`, 1),
		"empty path":     strings.Replace(raw, `"staged.md"`, `""`, 1),
		"directory path": strings.Replace(raw, `"staged.md"`, `"."`, 1),
		"backslash":      strings.Replace(raw, `"staged.md"`, `"notes\\..\\x.md"`, 1),
		"drive letter":   strings.Replace(raw, `"staged.md"`, `"C:/x.md"`, 1),
	} {
		if _, err := srv.customViewInvocation(t.Context(), def, bad, true); err == nil {
			t.Fatalf("hosted context with bad %s accepted", name)
		}
	}
}

func TestWorkspaceContextHasNoSubjectOrOntologyDependency(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{"poc/board.tsx": "export default null"})
	def := viewconfig.ViewDefinition{ID: "workspace", Name: "Workspace", Mount: viewconfig.MountSpec{Kind: viewconfig.MountKindWorkspace}}
	for _, raw := range []string{"", `{"kind":"workspace"}`} {
		invocation, err := srv.customViewInvocation(t.Context(), def, raw, false)
		if err != nil || invocation.Context.Kind != viewconfig.MountKindWorkspace {
			t.Fatalf("workspace context: %+v %v", invocation, err)
		}
	}
	for _, raw := range []string{`{"kind":"workspace","type":"Doc"}`, `{"kind":"workspace","interface":"Doc"}`, `{"kind":"workspace","group":"Delivery"}`, `{"kind":"workspace","ref":{"notePath":"a.md","kind":"NOTE"}}`, `{"kind":"standalone"}`} {
		if _, err := srv.customViewInvocation(t.Context(), def, raw, false); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestWorkspaceHTMLRedirectCarriesInferredContext(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.html", map[string]string{"poc/board.html": "<html></html>"})
	definition := filepath.Join(srv.cfg.VaultPath, ".rhizome", "views", "poc", "board.yaml")
	data, err := os.ReadFile(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definition, []byte(strings.Replace(string(data), "kind: standalone", "kind: workspace", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	w := getCustomView(srv, "/views/poc.board")
	if w.Code != http.StatusFound {
		t.Fatalf("redirect: %d %s", w.Code, w.Body.String())
	}
	target, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("context") != `{"kind":"workspace"}` {
		t.Fatalf("workspace context lost: %s", target)
	}
}
