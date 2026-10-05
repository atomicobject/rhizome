package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	htmlViewerTokenBytes = 16
	htmlViewerLifetime   = 8 * time.Hour
	htmlViewerMaxGrants  = 256
)

type htmlViewerGrant struct {
	id        string
	nonce     string
	notePath  string
	appOrigin string
	host      string
	scheme    string
	expiresAt time.Time
}

type htmlViewerRegistry struct {
	mu     sync.Mutex
	grants map[string]htmlViewerGrant
}

func newHTMLViewerRegistry() *htmlViewerRegistry {
	return &htmlViewerRegistry{grants: make(map[string]htmlViewerGrant)}
}

func (r *htmlViewerRegistry) put(grant htmlViewerGrant) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	if len(r.grants) >= htmlViewerMaxGrants {
		var oldestID string
		var oldestExpiry time.Time
		for id, existing := range r.grants {
			if oldestID == "" || existing.expiresAt.Before(oldestExpiry) {
				oldestID, oldestExpiry = id, existing.expiresAt
			}
		}
		delete(r.grants, oldestID)
	}
	r.grants[grant.id] = grant
}

func (r *htmlViewerRegistry) revoke(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.grants[id]; !ok {
		return false
	}
	delete(r.grants, id)
	return true
}

func (r *htmlViewerRegistry) byHost(host string) (htmlViewerGrant, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now())
	host = normalizeHostPort(host)
	for _, grant := range r.grants {
		if normalizeHostPort(grant.host) == host {
			return grant, true
		}
	}
	return htmlViewerGrant{}, false
}

func (r *htmlViewerRegistry) pruneLocked(now time.Time) {
	for id, grant := range r.grants {
		if !now.Before(grant.expiresAt) {
			delete(r.grants, id)
		}
	}
}

type htmlViewerCreateRequest struct {
	Path     string `json:"path"`
	Query    string `json:"query,omitempty"`
	Fragment string `json:"fragment,omitempty"`
}

type htmlViewerCreateResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Server) handleHTMLViewers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var request htmlViewerCreateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid viewer request: %w", err))
		return
	}
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, request.Path)
	if err != nil || readPath.rel == "" {
		writeError(w, http.StatusBadRequest, errors.New("path must be an admitted HTML note"))
		return
	}
	if !s.exactNotePathOwned(readPath.rel) {
		writeError(w, http.StatusNotFound, errors.New("HTML note is not currently admitted"))
		return
	}
	if _, err := s.viewerResource(readPath.rel, true, false); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	appOrigin, err := applicationOrigin(s.cfg.ApplicationOrigin, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, err := randomHex(htmlViewerTokenBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	nonce, err := randomHex(htmlViewerTokenBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	base, err := s.viewerOrigin(r, id)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	viewerURL := *base
	viewerURL.Path = "/" + readPath.rel
	viewerURL.RawQuery = strings.TrimPrefix(request.Query, "?")
	viewerURL.Fragment = strings.TrimPrefix(request.Fragment, "#")
	expiresAt := time.Now().Add(htmlViewerLifetime)
	s.htmlViewers.put(htmlViewerGrant{
		id: id, nonce: nonce, notePath: readPath.rel, appOrigin: appOrigin,
		host: viewerURL.Host, scheme: viewerURL.Scheme, expiresAt: expiresAt,
	})
	writeJSON(w, http.StatusCreated, htmlViewerCreateResponse{
		ID: id, URL: viewerURL.String(), Nonce: nonce, ExpiresAt: expiresAt.UTC(),
	})
}

func (s *Server) handleHTMLViewerByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/html-viewers/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, errors.New("viewer id is required"))
		return
	}
	s.htmlViewers.revoke(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) viewerOrigin(r *http.Request, token string) (*url.URL, error) {
	template := strings.TrimSpace(s.cfg.HTMLContentOriginTemplate)
	if template == "" {
		hostname := requestHostname(r.Host)
		if !isLoopbackHostname(hostname) {
			return nil, errors.New("remote HTML viewing requires --html-content-origin with a {token} hostname")
		}
		_, port, _ := net.SplitHostPort(r.Host)
		host := token + ".localhost"
		if port != "" {
			host = net.JoinHostPort(host, port)
		}
		return &url.URL{Scheme: requestScheme(r), Host: host}, nil
	}
	if strings.Count(template, "{token}") != 1 {
		return nil, errors.New("HTML content origin must contain exactly one {token} placeholder")
	}
	parsed, err := url.Parse(strings.Replace(template, "{token}", token, 1))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("remote HTML content origin must be an absolute HTTPS origin")
	}
	parsed.Path = ""
	return parsed, nil
}

