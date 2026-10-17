package graph

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// CollectBacklinks finds first-degree backlinks for the provided targets.
// Each target key is present in the returned map, even if it has no backlinks.
// Options allow skipping anchors or embeds; suppressedTags removes referrers that contain those tags.
// This function also scans markdown links if the vault definition supports them.

func CollectBacklinks(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, targets []string, options markdownsyntax.WikilinkOptions, suppressedTags []string) (map[string][]obsidian.Backlink, error) {
	snapshot, err := BuildGraphSnapshot(vaultDef, note, GraphAnalysisOptions{WikilinkOptions: options})
	if err != nil {
		return nil, err
	}
	return CollectBacklinksFromGraph(snapshot, targets, suppressedTags), nil
}

func containsSuppressedTags(tags []string, suppressedTags []string) bool {
	if len(tags) == 0 || len(suppressedTags) == 0 {
		return false
	}
	tagSet := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		norm := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tag, "#")))
		if norm == "" {
			continue
		}
		tagSet[norm] = struct{}{}
	}
	for _, suppressed := range suppressedTags {
		norm := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(suppressed, "#")))
		if norm == "" {
			continue
		}
		if _, ok := tagSet[norm]; ok {
			return true
		}
	}
	return false
}
