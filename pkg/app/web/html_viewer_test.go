package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	htmlformat "github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func TestHTMLViewerCapabilityServesIsolatedDocumentAndResources(t *testing.T) {
	root := t.TempDir()
	writeViewerFixture(t, root, "reports/prototype.html", "<!doctype html><html><head><script>window.authored=true</script></head><body>Report</body></html>")
	writeViewerFixture(t, root, "reports/data.json", `{"value":42}`)
	writeViewerFixture(t, root, "reports/secondary.html", `<html><body>Secondary</body></html>`)
	srv := newViewerTestServer(root, "")

	created := createViewer(t, srv, "http://127.0.0.1:8787", htmlViewerCreateRequest{
		Path: "reports/prototype.html", Query: "mode=demo", Fragment: "results",
	})
	if created.ID == "" || len(created.Nonce) != 32 {
		t.Fatalf("unexpected capability response: %#v", created)
	}
	parsed, err := url.Parse(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(parsed.Hostname(), ".localhost") || parsed.Port() != "8787" {
		t.Fatalf("viewer host = %q", parsed.Host)
	}
	if parsed.RawQuery != "mode=demo" || parsed.Fragment != "results" {
		t.Fatalf("viewer location = %q", created.URL)
	}

	document := requestViewer(t, srv, http.MethodGet, created.URL, nil)
	if document.Code != http.StatusOK {
		t.Fatalf("document status = %d, body %s", document.Code, document.Body.String())
	}
	body := document.Body.String()
	bootstrapAt := strings.Index(body, "window, \"rhizome\"")
	if bootstrapAt < 0 {
		bootstrapAt = strings.Index(body, "Object.defineProperty(window")
	}
	authoredAt := strings.Index(body, "window.authored=true")
	if bootstrapAt < 0 || authoredAt < 0 || bootstrapAt >= authoredAt {
		t.Fatalf("bootstrap must precede authored script: %s", body)
	}
	if got := document.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors http://127.0.0.1:8787") {
		t.Fatalf("CSP = %q", got)
	}
	requireScriptOnlySandbox(t, document.Header().Get("Content-Security-Policy"))
	if got := document.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}

	assetURL := *parsed
	assetURL.Path = "/reports/data.json"
	assetURL.RawQuery = ""
	assetURL.Fragment = ""
	asset := requestViewer(t, srv, http.MethodGet, assetURL.String(), nil)
	if asset.Code != http.StatusOK || asset.Body.String() != `{"value":42}` {
		t.Fatalf("asset = %d %q", asset.Code, asset.Body.String())
	}
	if got := asset.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("asset CORS = %q", got)
	}
	secondaryURL := assetURL
	secondaryURL.Path = "/reports/secondary.html"
	secondary := requestViewer(t, srv, http.MethodGet, secondaryURL.String(), nil)
	if secondary.Code != http.StatusOK {
		t.Fatalf("secondary HTML status = %d", secondary.Code)
	}
	requireScriptOnlySandbox(t, secondary.Header().Get("Content-Security-Policy"))

	denied := requestViewer(t, srv, http.MethodPost, created.URL, strings.NewReader("x"))
	if denied.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", denied.Code)
	}
	revoke := httptest.NewRequest(http.MethodDelete, "/api/v1/html-viewers/"+created.ID, nil)
	revoke.Host = "127.0.0.1:8787"
	revokeRecorder := httptest.NewRecorder()
	srv.handleHTMLViewerByID(revokeRecorder, revoke)
	if revokeRecorder.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d", revokeRecorder.Code)
	}
	after := requestViewer(t, srv, http.MethodGet, created.URL, nil)
	if after.Code != http.StatusNotFound || strings.Contains(after.Body.String(), "notes ui") {
		t.Fatalf("revoked capability response = %d %q", after.Code, after.Body.String())
	}
}

// requireScriptOnlySandbox parses the CSP sandbox directive: scripts may run,
// but the document must stay in an opaque origin.
func requireScriptOnlySandbox(t *testing.T, csp string) {
	t.Helper()
	var tokens []string
	found := false
	for _, directive := range strings.Split(csp, ";") {
		fields := strings.Fields(directive)
		if len(fields) > 0 && strings.EqualFold(fields[0], "sandbox") {
			found = true
			tokens = fields[1:]
		}
	}
	if !found {
		t.Fatalf("CSP has no sandbox directive: %q", csp)
	}
	scripts := false
	for _, token := range tokens {
		switch strings.ToLower(token) {
		case "allow-scripts":
			scripts = true
		case "allow-same-origin":
			t.Fatalf("sandbox must not permit same-origin: %q", csp)
		}
	}
	if !scripts {
		t.Fatalf("sandbox must permit scripts: %q", csp)
	}
}

