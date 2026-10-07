package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/viewscript"
)

// WHY: custom view code is repository configuration, trusted like the repo's
// build scripts (SPEC-0105 trust model). It is served same-origin so it can use
// the whole public API with no bridge, and so the URL that the Notes rail frames
// is also the standalone tool. The isolated viewer host stays for HTML notes,
// whose content is not assumed trusted. Only files under .rhizome/views are
// reachable here, and nothing runs until someone opens a view.

const (
	customViewFilesPrefix   = "_files/"
	customViewBundledPrefix = "_bundled/"
	customViewStampPrefix   = "_stamp/"
	customViewCheckPrefix   = "_check/"
	customViewKitBoot       = "kit/v1/boot.js"
	bundledViewsDirEnv      = "RHIZOME_BUNDLED_VIEWS_DIR"
)

// BundledViews returns the views shipped with this build: the kit build's copy
// of web/bundled-views inside assets, or the folder RHIZOME_BUNDLED_VIEWS_DIR
// names while developing them. A build without the web UI has none.
func BundledViews(assets fs.FS) fs.FS {
	if dir := strings.TrimSpace(os.Getenv(bundledViewsDirEnv)); dir != "" {
		return rootDirFS(dir)
	}
	if assets == nil {
		return nil
	}
	sub, err := fs.Sub(assets, "bundled-views")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "."); err != nil {
		return nil
	}
	return sub
}

// rootDirFS reads a folder through os.Root, so no symlink leads out of it.
// Each open takes its own root, so callers have nothing to close.
type rootDirFS string

func (dir rootDirFS) Open(name string) (fs.File, error) {
	root, err := os.OpenRoot(string(dir))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.FS().Open(name)
}

func (s *Server) handleCustomViews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Trusted code with write authority must not be framed by another site,
	// where one disguised click would commit an edit.
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	rest := strings.TrimPrefix(r.URL.Path, "/views/")
	switch {
	case strings.HasPrefix(rest, customViewFilesPrefix):
		s.serveRepositoryViewFile(w, r, strings.TrimPrefix(rest, customViewFilesPrefix))
	case strings.HasPrefix(rest, customViewBundledPrefix+customViewCheckPrefix):
		w.Header().Set("Cache-Control", "no-store")
		s.serveBundledViewCheck(w, strings.TrimPrefix(rest, customViewBundledPrefix+customViewCheckPrefix))
	case strings.HasPrefix(rest, customViewBundledPrefix):
		s.serveViewFile(w, r, BundledViews(s.assets), strings.TrimPrefix(rest, customViewBundledPrefix))
	case strings.HasPrefix(rest, customViewStampPrefix):
		w.Header().Set("Cache-Control", "no-store")
		s.serveCustomViewStamp(w, strings.TrimPrefix(rest, customViewStampPrefix))
	case strings.HasPrefix(rest, customViewCheckPrefix):
		w.Header().Set("Cache-Control", "no-store")
		s.serveCustomViewCheck(w, strings.TrimPrefix(rest, customViewCheckPrefix))
	default:
		w.Header().Set("Cache-Control", "no-store")
		s.serveCustomViewShell(w, r, rest)
	}
}

func (s *Server) customViewsRoot() string {
	return viewconfig.DefaultSourceRoot(s.cfg.VaultPath)
}

func (s *Server) serveRepositoryViewFile(w http.ResponseWriter, r *http.Request, rel string) {
	// os.Root refuses traversal and symlinks that leave .rhizome/views.
	root, err := os.OpenRoot(s.customViewsRoot())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	s.serveViewFile(w, r, root.FS(), rel)
}

