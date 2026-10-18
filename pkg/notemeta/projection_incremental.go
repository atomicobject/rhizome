package notemeta

import (
	"context"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// projectionLinkTopologyChanged identifies namespace changes that require
// rederiving dependent sources. It intentionally consumes aliases emitted by the
// selected provider rather than compatibility frontmatter or authored bytes.
func projectionLinkTopologyChanged(ctx context.Context, store Store, vaultDef obsidian.VaultDefinition, durable map[string]semdb.NoteMetadataRow, entries []projectedNoteEntry, deletedPaths []string) (bool, map[string][]string, error) {
	storedAliases, err := store.CurrentNoteAliases(ctx)
	if err != nil {
		return false, nil, err
	}
	if len(deletedPaths) > 0 || len(durable) != len(projectedPaths(entries)) {
		return true, storedAliases, nil
	}
	for _, entry := range entries {
		notePath := entry.Source.Path().String()
		if !equalStringSets(projectionAliasValues(entry), storedAliases[notePath]) {
			return true, storedAliases, nil
		}
	}
	return false, storedAliases, nil
}

// buildIncrementalProjectionLinkCache uses the persisted candidate inventory
// and provider aliases without reading or projecting unchanged authored notes.
func buildIncrementalProjectionLinkCache(ctx context.Context, store Store, vaultDef obsidian.VaultDefinition, entries []projectedNoteEntry, deletedPaths []string, aliasesByPath map[string][]string) (*obsidian.NotePathCache, error) {
	paths, err := store.CurrentNoteMetadataPaths(ctx)
	if err != nil {
		return nil, err
	}
	selected := selectedPathsAfterDelta(paths, entries, deletedPaths)
	aliases := overlayProjectionAliasesForDelta(aliasesByPath, entries, deletedPaths)
	return obsidian.BuildNotePathCacheWithAliases(selected, aliases), nil
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
		if targetPath == "." || targetPath == ".." || strings.HasPrefix(targetPath, "../") {
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
	aggregate, err := decodeNotesHash(currentNotesHash)
	if err != nil {
		return "", err
	}
	touched := append(projectedPaths(entries), deletedPaths...)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, dedupeStrings(touched))
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		aggregate.Remove(row.Path, row.ContentHash)
	}
	for _, entry := range entries {
		aggregate.Add(entry.Source.Path().String(), entry.Source.ContentHash())
	}
	return aggregate.String(), nil
}