func TestHTMLViewerCapabilityRequiresRemoteOriginConfiguration(t *testing.T) {
	root := t.TempDir()
	writeViewerFixture(t, root, "prototype.html", "<html><body>ok</body></html>")
	srv := newViewerTestServer(root, "")
	recorder := postViewerRequest(t, srv, "https://rhizome.example.test", htmlViewerCreateRequest{Path: "prototype.html"})
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "--html-content-origin") {
		t.Fatalf("remote response = %d %s", recorder.Code, recorder.Body.String())
	}

	srv = newViewerTestServer(root, "https://{token}.content.example.test")
	created := createViewer(t, srv, "https://rhizome.example.test", htmlViewerCreateRequest{Path: "prototype.html"})
	parsed, err := url.Parse(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || !strings.HasSuffix(parsed.Hostname(), ".content.example.test") {
		t.Fatalf("remote viewer URL = %q", created.URL)
	}
	srv.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "notes ui") })
	unknown := requestViewer(t, srv, http.MethodGet, "https://bad.content.example.test/prototype.html", nil)
	if unknown.Code != http.StatusNotFound || strings.Contains(unknown.Body.String(), "notes ui") {
		t.Fatalf("unknown content host reached app router: %d %q", unknown.Code, unknown.Body.String())
	}
}

func TestHTMLViewerRemoteCapabilityRequiresHTTPSAtProxyBoundary(t *testing.T) {
	root := t.TempDir()
	writeViewerFixture(t, root, "prototype.html", "<html><body>ok</body></html>")
	srv := newViewerTestServer(root, "https://{token}.content.example.test")
	srv.cfg.ApplicationOrigin = "https://rhizome.example.test"
	created := createViewer(t, srv, "https://rhizome.example.test", htmlViewerCreateRequest{Path: "prototype.html"})
	contentURL, err := url.Parse(created.URL)
	if err != nil {
		t.Fatal(err)
	}

	plainURL := *contentURL
	plainURL.Scheme = "http"
	plain := requestViewer(t, srv, http.MethodGet, plainURL.String(), nil)
	if plain.Code != http.StatusNotFound {
		t.Fatalf("plain HTTP status = %d", plain.Code)
	}

	proxiedRequest := httptest.NewRequest(http.MethodGet, plainURL.String(), nil)
	proxiedRequest.Host = contentURL.Host
	proxiedRequest.Header.Set("X-Forwarded-Proto", "https")
	proxied := httptest.NewRecorder()
	srv.Handler().ServeHTTP(proxied, proxiedRequest)
	if proxied.Code != http.StatusOK {
		t.Fatalf("trusted proxy HTTPS status = %d: %s", proxied.Code, proxied.Body.String())
	}
}