// serveViewFile serves one file of a view folder, repository or bundled. Every
// response carries an ETag and must be revalidated, so an unchanged file
// answers 304 and an edited one is fetched again.
func (s *Server) serveViewFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, rel string) {
	// Definitions, dotfiles, and installed packages are not view assets.
	if fsys == nil || !viewconfig.ServablePath(rel) {
		http.NotFound(w, r)
		return
	}
	if info, err := fs.Stat(fsys, rel); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	content, err := fs.ReadFile(fsys, rel)
	if err != nil {
		http.Error(w, "view file unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if viewscript.IsCSSModuleRequest(rel, r.URL.RawQuery) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		serveViewContent(w, r, []byte(viewscript.CSSModule))
		return
	}
	if viewscript.NeedsTransform(rel, content) {
		code, err := s.viewModules.transform(rel, content)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, err.Error()+"\n")
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		serveViewContent(w, r, code)
		return
	}
	content, ok := s.injectCustomHTMLContext(w, r, content)
	if !ok {
		return
	}
	contentType := mime.TypeByExtension(path.Ext(rel))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	serveViewContent(w, r, content)
}

// viewETagSalt changes with every process, so a new rzm build never answers
// 304 for output an older transform produced.
var viewETagSalt = strconv.FormatInt(time.Now().UnixNano(), 36)

// serveViewContent answers with the content's validator; http.ServeContent
// turns a matching If-None-Match into 304 and handles HEAD.
func serveViewContent(w http.ResponseWriter, r *http.Request, content []byte) {
	sum := sha256.Sum256(content)
	w.Header().Set("ETag", `"`+viewETagSalt+"-"+hex.EncodeToString(sum[:12])+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
}

// viewModuleCache keeps transformed scripts by the hash of their name and
// source, so revalidating or reloading an unchanged view skips esbuild.
type viewModuleCache struct {
	mu      sync.Mutex
	modules map[[32]byte][]byte
}

// ponytail: the cache empties itself when full instead of evicting the least
// recently used module; switch to an LRU if views grow past this bound.
const viewModuleCacheLimit = 512

func (c *viewModuleCache) transform(rel string, source []byte) ([]byte, error) {
	key := sha256.Sum256(append([]byte(rel+"\x00"), source...))
	c.mu.Lock()
	code, ok := c.modules[key]
	c.mu.Unlock()
	if ok {
		return code, nil
	}
	code, err := viewscript.Transform(rel, source)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.modules == nil || len(c.modules) >= viewModuleCacheLimit {
		c.modules = map[[32]byte][]byte{}
	}
	c.modules[key] = code
	c.mu.Unlock()
	return code, nil
}

// openCustomViewFolder resolves a folder request for the stamp and check
// routes. Its fs.FS comes from os.Root, so neither route can read through a
// symlink out of .rhizome/views, and private folders are refused like files.
func (s *Server) openCustomViewFolder(w http.ResponseWriter, rel string) (*os.Root, string, bool) {
	dir := strings.TrimSuffix(rel, "/")
	if dir == "" {
		dir = "."
	}
	if !fs.ValidPath(dir) || (dir != "." && !viewconfig.ServablePath(dir)) {
		http.Error(w, "invalid view folder", http.StatusBadRequest)
		return nil, "", false
	}
	root, err := os.OpenRoot(s.customViewsRoot())
	if err != nil {
		http.Error(w, "view folder not found", http.StatusNotFound)
		return nil, "", false
	}
	if info, err := fs.Stat(root.FS(), dir); err != nil || !info.IsDir() {
		_ = root.Close()
		http.Error(w, "view folder not found", http.StatusNotFound)
		return nil, "", false
	}
	return root, dir, true
}

// A browser reports a module that failed to transform as a failed import, or as
// a missing export in whatever imported it. The kit asks this route for the
// folder's real diagnostics when a view fails to load.
func (s *Server) serveCustomViewCheck(w http.ResponseWriter, rel string) {
	root, dir, ok := s.openCustomViewFolder(w, rel)
	if !ok {
		return
	}
	defer root.Close()
	diagnostics, unreadable, err := viewscript.CheckDir(root.FS(), dir)
	if err != nil {
		unreadable = append(unreadable, fmt.Sprintf("%s: cannot read view folder: %v", dir, err))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, strings.Join(append(diagnostics, unreadable...), "\n"))
}

func (s *Server) serveBundledViewCheck(w http.ResponseWriter, rel string) {
	dir := strings.TrimSuffix(rel, "/")
	fsys := BundledViews(s.assets)
	if fsys == nil || !fs.ValidPath(dir) || !viewconfig.ServablePath(dir) {
		http.Error(w, "view folder not found", http.StatusNotFound)
		return
	}
	diagnostics, unreadable, err := viewscript.CheckDir(fsys, dir)
	if err != nil {
		unreadable = append(unreadable, fmt.Sprintf("%s: cannot read view folder: %v", dir, err))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, strings.Join(append(diagnostics, unreadable...), "\n"))
}

// The stamp is a standalone page's reload fallback. Hosted views reload on the
// views.changed event instead (custom_view_watch.go).
func (s *Server) serveCustomViewStamp(w http.ResponseWriter, rel string) {
	root, dir, ok := s.openCustomViewFolder(w, rel)
	if !ok {
		return
	}
	defer root.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, customViewFolderStamp(root.FS(), dir))
}