func applicationOrigin(configured string, r *http.Request) (string, error) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if configured != "" {
		if origin != "" {
			authored, authoredErr := url.Parse(origin)
			expected, expectedErr := url.Parse(configured)
			if authoredErr != nil || expectedErr != nil || !sameWebOrigin(authored, expected) {
				return "", errors.New("request origin does not match the configured application origin")
			}
		}
		return configured, nil
	}
	if origin == "" {
		origin = requestScheme(r) + "://" + r.Host
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("request has an invalid application origin")
	}
	return parsed.String(), nil
}

func validateHTMLViewerOrigins(contentTemplate, application string) error {
	contentTemplate = strings.TrimSpace(contentTemplate)
	application = strings.TrimSpace(application)
	if contentTemplate == "" {
		if application != "" {
			app, err := url.Parse(application)
			if err != nil || !validHTTPOrigin(app) {
				return errors.New("application origin must be an absolute HTTP(S) origin")
			}
		}
		return nil
	}
	if application == "" {
		return errors.New("remote HTML viewing requires both an HTML content origin and an application origin")
	}
	marker := "rhizome-viewer-token"
	if strings.Count(contentTemplate, "{token}") != 1 {
		return errors.New("HTML content origin must contain exactly one {token} placeholder")
	}
	content, err := url.Parse(strings.Replace(contentTemplate, "{token}", marker, 1))
	if err != nil || !validHTTPOrigin(content) || content.Scheme != "https" {
		return errors.New("remote HTML content origin must be an absolute HTTPS origin")
	}
	labels := strings.Split(strings.ToLower(content.Hostname()), ".")
	foundMarker := false
	for _, label := range labels {
		if label == marker {
			foundMarker = true
		}
	}
	if !foundMarker {
		return errors.New("HTML content origin {token} must occupy a complete hostname label")
	}
	app, err := url.Parse(application)
	if err != nil || !validHTTPOrigin(app) || app.Scheme != "https" {
		return errors.New("remote application origin must be an absolute HTTPS origin")
	}
	if strings.EqualFold(content.Hostname(), app.Hostname()) {
		return errors.New("HTML content and application origins must use different hostnames")
	}
	return nil
}

