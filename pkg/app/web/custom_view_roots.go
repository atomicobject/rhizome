package web

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

// serveRepositoryViewRoute answers the file, stamp, and check routes inside
// one repository views folder. A module folder has only these routes; it
// reports false for anything else so the flat folder can try its own.
func (s *Server) serveRepositoryViewRoute(w http.ResponseWriter, r *http.Request, root viewRoot, rest string) bool {
	var serve func()
	switch {
	case strings.HasPrefix(rest, customViewFilesPrefix):
		serve = func() { s.serveRepositoryViewFile(w, r, root, strings.TrimPrefix(rest, customViewFilesPrefix)) }
	case strings.HasPrefix(rest, customViewStampPrefix):
		serve = func() {
			w.Header().Set("Cache-Control", "no-store")
			s.serveCustomViewStamp(w, root, strings.TrimPrefix(rest, customViewStampPrefix))
		}
	case strings.HasPrefix(rest, customViewCheckPrefix):
		serve = func() {
			w.Header().Set("Cache-Control", "no-store")
			s.serveCustomViewCheck(w, root, strings.TrimPrefix(rest, customViewCheckPrefix))
		}
	case root.module != "":
		http.NotFound(w, r)
		return true
	default:
		return false
	}
	if reason := s.viewsUntrusted(); reason != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, reason, http.StatusForbidden)
		return true
	}
	serve()
	return true
}

// defaultRepositoryTrusted reads the person's repository trust store.
var defaultRepositoryTrusted = func(checkout string) (bool, error) {
	store, err := repositorytrust.DefaultStore()
	if err != nil {
		return false, err
	}
	return store.Trusted(checkout)
}

// viewsUntrusted explains why the vault's repository and module views may not
// run, or returns "". Their code runs same-origin with the whole API, so they
// get the trust check actions and tasks get, on the folder `rzm trust` records.
func (s *Server) viewsUntrusted() string {
	trusted := s.cfg.RepositoryTrusted
	if trusted == nil {
		trusted = defaultRepositoryTrusted
	}
	return repositorytrust.DenialReason(s.cfg.VaultPath, "views", trusted)
}

// viewRoot is one repository views folder as the browser reaches it: the flat
// .rhizome/views (module "") or a module's views folder (SPEC-0130). Flat
// views keep their /views/_files/ URLs and bare folder keys; module views use
// /views/_module/<module>/ and the key /module/<module>/<folder>. A flat key
// is relative and never starts with "/", so no flat folder (not even one
// named "modules" or "module:x") can share a module key.
type viewRoot struct{ module string }

func definitionRoot(def viewconfig.ViewDefinition) viewRoot {
	return viewRoot{module: def.Source.Module}
}

// rel is the folder relative to the vault.
func (v viewRoot) rel() string {
	if v.module == "" {
		return assetroots.FlatDir(assetroots.Views)
	}
	return assetroots.ModuleDir(v.module, assetroots.Views)
}

func (v viewRoot) dir(vaultPath string) string {
	return filepath.Join(vaultPath, filepath.FromSlash(v.rel()))
}

// prefix is the URL path every route of this folder starts with.
func (v viewRoot) prefix() string {
	if v.module == "" {
		return "/views/"
	}
	return "/views/" + customViewModulePrefix + v.module + "/"
}

// folderKey names a definition folder in views.changed and the shell config.
func (v viewRoot) folderKey(folder string) string {
	if v.module == "" {
		return folder
	}
	return path.Join("/module", v.module, folder)
}

// open opens the folder as an os.Root once it resolves inside the vault, so
// nothing read from it can leave the folder and the folder cannot lead out of
// the repository. (Root.OpenRoot from a vault root would be one call, but Go
// 1.24's nested roots report directory entries whose Info fails.)
func (v viewRoot) open(vaultPath string) (*os.Root, error) {
	realVault, err := filepath.EvalSymlinks(vaultPath)
	if err != nil {
		return nil, err
	}
	realDir, err := filepath.EvalSymlinks(v.dir(vaultPath))
	if err != nil {
		return nil, err
	}
	if rel, err := filepath.Rel(realVault, realDir); err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("%s resolves outside the vault", v.rel())
	}
	return os.OpenRoot(realDir)
}

func (s *Server) serveRepositoryViewFile(w http.ResponseWriter, r *http.Request, v viewRoot, rel string) {
	// os.Root refuses traversal and symlinks that leave the views folder.
	root, err := v.open(s.cfg.VaultPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	s.serveViewFile(w, r, root.FS(), rel)
}