func customViewFolderStamp(fsys fs.FS, dir string) string {
	hash := fnv.New64a()
	_ = viewconfig.WalkFolder(fsys, dir, func(rel string, entry fs.DirEntry) {
		if info, err := entry.Info(); err == nil {
			_, _ = fmt.Fprintf(hash, "%s|%d|%d\n", rel, info.ModTime().UnixNano(), info.Size())
		}
	})
	return fmt.Sprintf("%x", hash.Sum64())
}

// errCustomViewUnavailable means the view may be valid but cannot be checked
// until the ontology loads; handlers answer 503 rather than 404.
var errCustomViewUnavailable = errors.New("view ontology is unavailable")

// findCustomView resolves through the same catalog validation as /api views
// (schema references, generated-id collisions, recipes, node types), so the
// shell and context injection refuse a view the catalog rejects.
func (s *Server) findCustomView(ctx context.Context, id string) (viewconfig.ViewDefinition, error) {
	service, err := s.configuredViewsService(ctx, nil)
	if err != nil {
		return s.findCustomViewWithoutOntology(id, err)
	}
	entry, err := service.View(ctx, id)
	if err != nil || entry.Generated || entry.Definition.SourceSpec.Kind != viewconfig.SourceKindCustom {
		return viewconfig.ViewDefinition{}, errors.New("custom view not found")
	}
	if issue := entry.BlockingIssue(); issue != nil {
		return entry.Definition, fmt.Errorf("custom view %s is invalid: %s", id, issue.Message)
	}
	return entry.Definition, nil
}

// findCustomViewWithoutOntology keeps standalone and workspace links working
// while the ontology cannot load (for example a schema mid-edit). These mounts need
// no schema, so schema-less validation is complete for them; subject mounts
// cannot be checked and report unavailable.
func (s *Server) findCustomViewWithoutOntology(id string, cause error) (viewconfig.ViewDefinition, error) {
	defs, _ := viewconfig.LoadDefaultSource(s.cfg.VaultPath)
	for i := range defs {
		defs[i].Origin = viewconfig.OriginRepository
	}
	validation := viewconfig.Validate(defs, viewconfig.ValidateOptions{})
	bundled, _ := viewconfig.LoadFS(BundledViews(s.assets))
	for i := range bundled {
		bundled[i].Origin = viewconfig.OriginBundled
	}
	for _, def := range append(validation.Views, bundled...) {
		if def.ID != id || def.SourceSpec.Kind != viewconfig.SourceKindCustom {
			continue
		}
		if def.Mount.Kind != viewconfig.MountKindStandalone && def.Mount.Kind != viewconfig.MountKindWorkspace {
			return def, fmt.Errorf("%w: %v", errCustomViewUnavailable, cause)
		}
		for _, issue := range validation.Issues {
			if issue.View == id && (issue.Severity == "" || issue.Severity == viewconfig.IssueFatal) {
				return def, fmt.Errorf("custom view %s is invalid: %s", id, issue.Message)
			}
		}
		return def, nil
	}
	return viewconfig.ViewDefinition{}, errors.New("custom view not found")
}

func customViewLookupStatus(err error, otherwise int) int {
	if errors.Is(err, errCustomViewUnavailable) {
		return http.StatusServiceUnavailable
	}
	return otherwise
}

