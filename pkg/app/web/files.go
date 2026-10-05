package web

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

func (s *Server) listTree(ctx context.Context, rel string, limit int) (TreeResponse, error) {
	if limit <= 0 {
		limit = 200
	}
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, rel)
	if err != nil {
		return TreeResponse{}, err
	}
	rel = readPath.rel
	info, err := os.Stat(readPath.abs)
	if err != nil {
		return TreeResponse{}, err
	}
	if !info.IsDir() {
		return TreeResponse{}, errors.New("path is not a directory")
	}
	if ignoreMatcher := s.runtime.IgnoreMatcher(); ignoreMatcher != nil {
		if ignoreMatcher.IsIgnored(rel, true) || ignoreMatcher.IsIgnored(readPath.resolvedRel, true) {
			return TreeResponse{}, errors.New("path is ignored")
		}
	}

	entries, err := os.ReadDir(readPath.abs)
	if err != nil {
		return TreeResponse{}, err
	}
	out := TreeResponse{Path: rel}

	for _, entry := range entries {
		name := entry.Name()
		candidate, ok := s.treeEntry(rel, name)
		if !ok {
			continue
		}
		entryRel := candidate.path.rel
		kind, lang := s.classifyFile(entryRel)
		if candidate.isDir {
			kind = "dir"
		}
		if kind == "" && !candidate.isDir {
			continue
		}
		out.Entries = append(out.Entries, TreeEntry{
			Name:        name,
			Path:        entryRel,
			Kind:        kind,
			Lang:        lang,
			HasChildren: candidate.isDir && s.dirHasChildren(entryRel),
		})
		if len(out.Entries) >= limit {
			out.Truncated = true
			break
		}
	}

	sort.SliceStable(out.Entries, func(i, j int) bool {
		if out.Entries[i].Kind != out.Entries[j].Kind {
			return out.Entries[i].Kind == "dir"
		}
		return out.Entries[i].Name < out.Entries[j].Name
	})
	return out, nil
}

func (s *Server) dirHasChildren(rel string) bool {
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, rel)
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(readPath.abs)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		candidate, ok := s.treeEntry(rel, entry.Name())
		if !ok {
			continue
		}
		if candidate.isDir {
			return true
		}
		kind, _ := s.classifyFile(candidate.path.rel)
		if kind != "" {
			return true
		}
	}
	return false
}

type vaultTreeEntry struct {
	path  vaultReadPath
	isDir bool
}

func (s *Server) treeEntry(parentRel, name string) (vaultTreeEntry, bool) {
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, filepath.Join(parentRel, name))
	if err != nil {
		return vaultTreeEntry{}, false
	}
	info, err := os.Stat(readPath.abs)
	if err != nil {
		return vaultTreeEntry{}, false
	}
	if ignoreMatcher := s.runtime.IgnoreMatcher(); ignoreMatcher != nil &&
		(ignoreMatcher.IsIgnoredShallow(readPath.rel, info.IsDir()) ||
			ignoreMatcher.IsIgnored(readPath.resolvedRel, info.IsDir())) {
		return vaultTreeEntry{}, false
	}
	return vaultTreeEntry{path: readPath, isDir: info.IsDir()}, true
}

func (s *Server) readFileView(ctx context.Context, rel string) (FileViewResponse, error) {
	readPath, err := resolveVaultReadPath(s.cfg.VaultPath, rel)
	if err != nil {
		return FileViewResponse{}, err
	}
	if readPath.rel == "" {
		return FileViewResponse{}, errors.New("empty path")
	}
	rel = readPath.rel
	info, err := os.Stat(readPath.abs)
	if err != nil {
		return FileViewResponse{}, err
	}
	if info.IsDir() {
		return FileViewResponse{}, errors.New("path is a directory")
	}
	if ignoreMatcher := s.runtime.IgnoreMatcher(); ignoreMatcher != nil {
		if ignoreMatcher.IsIgnored(rel, false) || ignoreMatcher.IsIgnored(readPath.resolvedRel, false) {
			return FileViewResponse{}, errors.New("path is ignored")
		}
	}

	if s.catalog != nil {
		if provider, unsupported := s.catalog.unsupportedNoteProjection(rel); unsupported {
			return FileViewResponse{}, errors.New(unsupportedProjectionMessage(rel, provider, "file view"))
		}
	}
	kind, lang := s.classifyFile(rel)
	if kind == "" {
		kind = "file"
	}

	content, err := os.ReadFile(readPath.abs)
	if err != nil {
		return FileViewResponse{}, err
	}

	resp := FileViewResponse{
		Path:    rel,
		Kind:    kind,
		Title:   titleFromPath(rel),
		Lang:    lang,
		Content: string(content),
	}

	if kind == "note" && s.catalog != nil {
		projection, isNote, err := s.catalog.projectNote(rel, content, info.ModTime().Unix())
		if err != nil {
			return FileViewResponse{}, err
		}
		if !isNote || projection.Status != noteformat.ProjectionStatusCurrent {
			return FileViewResponse{}, notemeta.ErrProjectionNotCurrent
		}
		if projection.Facts.Title != nil && projection.Facts.Title.Value != "" {
			resp.Title = projection.Facts.Title.Value
		}
		resp.Frontmatter = frontmatterFromProjection(projection)
		resp.Links = s.resolveProjectedLinksWithBase(rel, projection.Facts.DocumentBase, projection.Facts.Links)
	}

	if kind == "code" {
		resp.CodeRefs = s.scanCodeRefs(rel, content)
		resp.RelatedNotes = s.relatedNotesForCode(ctx, readPath.abs, resp.CodeRefs)
	}

	return resp, nil
}

