package operations

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const indexedFileContextCoderefLimit = 100
const indexedFileContextLinkTargetLimit = 25

// indexedCoderefsForRequestedFile uses the durable code-to-note edges as an
// exact target whitelist, then rescans only the explicitly requested source
// file. The rescan restores occurrence details that are intentionally absent
// from the compact index row without reopening the vault-wide note inventory.
func indexedCoderefsForRequestedFile(ctx context.Context, store *semdb.Store, codePath, absPath string) ([]coderefs.CodeRef, *obsidian.NotePathCache, bool, error) {
	if store == nil || strings.TrimSpace(codePath) == "" || strings.TrimSpace(absPath) == "" {
		return nil, nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	links, err := store.IndexedCodeNoteLinksForFile(ctx, codePath, indexedFileContextCoderefLimit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, false, ctxErr
		}
		return nil, nil, false, nil
	}
	truncated := len(links) >= indexedFileContextCoderefLimit
	if len(links) == 0 {
		return nil, nil, truncated, nil
	}

	notePaths := make([]string, 0, len(links))
	allowed := make(map[string]struct{}, len(links))
	for _, link := range links {
		notePath := string(paths.EnsureNoteExtension(link.NotePath, ".md"))
		if notePath == "" {
			continue
		}
		if _, exists := allowed[notePath]; exists {
			continue
		}
		allowed[notePath] = struct{}{}
		notePaths = append(notePaths, notePath)
	}
	if len(notePaths) == 0 {
		return nil, nil, truncated, nil
	}

	aliasesByPath := make(map[string][]string)
	if rows, aliasErr := store.CurrentNotePropertyValues(ctx, notePaths, []string{"aliases"}, semdb.NotePropertySourceFrontmatter); aliasErr == nil {
		for _, row := range rows {
			alias := strings.TrimSpace(row.ValueText)
			if alias != "" {
				aliasesByPath[row.NotePath] = append(aliasesByPath[row.NotePath], alias)
			}
		}
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, truncated, ctxErr
	}
	scopedCache := obsidian.BuildNotePathCacheWithAliases(notePaths, aliasesByPath)
	if err := ctx.Err(); err != nil {
		return nil, scopedCache, truncated, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, scopedCache, truncated, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, scopedCache, truncated, err
	}
	refs, err := coderefs.ScanFile(codePath, content, scopedCache)
	if err != nil {
		return nil, scopedCache, truncated, nil
	}
	filtered := refs[:0]
	for _, ref := range refs {
		if _, ok := allowed[string(paths.EnsureNoteExtension(ref.Target, ".md"))]; ok {
			filtered = append(filtered, ref)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, scopedCache, truncated, err
	}
	return filtered, scopedCache, truncated, nil
}

// extendIndexedNotePathCache resolves only wikilinks extracted from the small
// set of documents selected for this request. It keeps ancestor and hub/MOC
// expansion behavior without reopening the vault-wide note inventory.
func extendIndexedNotePathCache(ctx context.Context, store *semdb.Store, cache *obsidian.NotePathCache, contents []string) (*obsidian.NotePathCache, bool, error) {
	if store == nil {
		return cache, false, nil
	}
	if err := ctx.Err(); err != nil {
		return cache, false, err
	}
	targets, truncated := indexedWikilinkTargets(contents, indexedFileContextLinkTargetLimit)
	if len(targets) == 0 {
		return cache, truncated, nil
	}

	pathSet := make(map[string]struct{})
	aliasesByPath := make(map[string][]string)
	if cache != nil {
		for notePath := range cache.NotePaths {
			pathSet[notePath] = struct{}{}
		}
		for _, notePath := range cache.Paths {
			pathSet[notePath] = struct{}{}
		}

	}

	directPaths, err := store.StoredNoteLinkCandidatePaths(ctx, targets)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return cache, truncated, ctxErr
		}
		return cache, truncated, nil
	}
	for _, notePath := range directPaths {
		pathSet[notePath] = struct{}{}
	}
	directCache := obsidian.BuildNotePathCache(directPaths)

	// Path/title matches outrank aliases in NotePathCache. Add every targeted
	// alias claimant so ambiguous aliases remain unresolved.
	for _, target := range targets {
		if _, ok := directCache.ResolveNote(target); ok {
			continue
		}
		notePaths, aliasErr := store.CurrentNotePathsByPropertyValue(
			ctx,
			"aliases",
			indexedAliasLookupValue(target),
			semdb.NotePropertySourceFrontmatter,
		)
		if aliasErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return cache, truncated, ctxErr
			}
			continue
		}
		for _, notePath := range notePaths {
			pathSet[notePath] = struct{}{}
		}
	}

	notePaths := make([]string, 0, len(pathSet))
	for notePath := range pathSet {
		notePaths = append(notePaths, notePath)
	}
	sort.Strings(notePaths)
	if rows, aliasErr := store.CurrentNotePropertyValues(ctx, notePaths, []string{"aliases"}, semdb.NotePropertySourceFrontmatter); aliasErr == nil {
		for _, row := range rows {
			if alias := strings.TrimSpace(row.ValueText); alias != "" {
				aliasesByPath[row.NotePath] = append(aliasesByPath[row.NotePath], alias)
			}
		}
	} else {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return cache, truncated, ctxErr
		}
		return cache, truncated, nil
	}
	return obsidian.BuildNotePathCacheWithAliases(notePaths, aliasesByPath), truncated, nil
}

func indexedAliasLookupValue(target string) string {
	target = strings.TrimSpace(target)
	if ext := filepath.Ext(target); strings.EqualFold(ext, ".md") {
		target = strings.TrimSuffix(target, ext)
	}
	return paths.LinkKey(strings.TrimSpace(target))
}

func indexedWikilinkTargets(contents []string, limit int) ([]string, bool) {
	if limit <= 0 {
		return nil, false
	}
	seen := make(map[string]struct{}, limit)
	targets := make([]string, 0, min(limit, len(contents)))
	for _, content := range contents {
		for _, match := range wikilinkPattern.FindAllStringSubmatch(content, -1) {
			if len(match) < 2 {
				continue
			}
			target := strings.TrimSpace(match[1])
			if pipe := strings.Index(target, "|"); pipe >= 0 {
				target = target[:pipe]
			}
			if hash := strings.Index(target, "#"); hash >= 0 {
				target = target[:hash]
			}
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}
			if len(targets) >= limit {
				return targets, true
			}
			targets = append(targets, target)
		}
	}
	return targets, false
}

func indexedExpansionSourceContents(linked []LinkedNoteContext, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, matchExpr []string) []string {
	if len(linked) == 0 || noteMgr == nil {
		return nil
	}
	_, expr, err := ParseInputsWithExpression(matchExpr)
	if err != nil || expr == nil {
		return nil
	}
	contents := make([]string, 0, min(len(linked), indexedFileContextLinkTargetLimit))
	for _, src := range linked {
		if src.Path == "" || !linkedNoteMatches(src, expr) {
			continue
		}
		content, readErr := noteMgr.GetContents(vaultDef, src.Path)
		if readErr != nil || content == "" {
			continue
		}
		contents = append(contents, content)
		if len(contents) >= indexedFileContextLinkTargetLimit {
			break
		}
	}
	return contents
}