func TestHTMLViewerRemoteTLSProxyContract(t *testing.T) {
	root := t.TempDir()
	writeViewerFixture(t, root, "prototype.html", "<!doctype html><script>document.body.dataset.ready='yes'</script><body>remote</body>")
	srv := newViewerTestServer(root, "https://{token}.content.example.test")
	srv.cfg.ApplicationOrigin = "https://rhizome.example.test"
	srv.mux.HandleFunc("/api/v1/html-viewers", srv.handleHTMLViewers)
	proxy := httptest.NewTLSServer(srv.Handler())
	defer proxy.Close()

	payload, err := json.Marshal(htmlViewerCreateRequest{Path: "prototype.html"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, proxy.URL+"/api/v1/html-viewers", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "rhizome.example.test"
	request.Header.Set("Origin", "https://rhizome.example.test")
	response, err := proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("create status = %d: %s", response.StatusCode, body)
	}
	var created htmlViewerCreateResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	contentURL, err := url.Parse(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	contentRequest, err := http.NewRequest(http.MethodGet, proxy.URL+contentURL.RequestURI(), nil)
	if err != nil {
		t.Fatal(err)
	}
	contentRequest.Host = contentURL.Host
	contentResponse, err := proxy.Client().Do(contentRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer contentResponse.Body.Close()
	if contentResponse.StatusCode != http.StatusOK {
		t.Fatalf("content status = %d", contentResponse.StatusCode)
	}
	if got := contentResponse.Header.Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors https://rhizome.example.test") {
		t.Fatalf("CSP = %q", got)
	}
}

func TestHTMLViewerRejectsControlAndOutsideResources(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "vault")
	writeViewerFixture(t, outer, "outside.txt", "outside-sentinel")
	writeViewerFixture(t, root, "prototype.html", "<html><body>ok</body></html>")
	writeViewerFixture(t, root, ".rhizome/secret.txt", "secret")
	writeViewerFixture(t, root, ".obsidian/secret.txt", "secret")
	writeViewerFixture(t, root, ".git/secret.txt", "secret")
	srv := newViewerTestServer(root, "")
	created := createViewer(t, srv, "http://localhost:9898", htmlViewerCreateRequest{Path: "prototype.html"})
	parsed, _ := url.Parse(created.URL)
	for _, controlPath := range []string{".rhizome/secret.txt", ".obsidian/secret.txt", ".git/secret.txt"} {
		t.Run(controlPath, func(t *testing.T) {
			controlURL := *parsed
			controlURL.Path = "/" + controlPath
			control := requestViewer(t, srv, http.MethodGet, controlURL.String(), nil)
			if control.Code != http.StatusNotFound {
				t.Fatalf("control status = %d", control.Code)
			}
		})
	}
	parsed.RawPath = "/%2e%2e/outside.txt"
	parsed.Path = "/../outside.txt"
	outside := requestViewer(t, srv, http.MethodGet, parsed.String(), nil)
	if outside.Code != http.StatusNotFound || strings.Contains(outside.Body.String(), "outside-sentinel") {
		t.Fatalf("outside response = %d %q", outside.Code, outside.Body.String())
	}
}

func TestInjectHTMLViewerBootstrapPreservesBOMAndDoctype(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte("<!doctype html>\r\n<html><body>ok</body></html>")...)
	offset, err := htmlformat.ViewerBootstrapOffset(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := injectHTMLViewerBootstrap(source, offset, htmlViewerGrant{id: "id", nonce: "nonce", notePath: "x.html"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, append([]byte{0xef, 0xbb, 0xbf}, []byte("<!doctype html>")...)) {
		t.Fatalf("BOM or doctype moved: %q", got[:32])
	}
	start := bytes.Index(got, []byte("<script>"))
	end := bytes.Index(got[start:], []byte("</script>"))
	if start < 0 || end < 0 {
		t.Fatal("bootstrap script missing")
	}
	withoutBootstrap := append(append([]byte(nil), got[:start]...), got[start+end+len("</script>"):]...)
	if !bytes.Equal(withoutBootstrap, source) {
		t.Fatal("authored bytes changed outside bootstrap insertion")
	}
}

func TestBrowserMutationOriginGuardRejectsBeforeHandlerMutation(t *testing.T) {
	const external = "https://rhizome.example.test"
	for _, test := range []struct {
		name       string
		configured string
		scheme     string
		host       string
		origin     string
		deny       bool
	}{
		{name: "opaque content", configured: external, scheme: "https", host: "rhizome.example.test", origin: "null", deny: true},
		{name: "cross origin", configured: external, scheme: "https", host: "rhizome.example.test", origin: "https://content.example.test", deny: true},
		{name: "different scheme", configured: external, scheme: "https", host: "rhizome.example.test", origin: "http://rhizome.example.test", deny: true},
		{name: "same origin", configured: external, scheme: "https", host: "rhizome.example.test", origin: "https://rhizome.example.test"},
		{name: "non browser client", configured: external, scheme: "https", host: "rhizome.example.test"},
		{name: "configured external origin behind loopback proxy", configured: external, scheme: "https", host: "127.0.0.1:8787", origin: "https://rhizome.example.test"},
		{name: "equivalent default HTTPS port", configured: external, scheme: "https", host: "127.0.0.1:8787", origin: "https://rhizome.example.test:443"},
		// Default deployment: no configured origin, so the request's own
		// scheme and Host define the application origin.
		{name: "unconfigured same origin", scheme: "http", host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787"},
		{name: "unconfigured cross origin", scheme: "http", host: "127.0.0.1:8787", origin: "http://evil.example.test", deny: true},
		{name: "unconfigured different scheme", scheme: "http", host: "127.0.0.1:8787", origin: "https://127.0.0.1:8787", deny: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutated := false
			mux := http.NewServeMux()
			mux.HandleFunc("/mutate", func(w http.ResponseWriter, _ *http.Request) {
				mutated = true
				w.WriteHeader(http.StatusNoContent)
			})
			srv := &Server{
				cfg:         Config{ApplicationOrigin: test.configured},
				mux:         mux,
				htmlViewers: newHTMLViewerRegistry(),
			}
			// Every row uses an admitted application host, so a denial comes
			// from the origin guard rather than the host guard.
			req := httptest.NewRequest(http.MethodPost, test.scheme+"://"+test.host+"/mutate", nil)
			req.Host = test.host
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			recorder := httptest.NewRecorder()
			srv.Handler().ServeHTTP(recorder, req)
			wantStatus, wantMutated := http.StatusNoContent, true
			if test.deny {
				wantStatus, wantMutated = http.StatusForbidden, false
			}
			if recorder.Code != wantStatus || mutated != wantMutated {
				t.Fatalf("status = %d, mutated = %v; want %d, %v", recorder.Code, mutated, wantStatus, wantMutated)
			}
		})
	}
}

func TestApplicationHostGuardProtectsActualReadAndAgentSettingsRoutes(t *testing.T) {
	configRoot := t.TempDir()
	configFile := filepath.Join(configRoot, "config.yml")
	originalConfigPath := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return configRoot, configFile, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = originalConfigPath })
	_, err := agentchat.SaveSettings(agentchat.Settings{Harness: harness.KindClaude})
	if err != nil {
		t.Fatal(err)
	}
	fake := &harnesstest.Harness{StatusResult: harness.Status{Installed: true, LoggedIn: true}}
	service, err := agentchat.NewService(context.Background(), t.TempDir(), map[harness.Kind]harness.Harness{
		harness.KindCodex: fake, harness.KindClaude: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	srv := &Server{
		cfg:          Config{},
		runtime:      &Runtime{},
		mux:          http.NewServeMux(),
		htmlViewers:  newHTMLViewerRegistry(),
		agentService: service,
	}
	srv.mux.HandleFunc("/api/v1/status", srv.handlePublicStatus)
	srv.mux.HandleFunc("/api/agent/settings", srv.handleAgentSettings)

	read := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/status", nil)
	read.Host = "attacker.example.test"
	read.Header.Set("Origin", "http://attacker.example.test")
	readResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusForbidden {
		t.Fatalf("hostile read status = %d", readResponse.Code)
	}

	payload := bytes.NewBufferString(`{"settings":{"harness":"codex","harnesses":{"codex":{"permissionMode":"full-access"}}}}`)
	mutation := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/agent/settings", payload)
	mutation.Host = "attacker.example.test"
	mutation.Header.Set("Origin", "http://attacker.example.test")
	mutationResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(mutationResponse, mutation)
	if mutationResponse.Code != http.StatusForbidden {
		t.Fatalf("hostile settings status = %d", mutationResponse.Code)
	}
	settings, err := agentchat.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Harness != harness.KindClaude {
		t.Fatalf("agent harness changed through hostile host: %q", settings.Harness)
	}

	for _, test := range []struct {
		name       string
		host       string
		configured string
		deny       bool
	}{
		{name: "localhost", host: "localhost:8787"},
		{name: "IPv4 loopback", host: "127.0.0.1:8787"},
		{name: "IPv6 loopback", host: "[::1]:8787"},
		{name: "unrelated host", host: "evil.example.test:8787", deny: true},
		{name: "unrelated host with configured origin", host: "evil.example.test", configured: "https://rhizome.example.test", deny: true},
		{name: "configured origin", host: "rhizome.example.test", configured: "https://rhizome.example.test"},
		{name: "configured default port", host: "rhizome.example.test:443", configured: "https://rhizome.example.test"},
		{name: "configured origin keeps loopback control host", host: "127.0.0.1:8787", configured: "https://rhizome.example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv.cfg.ApplicationOrigin = test.configured
			request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/status", nil)
			request.Host = test.host
			response := httptest.NewRecorder()
			srv.Handler().ServeHTTP(response, request)
			want := http.StatusOK
			if test.deny {
				want = http.StatusForbidden
			}
			if response.Code != want {
				t.Fatalf("host %q status = %d, want %d", test.host, response.Code, want)
			}
		})
	}
}

func TestHTMLViewerRemoteOriginsAreValidatedBeforeServing(t *testing.T) {
	for _, test := range []struct {
		name        string
		content     string
		application string
		wantError   string
	}{
		{name: "valid", content: "https://{token}.content.example.test", application: "https://rhizome.example.test"},
		{name: "standalone HTTPS application", application: "https://rhizome.example.test"},
		{name: "standalone LAN application", application: "http://192.0.2.10:8787"},
		{name: "standalone application path", application: "https://rhizome.example.test/app", wantError: "absolute HTTP(S) origin"},
		{name: "standalone application credentials", application: "https://user:password@rhizome.example.test", wantError: "absolute HTTP(S) origin"},
		{name: "standalone application scheme", application: "ftp://rhizome.example.test", wantError: "absolute HTTP(S) origin"},
		{name: "HTTP content", content: "http://{token}.content.example.test", application: "https://rhizome.example.test", wantError: "HTTPS"},
		{name: "HTTP application", content: "https://{token}.content.example.test", application: "http://rhizome.example.test", wantError: "HTTPS"},
		{name: "missing application", content: "https://{token}.content.example.test", wantError: "both"},
		{name: "partial token label", content: "https://viewer-{token}.content.example.test", application: "https://rhizome.example.test", wantError: "complete hostname label"},
		{name: "application path", content: "https://{token}.content.example.test", application: "https://rhizome.example.test/app", wantError: "absolute HTTPS origin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateHTMLViewerOrigins(test.content, test.application)
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestHTMLViewerContentNamespaceNeverFallsThroughToApplication(t *testing.T) {
	srv := newViewerTestServer(t.TempDir(), "https://{token}.content.example.test")
	for _, host := range []string{"content.example.test", "unknown.content.example.test", "nested.unknown.content.example.test"} {
		req := httptest.NewRequest(http.MethodGet, "https://"+host+"/notes", nil)
		req.Host = host
		recorder := httptest.NewRecorder()
		srv.Handler().ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", host, recorder.Code)
		}
	}
}

func TestHTMLViewerRegistryBoundsLiveCapabilities(t *testing.T) {
	registry := newHTMLViewerRegistry()
	base := time.Now().Add(time.Hour)
	for index := 0; index <= htmlViewerMaxGrants; index++ {
		registry.put(htmlViewerGrant{
			id:        fmt.Sprintf("viewer-%03d", index),
			host:      fmt.Sprintf("viewer-%03d.localhost", index),
			expiresAt: base.Add(time.Duration(index) * time.Second),
		})
	}
	if _, exists := registry.byHost("viewer-000.localhost"); exists {
		t.Fatal("oldest capability was not evicted")
	}
	if _, exists := registry.byHost(fmt.Sprintf("viewer-%03d.localhost", htmlViewerMaxGrants)); !exists {
		t.Fatal("newest capability was not retained")
	}
}

func newViewerTestServer(root, template string) *Server {
	registry, err := builtin.NewRegistry()
	if err != nil {
		panic(err)
	}
	formatRuntime, err := noteformat.NewRuntime(registry, htmlformat.New())
	if err != nil {
		panic(err)
	}
	indexer, err := notemeta.NewIndexer(formatRuntime)
	if err != nil {
		panic(err)
	}
	vaultDef := obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.html", "*.html"}}
	catalog, err := NewFileCatalog(vaultDef, nil, indexer)
	if err != nil {
		panic(err)
	}
	return &Server{
		cfg: Config{
			VaultPath:                 root,
			VaultDef:                  vaultDef,
			NoteMetadata:              indexer,
			NotePathOwned:             func(path string) bool { return path == "prototype.html" || path == "reports/prototype.html" },
			HTMLContentOriginTemplate: template,
		},
		runtime:     &Runtime{},
		mux:         http.NewServeMux(),
		htmlViewers: newHTMLViewerRegistry(),
		catalog:     catalog,
	}
}

func writeViewerFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func createViewer(t *testing.T, srv *Server, appURL string, body htmlViewerCreateRequest) htmlViewerCreateResponse {
	t.Helper()
	recorder := postViewerRequest(t, srv, appURL, body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create viewer = %d %s", recorder.Code, recorder.Body.String())
	}
	var response htmlViewerCreateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func postViewerRequest(t *testing.T, srv *Server, appURL string, body htmlViewerCreateRequest) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(appURL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, appURL+"/api/v1/html-viewers", bytes.NewReader(payload))
	req.Host = parsed.Host
	req.Header.Set("Origin", appURL)
	recorder := httptest.NewRecorder()
	srv.handleHTMLViewers(recorder, req)
	return recorder
}

func requestViewer(t *testing.T, srv *Server, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, req)
	return recorder
}