type vaultReadPath struct {
	rel         string
	resolvedRel string
	abs         string
}

func resolveVaultReadPath(vaultRoot, input string) (vaultReadPath, error) {
	input = filepath.ToSlash(strings.TrimPrefix(input, "/"))
	rel, err := paths.CleanRelPath(input)
	if err != nil {
		return vaultReadPath{}, err
	}
	vaultPaths, err := paths.NewVaultPaths(vaultRoot)
	if err != nil {
		return vaultReadPath{}, err
	}
	if vaultPaths.Root() == "" {
		return vaultReadPath{}, errors.New("vault root is required")
	}
	if rel == "" {
		return vaultReadPath{abs: vaultPaths.Root()}, nil
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(vaultPaths.Root(), filepath.FromSlash(rel.String())))
	if err != nil {
		return vaultReadPath{}, err
	}
	resolvedRel, err := vaultPaths.RelStrict(resolved)
	if err != nil {
		return vaultReadPath{}, err
	}
	return vaultReadPath{
		rel:         rel.String(),
		resolvedRel: resolvedRel.String(),
		abs:         resolved,
	}, nil
}

func frontmatterFromProjection(projection noteformat.Projection) map[string]interface{} {
	if len(projection.Facts.RootMetadata) == 0 {
		return nil
	}
	frontmatter := make(map[string]interface{}, len(projection.Facts.RootMetadata))
	for _, fact := range projection.Facts.RootMetadata {
		frontmatter[fact.Key] = fact.Value.Export()
	}
	return frontmatter
}

func (s *Server) resolveProjectedLinksWithBase(rel string, base *noteformat.DocumentBaseFact, links []noteformat.UnresolvedAuthoredLinkFact) []ResolvedLink {
	cache := s.notePathCache(context.Background())
	out := make([]ResolvedLink, 0, len(links))
	for _, link := range links {
		target := link.Target
		baseTarget, fragment, fragmentType := splitLinkFragment(link.Target)
		kind := ""
		switch link.Resolution {
		case noteformat.LinkResolutionNoteReference:
			kind = "wikilink"
			if baseTarget == "" && fragment != "" {
				target = joinLinkFragment(rel, fragment, fragmentType)
			} else if resolved, ok := cache.ResolveNote(link.ResolverInput); ok {
				target = joinLinkFragment(string(paths.NormalizeNotePath(resolved)), fragment, fragmentType)
			}
		case noteformat.LinkResolutionRelativePath:
			kind = "mdlink"
			if s.cfg.VaultDef.SupportsMarkdownLinks() {
				if resolved, ok := cache.ResolveMdLink(link.ResolverInput, rel); ok {
					target = joinLinkFragment(string(paths.NormalizeNotePath(resolved)), fragment, fragmentType)
				}
			}
		case noteformat.LinkResolutionURI:
			kind = "uri"
			fragment = ""
			fragmentType = ""
			if link.URI != nil {
				fragment = link.URI.Fragment
				fragmentType = uriFragmentType(fragment)
				if resolved, ok := notemeta.ResolveProjectedLink(cache, rel, base, link); ok {
					target = joinURIComponents(string(paths.NormalizeNotePath(resolved)), link.URI)
				}
			}
		default:
			continue
		}
		out = append(out, ResolvedLink{
			Target: target,
			Kind:   kind,
			Text:   link.Target,
			Anchor: joinAnchorFragment(fragment, fragmentType),
		})
	}
	return out
}

func uriFragmentType(fragment string) string {
	// HTML URI fragments name element IDs or legacy anchor names. A leading
	// caret is part of that authored ID, rather than Markdown block syntax.
	// Keep it intact when building both Target and Anchor.
	return "heading"
}

func joinURIComponents(base string, uri *noteformat.URIReferenceFact) string {
	if uri == nil {
		return base
	}
	authored, _ := url.Parse(uri.Raw)
	if uri.QueryRange.Present {
		query := uri.Query
		if authored != nil {
			query = authored.RawQuery
		}
		base += "?" + query
	}
	if uri.FragmentRange.Present {
		base += "#" + uri.Fragment
	}
	return base
}

func splitLinkFragment(target string) (base string, fragment string, fragmentType string) {
	base = target
	if idx := strings.Index(base, "#"); idx >= 0 {
		fragment = base[idx+1:]
		base = base[:idx]
		if strings.HasPrefix(fragment, "^") {
			fragmentType = "block"
			fragment = strings.TrimPrefix(fragment, "^")
		} else {
			fragmentType = "heading"
		}
	}
	return base, fragment, fragmentType
}