func validHTTPOrigin(parsed *url.URL) bool {
	return parsed != nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" &&
		parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func sameWebOrigin(left, right *url.URL) bool {
	if !validHTTPOrigin(left) || !validHTTPOrigin(right) || !strings.EqualFold(left.Scheme, right.Scheme) || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	effectivePort := func(value *url.URL) string {
		if value.Port() != "" {
			return value.Port()
		}
		if strings.EqualFold(value.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return effectivePort(left) == effectivePort(right)
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHostname(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func isLoopbackHostname(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func normalizeHostPort(host string) string { return strings.ToLower(strings.TrimSuffix(host, ".")) }

func (s *Server) viewerRequestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	// Configuring both public origins is the explicit reverse-proxy boundary.
	// At that boundary, the proxy must replace this header and keep the Rhizome
	// listener private; arbitrary forwarded hosts are never used for routing.
	if s.cfg.HTMLContentOriginTemplate != "" && s.cfg.ApplicationOrigin != "" &&
		strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https") {
		return "https"
	}
	return "http"
}

func (s *Server) isHTMLViewerHost(hostport string) bool {
	hostname := strings.ToLower(requestHostname(hostport))
	if strings.HasSuffix(hostname, ".localhost") {
		return hostname != "localhost"
	}
	template := strings.TrimSpace(s.cfg.HTMLContentOriginTemplate)
	if template == "" || strings.Count(template, "{token}") != 1 {
		return false
	}
	parsed, err := url.Parse(strings.Replace(template, "{token}", "viewer-token", 1))
	if err != nil {
		return false
	}
	pattern := strings.ToLower(parsed.Hostname())
	labels := strings.Split(pattern, ".")
	marker := -1
	for index, label := range labels {
		if label == "viewer-token" {
			marker = index
			break
		}
	}
	if marker < 0 {
		return false
	}
	suffix := strings.Join(labels[marker+1:], ".")
	return suffix != "" && (hostname == suffix || strings.HasSuffix(hostname, "."+suffix))
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate viewer capability: %w", err)
	}
	return hex.EncodeToString(value), nil
}

type viewerResource struct {
	rel         string
	content     []byte
	contentType string
	mtime       int64
}

func (s *Server) viewerResource(rel string, requireHTML, read bool) (viewerResource, error) {
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, rel)
	if err != nil || readPath.rel == "" {
		return viewerResource{}, errors.New("resource is outside the vault")
	}
	// Capability resources deliberately reject symlink aliases. This keeps the
	// path checked for control/ignore policy identical to the path opened.
	if readPath.rel != readPath.resolvedRel {
		return viewerResource{}, errors.New("symlinked resources are unavailable")
	}
	if hasControlPath(readPath.rel) || hasControlPath(readPath.resolvedRel) {
		return viewerResource{}, errors.New("control files are unavailable")
	}
	file, err := os.Open(readPath.abs)
	if err != nil {
		return viewerResource{}, errors.New("resource is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return viewerResource{}, errors.New("resource is not a regular file")
	}
	if requireHTML && (s.catalog == nil || !s.catalog.supportsActiveViewer(readPath.rel)) {
		return viewerResource{}, errors.New("note format does not support active viewing")
	}
	if s.runtime != nil {
		if matcher := s.runtime.IgnoreMatcher(); matcher != nil &&
			(matcher.IsIgnored(readPath.rel, false) || matcher.IsIgnored(readPath.resolvedRel, false)) {
			return viewerResource{}, errors.New("resource is ignored")
		}
	}
	resource := viewerResource{rel: readPath.rel, contentType: viewerMIMEType(readPath.rel), mtime: info.ModTime().Unix()}
	if read {
		resource.content, err = io.ReadAll(file)
		if err != nil {
			return viewerResource{}, errors.New("resource is unavailable")
		}
	}
	return resource, nil
}

func hasControlPath(rel string) bool {
	for _, component := range strings.Split(strings.TrimPrefix(filepath.ToSlash(rel), "./"), "/") {
		switch strings.ToLower(component) {
		case ".rhizome", ".obsidian", ".git":
			return true
		}
	}
	return false
}

func (s *Server) serveHTMLViewerHost(w http.ResponseWriter, r *http.Request) bool {
	if s == nil || s.htmlViewers == nil {
		return false
	}
	grant, ok := s.htmlViewers.byHost(r.Host)
	if !ok {
		return false
	}
	if grant.scheme != s.viewerRequestScheme(r) {
		http.NotFound(w, r)
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if !s.exactNotePathOwned(grant.notePath) {
		s.htmlViewers.revoke(grant.id)
		http.NotFound(w, r)
		return true
	}
	if _, err := s.viewerResource(grant.notePath, true, false); err != nil {
		s.htmlViewers.revoke(grant.id)
		http.NotFound(w, r)
		return true
	}
	if r.Method == http.MethodOptions {
		setViewerResourceHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	rel, err := url.PathUnescape(r.URL.EscapedPath())
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	rel = strings.TrimPrefix(rel, "/")
	resource, err := s.viewerResource(rel, rel == grant.notePath, true)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	current, active := s.htmlViewers.byHost(r.Host)
	if !active || current.id != grant.id {
		http.NotFound(w, r)
		return true
	}
	if _, err := s.viewerResource(grant.notePath, true, false); err != nil {
		s.htmlViewers.revoke(grant.id)
		http.NotFound(w, r)
		return true
	}
	setViewerResourceHeaders(w)
	w.Header().Set("Content-Type", resource.contentType)
	if activeViewerDocumentMIME(resource.contentType) {
		w.Header().Set("Content-Security-Policy", viewerDocumentCSP(grant.appOrigin))
	}
	content := resource.content
	if rel == grant.notePath {
		source, supported, sourceErr := s.catalog.activeViewerSource(rel, content, resource.mtime)
		if sourceErr != nil || !supported {
			http.Error(w, "HTML viewer source unavailable", http.StatusUnprocessableEntity)
			return true
		}
		offset, planErr := s.catalog.viewerBootstrapOffset(source)
		if planErr != nil {
			http.Error(w, "HTML viewer bootstrap unavailable", http.StatusUnprocessableEntity)
			return true
		}
		content, err = injectHTMLViewerBootstrap(content, offset, grant)
		if err != nil {
			http.Error(w, "HTML viewer bootstrap unavailable", http.StatusUnprocessableEntity)
			return true
		}
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(content)))
	if r.Method == http.MethodGet {
		_, _ = w.Write(content)
	}
	return true
}

func activeViewerDocumentMIME(contentType string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return base == "text/html" || base == "application/xhtml+xml" || base == "image/svg+xml"
}

func viewerMIMEType(rel string) string {
	if contentType := viewerMIMETypes[strings.ToLower(filepath.Ext(rel))]; contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

var viewerMIMETypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".xhtml": "application/xhtml+xml",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".map":   "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".wasm":  "application/wasm",
	".csv":   "text/csv; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
}

func setViewerResourceHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func viewerDocumentCSP(appOrigin string) string {
	return "sandbox allow-scripts; default-src 'self' https: data: blob:; " +
		"connect-src 'self' https: data: blob:; img-src 'self' https: data: blob:; " +
		"font-src 'self' https: data:; style-src 'self' https: 'unsafe-inline'; " +
		"script-src 'self' https: 'unsafe-inline' 'unsafe-eval' blob:; " +
		"frame-ancestors " + appOrigin
}