func customViewEntryURL(s *Server, def viewconfig.ViewDefinition) string {
	if def.Origin == viewconfig.OriginBundled {
		return "/views/" + customViewBundledPrefix + escapeURLPath(path.Join(path.Dir(def.Source.Path), def.SourceSpec.Entry))
	}
	rel, _ := filepath.Rel(s.customViewsRoot(), def.EntryPath())
	return "/views/" + customViewFilesPrefix + escapeURLPath(filepath.ToSlash(rel))
}

// customViewFolder is the definition's folder relative to the views root, the
// unit that views.changed names and the stamp and check routes take. Bundled
// folders are relative to the bundled views root.
func (s *Server) customViewFolder(def viewconfig.ViewDefinition) (string, error) {
	if def.Origin == viewconfig.OriginBundled {
		return path.Dir(def.Source.Path), nil
	}
	rel, err := filepath.Rel(s.customViewsRoot(), filepath.Dir(def.Source.Path))
	return filepath.ToSlash(rel), err
}

func (s *Server) serveCustomViewShell(w http.ResponseWriter, r *http.Request, id string) {
	def, err := s.findCustomView(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), customViewLookupStatus(err, http.StatusNotFound))
		return
	}
	entryURL := customViewEntryURL(s, def)
	invocation, err := s.customViewInvocationForRequest(r, def)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !viewconfig.IsScriptEntry(def.SourceSpec.Entry) {
		query := r.URL.Query()
		query.Set("viewId", def.ID)
		if invocation.Context.Kind != viewconfig.MountKindStandalone {
			contextJSON, _ := json.Marshal(invocation.Context)
			query.Set("context", string(contextJSON))
		}
		entryURL += "?" + query.Encode()
		http.Redirect(w, r, entryURL, http.StatusFound)
		return
	}
	if s.assets == nil {
		http.Error(w, "this rzm build has no web kit (built with NO_WEB=1)", http.StatusServiceUnavailable)
		return
	}
	if _, err := fs.Stat(s.assets, customViewKitBoot); err != nil {
		http.Error(w, "this rzm build has no web kit (built with NO_WEB=1)", http.StatusServiceUnavailable)
		return
	}
	// The reload and check unit is the definition's folder, not the entry's, so
	// a nested entry (src/main.tsx, dist/index.html) still sees sibling edits.
	folderRel, err := s.customViewFolder(def)
	if err != nil {
		http.Error(w, "custom view is outside the views folder", http.StatusNotFound)
		return
	}
	folder := escapeURLPath(folderRel)
	if folderRel == "." {
		// "/views/_stamp/." is not a clean path; the mux would redirect it.
		folder = ""
	}
	settings := map[string]string{"id": def.ID, "name": def.Name, "origin": string(def.Origin)}
	if def.Origin == viewconfig.OriginBundled {
		// Bundled files change only with the binary, so nothing polls them.
		settings["check"] = "/views/" + customViewBundledPrefix + customViewCheckPrefix + folder
	} else {
		root, err := os.OpenRoot(s.customViewsRoot())
		if err != nil {
			http.Error(w, "views folder unavailable", http.StatusNotFound)
			return
		}
		defer root.Close()
		// folder matches the views.changed event a hosted view reloads on.
		settings["folder"] = folderRel
		settings["stamp"] = "/views/" + customViewStampPrefix + folder
		// The baseline is taken here, not on the page's first poll, so an edit
		// made while the page loads still triggers a reload.
		settings["stampValue"] = customViewFolderStamp(root.FS(), folderRel)
		settings["check"] = "/views/" + customViewCheckPrefix + folder
	}
	config, _ := json.Marshal(settings)
	entryJSON, _ := json.Marshal(entryURL)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, customViewShell, html.EscapeString(def.Name), customViewKitBoot, invocationScript(invocation), entryJSON, config)
}

func escapeURLPath(rel string) string {
	return (&url.URL{Path: rel}).EscapedPath()
}

// boot.js owns the import map, theme, and Tailwind compiler so an authored HTML
// page gets the same environment from the same one script tag.
const customViewShell = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<title>%s</title>
<script src="/%s"></script>
</head>
<body>
<div id="root"></div>
%s
<script type="module">
import { mountView } from "@rhizome/kit";
mountView(() => import(%s), %s);
</script>
</body>
</html>
`
