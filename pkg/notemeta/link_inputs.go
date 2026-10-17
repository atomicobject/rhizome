package notemeta

import (
	"path"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Basename keys deliberately overapproximate source-relative and URI lookup.
// A namespace change may reproject an extra same-name source, but cannot omit
// a formerly unresolved or ambiguous reference.
func linkInputKeys(input string) []string {
	input = paths.LinkKey(strings.TrimSpace(strings.ReplaceAll(input, "\\", "/")))
	if input == "" {
		return nil
	}
	base := path.Base(input)
	keys := []string{base}
	if ext := path.Ext(base); ext != "" {
		keys = append(keys, strings.TrimSuffix(base, ext))
	}
	return dedupeStrings(keys)
}

func projectionLinkInputRows(vault obsidian.VaultDefinition, entry projectedNoteEntry) []semdb.NoteLinkInputRow {
	keys := map[string]struct{}{}
	for _, link := range entry.Links {
		if !resolutionSupported(vault, link.Resolution) {
			continue
		}
		for _, key := range linkInputKeys(linkLookupPath(entry.Source.Path().String(), entry.Projection.Facts.DocumentBase, link)) {
			keys[key] = struct{}{}
		}
	}
	rows := make([]semdb.NoteLinkInputRow, 0, len(keys))
	for _, key := range sortedKeys(keys) {
		rows = append(rows, semdb.NoteLinkInputRow{NotePath: entry.Source.Path().String(), LookupKey: key})
	}
	return rows
}

func topologyLookupKeys(pathsList []string, oldAliases, newAliases map[string][]string) []string {
	keys := map[string]struct{}{}
	for _, notePath := range pathsList {
		for _, key := range linkInputKeys(notePath) {
			keys[key] = struct{}{}
		}
		for _, aliases := range [][]string{oldAliases[notePath], newAliases[notePath]} {
			for _, alias := range aliases {
				for _, key := range linkInputKeys(alias) {
					keys[key] = struct{}{}
				}
			}
		}
	}
	return sortedKeys(keys)
}