func joinLinkFragment(base string, fragment string, fragmentType string) string {
	if fragment == "" {
		return base
	}
	return base + "#" + joinAnchorFragment(fragment, fragmentType)
}

func joinAnchorFragment(fragment string, fragmentType string) string {
	if fragment == "" {
		return ""
	}
	if fragmentType == "block" {
		return "^" + fragment
	}
	return fragment
}

func (s *Server) scanCodeRefs(rel string, content []byte) []CodeRefLink {
	cache := s.notePathCache(context.Background())
	refs, err := coderefs.ScanFile(rel, content, cache)
	if err != nil {
		return nil
	}
	out := make([]CodeRefLink, 0, len(refs))
	for _, ref := range refs {
		kind := "mention"
		if ref.Kind == coderefs.RefKindWikilink {
			kind = "wikilink"
		}
		out = append(out, CodeRefLink{
			Target:  ref.Target,
			Kind:    kind,
			Line:    ref.Line,
			Snippet: ref.Snippet,
		})
	}
	return out
}

func (s *Server) relatedNotesForCode(ctx context.Context, absPath string, refs []CodeRefLink) []RelatedNote {
	seen := map[string]RelatedNote{}
	add := func(path, reason, label string) {
		if path == "" {
			return
		}
		key := path + "|" + reason + "|" + label
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = RelatedNote{
			Path:        path,
			Title:       titleFromPath(path),
			Reason:      reason,
			AnchorLabel: label,
		}
	}

	for _, ref := range refs {
		target := string(paths.NormalizeNotePath(ref.Target))
		add(target, "coderef", "")
	}

	if s.runtime != nil && s.runtime.Intel() != nil {
		svc := s.codeAnchorService()
		if svc != nil {
			if fc, err := svc.NotesForFile(ctx, absPath); err == nil {
				if len(fc.AnchorNotes) > 0 {
					for _, group := range fc.AnchorNotes {
						for _, note := range group.Notes {
							add(note.Path, "codeanchor", group.Label)
						}
					}
				} else {
					for _, note := range fc.Notes {
						add(note.Path, "codeanchor", "")
					}
				}
			}
		}
	}

	out := make([]RelatedNote, 0, len(seen))
	for _, note := range seen {
		out = append(out, note)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Reason != out[j].Reason {
			return out[i].Reason < out[j].Reason
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func (s *Server) notePathCache(ctx context.Context) *obsidian.NotePathCache {
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	if s.noteCache != nil {
		return s.noteCache
	}

	if cache, ok := s.indexedNotePathCache(ctx); ok {
		s.noteCache = cache
		return cache
	}

	notePaths, err := s.listNotePaths(ctx)
	if err != nil {
		return obsidian.BuildNotePathCache(nil)
	}
	// Pull frontmatter aliases from the intel store when available so
	// wikilinks like [[SPEC-001]] resolve. Swallow errors — callers
	// still get filename-only resolution if the read fails.
	var aliasesByPath map[string][]string
	if s.runtime != nil {
		aliasesByPath = notemeta.LoadAliasMap(ctx, s.runtime.Intel())
	}
	cache := obsidian.BuildNotePathCacheWithAliases(notePaths, aliasesByPath)
	s.noteCache = cache
	return cache
}

func (s *Server) indexedNotePathCache(ctx context.Context) (*obsidian.NotePathCache, bool) {
	if s == nil || s.runtime == nil {
		return nil, false
	}
	store := s.runtime.Intel()
	if store == nil {
		return nil, false
	}
	pathsList, err := store.CurrentNoteMetadataPaths(ctx)
	if err != nil || len(pathsList) == 0 {
		return nil, false
	}
	aliasesByPath, err := store.CurrentNoteAliases(ctx)
	if err != nil {
		aliasesByPath = nil
	}
	return obsidian.BuildNotePathCacheWithAliases(pathsList, aliasesByPath), true
}

func (s *Server) listNotePaths(ctx context.Context) ([]string, error) {
	if s.cfg.Cache != nil {
		_ = s.cfg.Cache.Refresh(ctx)
		return s.cfg.Cache.Paths(), nil
	}
	return obsidian.DiscoverFiles(s.cfg.VaultDef)
}

func (s *Server) updateNotePathCacheForWatchEvents(ctx context.Context, events []watchhub.WatchEvent) {
	if s == nil || len(events) == 0 {
		return
	}
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	if s.noteCache == nil {
		return
	}
	for _, event := range events {
		notePath := strings.TrimSpace(filepath.ToSlash(event.RelPath))
		if notePath == "" || event.IsDir {
			continue
		}
		if event.Op.Has(watchhub.OpRemove) || event.Op.Has(watchhub.OpRename) {
			s.noteCache.Remove(notePath)
			continue
		}
		if event.Op.Has(watchhub.OpCreate) || event.Op.Has(watchhub.OpWrite) {
			s.noteCache.AddOrUpdate(notePath, s.markdownAliasesForNotePathCompat(ctx, notePath))
		}
	}
}
