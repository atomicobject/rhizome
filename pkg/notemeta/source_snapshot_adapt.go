package notemeta

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NewContentOnlyNoteSourceSnapshot builds the anchor-compatible subset of the
// raw-note source contract from a caller that only has content in hand. This
// is a content-only legacy Markdown snapshot adapter; generic provider paths
// must use projected source facts instead.
func NewContentOnlyNoteSourceSnapshot(path string, content string, mtime int64) NoteSourceSnapshot {
	return NoteSourceSnapshot{Path: normalizeContentOnlyNotePath(path), Format: noteformat.FormatID("markdown"), Content: content, RawSource: []byte(content), ContentHash: contentHash(content), Mtime: mtime, Size: int64(len(content)), Title: titleFor(path, content, nil)}
}

func normalizeContentOnlyNotePath(path string) paths.NotePath {
	normalized := paths.NormalizeNotePath(path)
	if strings.HasSuffix(strings.ToLower(normalized.String()), ".md") {
		return normalized
	}
	return paths.NormalizeNote(path)
}

func (s NoteSourceSnapshot) NotePathString() string            { return s.Path.String() }
func (s NoteSourceSnapshot) NoteFormatID() noteformat.FormatID { return s.Format }
func (s NoteSourceSnapshot) NoteContentString() string         { return s.Content }
func (s NoteSourceSnapshot) NoteContentHashString() string     { return s.ContentHash }
func (s NoteSourceSnapshot) NoteMtimeUnix() int64              { return s.Mtime }

func sourceSnapshotFromProjectedEntry(vaultDef obsidian.VaultDefinition, cache *obsidian.NotePathCache, entry projectedNoteEntry) NoteSourceSnapshot {
	snapshot := sourceFactsSnapshotFromProjectedEntry(entry)
	snapshot.Links = resolvedLinksFromProjection(vaultDef, cache, entry.Source.Path().String(), entry.Projection.Facts.DocumentBase, entry.Links)
	return snapshot
}

func sourceFactsSnapshotFromEntry(entry noteEntry) NoteSourceSnapshot {
	return NoteSourceSnapshot{Path: legacyEntryPath(entry.Path), Format: noteformat.FormatID("markdown"), Content: entry.Content, RawSource: []byte(entry.Content), ContentHash: contentHash(entry.Content), Mtime: entry.Mtime, Size: entry.Size, Title: entry.Title, Frontmatter: cloneMap(entry.Frontmatter), InlineProps: cloneInline(entry.InlineProps), Tags: dedupeStrings(entry.Tags), Aliases: extractAliasList(entry.Frontmatter["aliases"])}
}

func sourceFactsSnapshotFromProjectedEntry(entry projectedNoteEntry) NoteSourceSnapshot {
	compatibility := sourceFactsSnapshotFromEntry(entry.Entry)
	compatibility.Path, compatibility.Format, compatibility.Projection = entry.Source.Path(), entry.Source.Format(), entry.Projection.Copy()
	compatibility.Content, compatibility.ContentHash = string(entry.Source.Bytes()), entry.Source.ContentHash()
	compatibility.Mtime, compatibility.Size, compatibility.Aliases = entry.Source.Mtime(), entry.Source.Size(), aliasValuesFromFacts(entry.Aliases)
	compatibility.RawSource = entry.Source.Bytes()
	compatibility.FragmentTargets = append([]noteformat.FragmentTargetFact(nil), entry.Projection.Facts.FragmentTargets...)
	compatibility.SearchRegions = append([]noteformat.SearchRegionFact(nil), entry.Projection.Facts.SearchRegions...)
	compatibility.Diagnostics = append([]noteformat.Diagnostic(nil), entry.Projection.Diagnostics...)
	if entry.Projection.Facts.DocumentBase != nil {
		base := *entry.Projection.Facts.DocumentBase
		compatibility.DocumentBase = &base
	}
	return compatibility
}

func legacyEntryPath(path string) paths.NotePath {
	canonical, err := paths.CleanNotePath(path)
	if err == nil {
		return canonical
	}
	return paths.NormalizeNote(path)
}

func resolvedLinksFromProjection(vaultDef obsidian.VaultDefinition, cache *obsidian.NotePathCache, sourcePath string, base *noteformat.DocumentBaseFact, links []noteformat.UnresolvedAuthoredLinkFact) []ResolvedNoteLink {
	if cache == nil {
		return nil
	}
	source, err := paths.CleanNotePath(sourcePath)
	if err != nil {
		return nil
	}
	out, seen := make([]ResolvedNoteLink, 0, len(links)), make(map[string]struct{}, len(links))
	for _, link := range links {
		if !linkResolutionSupported(vaultDef, link.Resolution) {
			continue
		}
		targetPath, ok := resolveProjectedLink(cache, source.String(), base, link)
		if !ok || targetPath == source.String() {
			continue
		}
		target, err := paths.CleanNotePath(targetPath)
		if err != nil {
			continue
		}
		for _, kind := range resolvedLinkKindsFromProjection(link) {
			key := source.String() + "\x00" + link.Target + "\x00" + target.String() + "\x00" + kind
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ResolvedNoteLink{SourcePath: source, TargetInput: link.Target, TargetPath: target, Kind: kind})
		}
	}
	return out
}

func linkResolutionSupported(vaultDef obsidian.VaultDefinition, resolution noteformat.LinkResolution) bool {
	switch resolution {
	case noteformat.LinkResolutionNoteReference:
		return vaultDef.SupportsWikilinks()
	case noteformat.LinkResolutionRelativePath:
		return vaultDef.SupportsMarkdownLinks()
	case noteformat.LinkResolutionURI:
		return true
	default:
		return false
	}
}

func resolveProjectedLink(cache *obsidian.NotePathCache, sourcePath string, base *noteformat.DocumentBaseFact, link noteformat.UnresolvedAuthoredLinkFact) (string, bool) {
	return ResolveProjectedLink(cache, sourcePath, base, link)
}

func resolvedLinkKindsFromProjection(link noteformat.UnresolvedAuthoredLinkFact) []string {
	linkType := ""
	switch link.Resolution {
	case noteformat.LinkResolutionNoteReference:
		linkType = "wikilink"
	case noteformat.LinkResolutionRelativePath:
		linkType = "mdlink"
	case noteformat.LinkResolutionURI:
		subtype := strings.TrimSpace(string(link.Subtype))
		if subtype == "" {
			subtype = "basic"
		}
		return []string{"note_link:uri:" + subtype}
	}
	if linkType == "" {
		return nil
	}
	out := []string{linkType}
	if subtype := strings.TrimSpace(string(link.Subtype)); subtype != "" {
		out = append(out, "note_link:"+linkType+":"+subtype)
	}
	return out
}
