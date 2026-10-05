package web

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// FileCatalog caches discoverable files for suggestions.
type FileCatalog struct {
	root     string
	ignore   *ignore.Matcher
	selector noteownership.Selector
	formats  noteformat.Runtime
	mu       sync.Mutex
	files    []FileMatch
	updated  time.Time
}

// NewFileCatalog creates a catalog for the given vault root.
func NewFileCatalog(vaultDef obsidian.VaultDefinition, matcher *ignore.Matcher, indexer notemeta.Indexer) (*FileCatalog, error) {
	formats, err := indexer.FormatRuntime()
	if err != nil {
		return nil, err
	}
	root := vaultDef.BasePath()
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return nil, err
	}
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{
		VaultDefinition: vaultDef,
		Registry:        formats.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(vaultPaths.Root())},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			return codeanchor.Lang(coderefs.DetectLanguage(ref.Rel.String()))
		},
	})
	if err != nil {
		return nil, err
	}
	return &FileCatalog{root: root, ignore: matcher, selector: selector, formats: formats}, nil
}

// Suggest returns fuzzy matches for a query.
func (c *FileCatalog) Suggest(query string, limit int) []FileMatch {
	if limit <= 0 {
		limit = 25
	}
	c.ensureFresh()
	if query == "" {
		if len(c.files) < limit {
			return append([]FileMatch{}, c.files...)
		}
		return append([]FileMatch{}, c.files[:limit]...)
	}
	lower := strings.ToLower(query)
	c.mu.Lock()
	defer c.mu.Unlock()
	var matches []FileMatch
	for _, f := range c.files {
		score, ok := scoreMatch(lower, f.Path, f.Title)
		if !ok {
			continue
		}
		item := f
		item.Score = score
		matches = append(matches, item)
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score < matches[j].Score
		}
		return len(matches[i].Path) < len(matches[j].Path)
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func (c *FileCatalog) ensureFresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.updated) < 3*time.Second && len(c.files) > 0 {
		return
	}
	c.files = c.collect()
	c.updated = time.Now()
}

func (c *FileCatalog) collect() []FileMatch {
	var out []FileMatch
	vaultPaths, _ := paths.NewVaultPaths(c.root)
	_ = filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relPath, err := vaultPaths.RelStrict(path)
		if err != nil {
			return nil
		}
		rel := relPath.String()
		if rel == "." {
			return nil
		}
		if c.ignore != nil && c.ignore.IsIgnoredShallow(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		kind, lang := c.classify(rel)
		if kind == "" {
			return nil
		}
		out = append(out, FileMatch{
			Path:  rel,
			Title: titleFromPath(rel),
			Kind:  kind,
			Lang:  lang,
		})
		return nil
	})

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}

func scoreMatch(query, path, title string) (int, bool) {
	pathLower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))
	titleLower := strings.ToLower(title)

	if strings.HasPrefix(base, query) || strings.HasPrefix(titleLower, query) {
		return 0, true
	}
	if strings.Contains(base, query) || strings.Contains(titleLower, query) {
		return 1, true
	}
	if obsidian.FuzzyMatch(query, pathLower) {
		return 2, true
	}
	return 0, false
}

func (c *FileCatalog) classify(rel string) (string, string) {
	format, note := c.noteFormat(rel)
	if note {
		if !c.formats.CanProject(format) {
			return "", ""
		}
		return "note", string(format)
	}
	path, err := paths.CleanRelPath(rel)
	if err != nil || path == "" {
		return "", ""
	}
	selection, err := c.selector.Select(path)
	if err != nil {
		return "", ""
	}
	if selection.Owner == notediscovery.Code {
		return "code", string(selection.Language)
	}
	return "", ""
}

func (c *FileCatalog) noteFormat(rel string) (noteformat.FormatID, bool) {
	path, err := paths.CleanRelPath(rel)
	if err != nil || path == "" {
		return "", false
	}
	selection, err := c.selector.Select(path)
	if err != nil || selection.Owner != notediscovery.Note {
		return "", false
	}
	return selection.Provider, true
}

func (c *FileCatalog) unsupportedNoteProjection(rel string) (string, bool) {
	path, err := paths.CleanRelPath(rel)
	if err != nil || path == "" {
		return "", false
	}
	selection, err := c.selector.Select(path)
	if err != nil || selection.Owner != notediscovery.Note || c.formats.CanProject(selection.Provider) {
		return "", false
	}
	return string(selection.Provider), true
}

// projectNote turns one selected note source into provider facts. File views
// use this narrow seam so web does not choose or parse a note format itself.
func (c *FileCatalog) projectNote(rel string, content []byte, mtime int64) (noteformat.Projection, bool, error) {
	path, err := paths.CleanNotePath(rel)
	if err != nil || path == "" {
		return noteformat.Projection{}, false, err
	}
	selection, err := c.selector.Select(paths.RelPath(path))
	if err != nil {
		return noteformat.Projection{}, false, err
	}
	if selection.Owner != notediscovery.Note {
		return noteformat.Projection{}, false, nil
	}
	provider, ok := c.formats.Provider(selection.Provider)
	if !ok {
		return noteformat.Projection{}, true, fmt.Errorf("selected note format %q is not registered", selection.Provider)
	}
	source, err := noteformat.NewAuthoredSource(path, provider.Descriptor(), content, mtime)
	if err != nil {
		return noteformat.Projection{}, true, err
	}
	projection, err := c.formats.Project(source)
	return projection, true, err
}

func (c *FileCatalog) activeViewerSource(rel string, content []byte, mtime int64) (noteformat.AuthoredSource, bool, error) {
	path, err := paths.CleanNotePath(rel)
	if err != nil || path == "" {
		return noteformat.AuthoredSource{}, false, err
	}
	selection, err := c.selector.Select(paths.RelPath(path))
	if err != nil || selection.Owner != notediscovery.Note {
		return noteformat.AuthoredSource{}, false, err
	}
	provider, ok := c.formats.Provider(selection.Provider)
	if !ok || !provider.Descriptor().Capabilities.Has(noteformat.CapabilityActiveContentViewing) {
		return noteformat.AuthoredSource{}, false, nil
	}
	source, err := noteformat.NewAuthoredSource(path, provider.Descriptor(), content, mtime)
	return source, true, err
}

func (c *FileCatalog) supportsActiveViewer(rel string) bool {
	format, note := c.noteFormat(rel)
	if !note || !c.formats.CanProject(format) {
		return false
	}
	provider, ok := c.formats.Provider(format)
	return ok && provider.Descriptor().Capabilities.Has(noteformat.CapabilityActiveContentViewing)
}

func (c *FileCatalog) viewerBootstrapOffset(source noteformat.AuthoredSource) (int, error) {
	return c.formats.ViewerBootstrapOffset(source)
}

func (s *Server) classifyFile(rel string) (string, string) {
	if s == nil || s.catalog == nil {
		return "", ""
	}
	return s.catalog.classify(rel)
}

func unsupportedProjectionMessage(path, provider, operation string) string {
	return fmt.Sprintf("%s is a note owned by format %q, but %s projection is not supported", path, provider, operation)
}

func titleFromPath(p string) string {
	base := filepath.Base(p)
	if extension := filepath.Ext(base); extension != "" {
		base = strings.TrimSuffix(base, extension)
	}
	return base
}
