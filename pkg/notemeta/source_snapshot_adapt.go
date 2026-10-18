package notemeta

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ProjectNoteSource snapshots one already-captured authored source through the
// selected provider. It performs no filesystem reads or link resolution.
func (i Indexer) ProjectNoteSource(path, content string, mtime int64) (NoteSourceSnapshot, error) {
	entry, err := i.projectEntry(path, content, mtime)
	if err != nil {
		return NoteSourceSnapshot{}, err
	}
	if entry.Projection.Status != noteformat.ProjectionStatusCurrent {
		return NoteSourceSnapshot{}, ErrProjectionNotCurrent
	}
	return sourceFactsSnapshotFromProjectedEntry(entry), nil
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

func sourceFactsSnapshotFromProjectedEntry(entry projectedNoteEntry) NoteSourceSnapshot {
	snapshot := NoteSourceSnapshot{
		Path: entry.Source.Path(), Format: entry.Source.Format(), Projection: entry.Projection.Copy(),
		Content: string(entry.Source.Bytes()), RawSource: entry.Source.Bytes(), ContentHash: entry.Source.ContentHash(), Mtime: entry.Source.Mtime(), Size: entry.Source.Size(), Title: entry.Entry.Title,
		Frontmatter: cloneMap(entry.Entry.Frontmatter), InlineProps: cloneInline(entry.Entry.InlineProps), Tags: dedupeStrings(entry.Entry.Tags), Aliases: aliasValuesFromFacts(entry.Aliases),
		FragmentTargets: append([]noteformat.FragmentTargetFact(nil), entry.Projection.Facts.FragmentTargets...), SearchRegions: append([]noteformat.SearchRegionFact(nil), entry.Projection.Facts.SearchRegions...), Diagnostics: append([]noteformat.Diagnostic(nil), entry.Projection.Diagnostics...),
	}
	if entry.Projection.Facts.DocumentBase != nil {
		base := *entry.Projection.Facts.DocumentBase
		snapshot.DocumentBase = &base
	}
	return snapshot
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
		if !resolutionSupported(vaultDef, link.Resolution) {
			continue
		}
		targetPath, ok := ResolveProjectedLink(cache, source.String(), base, link)
		if !ok || targetPath == source.String() {
			continue
		}
		target, err := paths.CleanNotePath(targetPath)
		if err != nil {
			continue
		}
		for _, kind := range projectionEdgeKinds(link) {
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
