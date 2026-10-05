package web

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const markdownCompatibilityFormatID noteformat.FormatID = "markdown"

type markdownEmbedCompat struct {
	Target   string
	LinkType string
}

// resolveMarkdownLinksCompat preserves raw Markdown link parsing for rendered
// compatibility responses. Provider-aware file views use resolveProjectedLinksWithBase.
// Only the selected Markdown FormatID may enter this adapter.
func (s *Server) resolveMarkdownLinksCompat(rel string, content string) []ResolvedLink {
	if !s.markdownCompatibilityFormat(rel) {
		return nil
	}

	vaultDef := s.cfg.VaultDef
	cache := s.notePathCache(context.Background())
	wikiOpts := obsidian.DefaultWikilinkOptions
	mdOpts := obsidian.DefaultMdLinkOptions
	links := obsidian.ScanAllLinks(content, wikiOpts, mdOpts)
	out := make([]ResolvedLink, 0, len(links))
	for _, link := range links {
		target := link.Target
		baseTarget, fragment, fragmentType := splitLinkFragment(link.Target)
		switch link.LinkType {
		case "wikilink":
			if baseTarget == "" && fragment != "" {
				target = joinLinkFragment(rel, fragment, fragmentType)
			} else if resolved, ok := cache.ResolveNote(baseTarget); ok {
				target = joinLinkFragment(string(paths.NormalizeNotePath(resolved)), fragment, fragmentType)
			}
		case "mdlink":
			if vaultDef.SupportsMarkdownLinks() {
				if resolved, ok := cache.ResolveMdLink(link.Target, rel); ok {
					target = joinLinkFragment(string(paths.NormalizeNotePath(resolved)), fragment, fragmentType)
				}
			}
		}
		out = append(out, ResolvedLink{
			Target: target,
			Kind:   link.LinkType,
			Text:   link.Target,
			Anchor: joinAnchorFragment(fragment, fragmentType),
		})
	}
	return out
}

// markdownEmbedsCompat is the matching explicit Markdown-only adapter for
// rendered embed cards. Only the selected Markdown FormatID may enter this
// parser; provider-aware file views use projection facts instead.
func (s *Server) markdownEmbedsCompat(rel string, content string) []markdownEmbedCompat {
	if !s.markdownCompatibilityFormat(rel) {
		return nil
	}
	links := obsidian.ScanAllLinks(content, obsidian.DefaultWikilinkOptions, obsidian.DefaultMdLinkOptions)
	out := make([]markdownEmbedCompat, 0, len(links))
	for _, link := range links {
		if link.Subtype != "embed" {
			continue
		}
		out = append(out, markdownEmbedCompat{Target: link.Target, LinkType: link.LinkType})
	}
	return out
}

// markdownAliasesForNotePathCompat updates the legacy in-memory Markdown link
// cache from raw source. The FormatID guard keeps generic formats out of this
// compatibility parser.
func (s *Server) markdownAliasesForNotePathCompat(ctx context.Context, notePath string) []string {
	if s.catalog == nil || !s.markdownCompatibilityFormat(notePath) {
		return nil
	}
	if kind, _ := s.classifyFile(notePath); kind != "note" {
		return nil
	}
	reader := s.noteReader
	if reader == nil {
		reader = &obsidian.Note{}
	}
	content, err := reader.GetContents(s.cfg.VaultDef, notePath)
	if err != nil {
		return nil
	}
	frontmatter, _ := obsidian.ExtractFrontmatter(content)
	return obsidian.AliasListFromFrontmatter(frontmatter)
}

// markdownDocumentSnapshotCompat is the only web adapter that may build an
// ontology Markdown document snapshot. Generic note formats use projection
// facts instead of being treated as Markdown because they can project.
func (s *Server) markdownDocumentSnapshotCompat(path, content string, mtime time.Time) (*ontology.DocumentSnapshot, bool, error) {
	if !s.markdownCompatibilityFormat(path) {
		return nil, false, nil
	}
	snapshot, err := buildMarkdownDocumentSnapshotCompat(path, content, mtime)
	return snapshot, true, err
}

// buildMarkdownDocumentSnapshotCompat is the parser boundary for edit-session
// recovery evidence, which is explicitly Markdown source rather than a generic
// provider document.
func buildMarkdownDocumentSnapshotCompat(path, content string, mtime time.Time) (*ontology.DocumentSnapshot, error) {
	return ontology.BuildDocumentSnapshot(path, content, mtime)
}

// scanStructuredMarkdownLinksCompat is the matching boundary for protecting
// link targets while a user edits a note's complete Markdown source.
func scanStructuredMarkdownLinksCompat(content string) []obsidian.StructuredLink {
	return obsidian.ScanStructuredLinks(content)
}

func (s *Server) markdownCompatibilityFormat(rel string) bool {
	if s == nil || s.catalog == nil {
		// A server without a catalog has no configured generic provider route.
		// Preserve the legacy Markdown-only compatibility surface for that mode.
		return true
	}
	format, note := s.catalog.noteFormat(rel)
	return note && format == markdownCompatibilityFormatID
}
