package notemeta

import (
	"context"
	"path"
	"slices"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// projectionLinkTopologyChanged determines whether a bounded delta must
// rederive all links. It intentionally consumes aliases emitted by the
// selected provider rather than compatibility frontmatter or authored bytes.
func projectionLinkTopologyChanged(ctx context.Context, store Store, vaultDef obsidian.VaultDefinition, durable map[string]semdb.NoteMetadataRow, entries []projectedNoteEntry, deletedPaths []string) (bool, map[string][]string, error) {
	if !vaultDef.SupportsWikilinks() && !vaultDef.SupportsMarkdownLinks() && !projectedEntriesHaveURI(entries) {
		return false, nil, nil
	}
	if len(deletedPaths) > 0 {
		return true, nil, nil
	}
	pathsList := projectedPaths(entries)
	if len(pathsList) == 0 {
		return false, nil, nil
	}
	if len(durable) != len(pathsList) {
		return true, nil, nil
	}
	storedAliases, err := store.CurrentNoteAliases(ctx)
	if err != nil {
		return false, nil, err
	}
	for _, entry := range entries {
		notePath := entry.Source.Path().String()
		if !equalStringSets(projectionAliasValues(entry), storedAliases[notePath]) {
			return true, nil, nil
		}
	}
	return false, storedAliases, nil
}

// buildIncrementalProjectionLinkCache keeps ordinary link lookup bounded to
// targets named by the changed projection, while URI links use the complete
// candidate set needed to preserve ambiguity. It then overlays changed
// sources and never rescans authored Markdown syntax.
// aliasesByPath is the snapshot already loaded for topology validation; using
// it here also keeps provider aliases available when SQL path lookup cannot
// resolve an alias key on its own.
func buildIncrementalProjectionLinkCache(ctx context.Context, store Store, vaultDef obsidian.VaultDefinition, entries []projectedNoteEntry, deletedPaths []string, aliasesByPath map[string][]string) (*obsidian.NotePathCache, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	lookupInputs := make(map[string]struct{})
	uriEntries := make([]projectedNoteEntry, 0)
	for _, entry := range entries {
		for _, link := range entry.Links {
			if !resolutionSupported(vaultDef, link.Resolution) {
				continue
			}
			if link.Resolution == noteformat.LinkResolutionURI {
				uriEntries = append(uriEntries, entry)
				break
			}
			if input := linkLookupPath(entry.Source.Path().String(), entry.Projection.Facts.DocumentBase, link); input != "" {
				lookupInputs[input] = struct{}{}
			}
		}
	}
	if len(lookupInputs) == 0 && len(uriEntries) == 0 {
		return nil, nil
	}
	resolvedPaths := make([]string, 0)
	var uriCandidatePaths []string
	if len(lookupInputs) > 0 {
		resolved, err := store.ResolveStoredNoteLinks(ctx, sortedKeys(lookupInputs))
		if err != nil {
			return nil, err
		}
		resolvedPaths = make([]string, 0, len(resolved))
		for _, notePath := range resolved {
			if notePath = strings.TrimSpace(notePath); notePath != "" {
				resolvedPaths = append(resolvedPaths, notePath)
			}
		}
	}
	if len(uriEntries) > 0 {
		// URI links use the same provider-neutral resolver as full snapshots and
		// public reads. Build one complete candidate union so extensionless and
		// alias targets cannot silently inherit Markdown suffix preference.
		allPaths, err := store.CurrentNoteMetadataPaths(ctx)
		if err != nil {
			return nil, err
		}
		uriCandidatePaths = allPaths
		cache := obsidian.BuildNotePathCacheWithAliases(allPaths, aliasesByPath)
		for _, entry := range uriEntries {
			for _, link := range entry.Links {
				if link.Resolution != noteformat.LinkResolutionURI {
					continue
				}
				if target, ok := ResolveURIProjectionLink(cache, entry.Source.Path().String(), entry.Projection.Facts.DocumentBase, link); ok {
					resolvedPaths = append(resolvedPaths, target)
				}
			}
		}
	}
	if len(resolvedPaths) == 0 && len(uriCandidatePaths) == 0 {
		if len(aliasesByPath) == 0 {
			return nil, nil
		}
		cache := buildIncrementalPathCache(nil, projectedPaths(entries), deletedPaths)
		return buildProjectionAliasCache(cache, aliasesByPath), nil
	}
	// Keep the complete URI candidate set in the final cache. Resolving URI
	// links against the complete set above is only half of the contract: the
	// same cache resolves every changed entry's links below. Rebuilding it
	// from resolved targets alone would drop an ambiguous claimant such as
	// report.md when another changed source explicitly links report.html.
	cachePaths := append(uriCandidatePaths, resolvedPaths...)
	cache := buildIncrementalPathCache(cachePaths, projectedPaths(entries), deletedPaths)
	if len(aliasesByPath) > 0 {
		cache = buildProjectionAliasCache(cache, aliasesByPath)
	}
	return cache, nil
}

// linkLookupPath adapts only the closed resolution semantics into the store's
// candidate lookup vocabulary. Path is provider-decoded, so authored
// ResolverInput remains opaque and is reserved for final shared-cache
// resolution (for example, Markdown angle destinations with titles).
func linkLookupPath(sourcePath string, base *noteformat.DocumentBaseFact, link noteformat.UnresolvedAuthoredLinkFact) string {
	targetPath := strings.TrimSpace(link.Path)
	if targetPath == "" {
		return ""
	}
	switch link.Resolution {
	case noteformat.LinkResolutionNoteReference:
		return targetPath
	case noteformat.LinkResolutionRelativePath:
		if strings.HasPrefix(targetPath, "/") {
			targetPath = strings.TrimPrefix(targetPath, "/")
		} else {
			dir := path.Dir(sourcePath)
			if dir != "." && dir != "/" {
				targetPath = path.Join(dir, targetPath)
			}
		}
		targetPath = path.Clean(targetPath)
		if targetPath == "." || strings.HasPrefix(targetPath, "../") {
			return ""
		}
		return targetPath
	case noteformat.LinkResolutionURI:
		if link.URI == nil || link.URI.Encoding != noteformat.URIEncodingPercent || link.URI.Scheme != "" || link.URI.Authority != "" || link.URI.ProtocolRelative {
			return ""
		}
		if strings.HasSuffix(link.URI.Path, "/") {
			return ""
		}
		basePath := sourcePath
		if base != nil {
			if base.URI.Scheme != "" || base.URI.Authority != "" || base.URI.ProtocolRelative {
				return ""
			}
			if base.URI.Path != "" {
				basePath = uriVaultPath(sourcePath, base.URI.Path)
				if strings.HasSuffix(base.URI.Path, "/") {
					basePath += "/"
				}
			}
		}
		targetPath := uriVaultPath(basePath, link.URI.Path)
		if !isVaultRelativeURIPath(targetPath) {
			return ""
		}
		return targetPath
	default:
		return ""
	}
}

func projectedEntriesHaveURI(entries []projectedNoteEntry) bool {
	for _, entry := range entries {
		for _, link := range entry.Links {
			if link.Resolution == noteformat.LinkResolutionURI {
				return true
			}
		}
	}
	return false
}

// overlayProjectionAliasesForDelta replaces changed claims and removes deleted paths.
func overlayProjectionAliasesForDelta(stored map[string][]string, entries []projectedNoteEntry, deletedPaths []string) map[string][]string {
	out := make(map[string][]string, len(stored)+len(entries))
	for notePath, aliases := range stored {
		if values := dedupeStrings(aliases); len(values) > 0 {
			out[notePath] = values
		}
	}
	for _, notePath := range deletedPaths {
		delete(out, strings.TrimSpace(notePath))
	}
	for _, entry := range entries {
		notePath := entry.Source.Path().String()
		if aliases := projectionAliasValues(entry); len(aliases) > 0 {
			out[notePath] = aliases
		} else {
			delete(out, notePath)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildProjectionAliasCache bulk-builds aliases over the complete candidate
// and alias-owner union. Alias owners must remain note candidates because the
// incremental mutation path previously added them through AddOrUpdate.
func buildProjectionAliasCache(cache *obsidian.NotePathCache, aliasesByPath map[string][]string) *obsidian.NotePathCache {
	if cache == nil {
		return nil
	}
	paths := make([]string, 0, len(cache.NotePaths)+len(aliasesByPath))
	seen := make(map[string]struct{}, len(cache.NotePaths)+len(aliasesByPath))
	for notePath := range cache.NotePaths {
		seen[notePath] = struct{}{}
		paths = append(paths, notePath)
	}
	for notePath := range aliasesByPath {
		if _, ok := seen[notePath]; ok {
			continue
		}
		seen[notePath] = struct{}{}
		paths = append(paths, notePath)
	}
	return obsidian.BuildNotePathCacheWithAliases(paths, aliasesByPath)
}

func projectionAliasValues(entry projectedNoteEntry) []string {
	return dedupeStrings(aliasValuesFromFacts(entry.Aliases))
}

func equalStringSets(left, right []string) bool {
	left, right = dedupeStrings(left), dedupeStrings(right)
	sort.Strings(left)
	sort.Strings(right)
	return slices.Equal(left, right)
}

func incrementalProjectedNotesHash(ctx context.Context, store metadataRowsProvider, currentNotesHash string, entries []projectedNoteEntry, deletedPaths []string) (string, error) {
	return incrementalNotesHash(ctx, store, currentNotesHash, entriesFromProjected(entries), deletedPaths)
}
