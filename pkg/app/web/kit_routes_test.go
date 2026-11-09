package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newKitRouteServer(files fstest.MapFS) *Server {
	s := &Server{mux: http.NewServeMux(), assets: files}
	s.registerRoutes()
	return s
}

func kitFiles(kit string) fstest.MapFS {
	return fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html>")},
		"kit/v1/boot.js":         {Data: []byte("// boot")},
		"kit/v1/kit.js":          {Data: []byte(kit)},
		"kit/v1/kit.css":         {Data: []byte(".x{}")},
		"kit/v1/chunks/a-123.js": {Data: []byte("// chunk")},
	}
}

func getKit(s *Server, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestKitBuildStampedPathIsImmutable(t *testing.T) {
	s := newKitRouteServer(kitFiles("// kit"))
	base := s.kitBase()
	if !strings.HasPrefix(base, "/kit/v1/") || base == "/kit/v1/" || !strings.HasSuffix(base, "/") {
		t.Fatalf("kit base %q is not build-stamped", base)
	}
	for _, file := range []string{"boot.js", "kit.js", "kit.css", "chunks/a-123.js"} {
		rec := getKit(s, base+file, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", file, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("%s Cache-Control = %q", file, got)
		}
	}
	if rec := getKit(s, base+"kit.js", nil); rec.Body.String() != "// kit" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("stamped kit.js: %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec := getKit(s, base+"missing.js", nil); rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "" {
		t.Fatalf("missing stamped file: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestKitBuildStampFollowsContent(t *testing.T) {
	a := newKitRouteServer(kitFiles("// kit")).kitBase()
	if again := newKitRouteServer(kitFiles("// kit")).kitBase(); again != a {
		t.Fatalf("same kit, different stamps: %q %q", a, again)
	}
	if changed := newKitRouteServer(kitFiles("// kit 2")).kitBase(); changed == a {
		t.Fatalf("changed kit kept stamp %q", a)
	}
	// An old build's stamp is not this build: it must never be cached as immutable.
	s := newKitRouteServer(kitFiles("// kit 2"))
	if rec := getKit(s, a+"kit.js", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("stale stamp: %d", rec.Code)
	}
}

func TestKitUnstampedPathKeepsWorkingAndRevalidates(t *testing.T) {
	s := newKitRouteServer(kitFiles("// kit"))
	for _, file := range []string{"/kit/v1/boot.js", "/kit/v1/kit.js", "/kit/v1/chunks/a-123.js"} {
		rec := getKit(s, file, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", file, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s Cache-Control = %q", file, got)
		}
		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("%s has no validator", file)
		}
		if again := getKit(s, file, http.Header{"If-None-Match": {etag}}); again.Code != http.StatusNotModified {
			t.Fatalf("%s revalidation: %d", file, again.Code)
		}
	}
	if rec := getKit(s, "/kit/v1/nope.js", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing unstamped file: %d", rec.Code)
	}
}

func TestCustomViewShellReferencesStampedKit(t *testing.T) {
	srv := newCustomViewTestServer(t, "board.tsx", map[string]string{
		"poc/board.tsx": `export default function Board() { return <p>hi</p>; }`,
	})
	body := getCustomView(srv, "/views/poc.board").Body.String()
	if want := `src="` + srv.kitBase() + `boot.js"`; !strings.Contains(body, want) || srv.kitBase() == "/kit/v1/" {
		t.Fatalf("shell missing %q:\n%s", want, body)
	}
}
