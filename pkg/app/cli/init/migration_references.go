package init

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// referencedLegacyProcessDocs returns legacy process documents targeted by
// authored links outside the legacy process directory. Such links are adoption
// evidence: deleting the target would break the repository's graph even when
// the target bytes still match a shipped template.
func referencedLegacyProcessDocs(projectRoot string, catalog legacyProcessFingerprintCatalog) (map[string]bool, error) {
	var notes []string
	err := filepath.WalkDir(projectRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".rhizome", "node_modules", "vendor":
				if path != projectRoot {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return err
		}
		notes = append(notes, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan legacy process references: %w", err)
	}

	cache := obsidian.BuildNotePathCache(notes)
	legacyPrefix := legacyProcessDocsRoot + "/"
	legacyContent := make(map[string][]byte)
	linksBySource := make(map[string][]string)
	for _, source := range notes {
		content, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(source)))
		if err != nil {
			return nil, fmt.Errorf("read link source %s: %w", source, err)
		}
		if strings.HasPrefix(source, legacyPrefix) {
			legacyContent[source] = content
		}
		for _, link := range markdownsyntax.ScanStructuredLinks(string(content)) {
			var target string
			var ok bool
			switch link.Kind {
			case markdownsyntax.StructuredLinkWikilink:
				if strings.HasPrefix(link.Path, "./") || strings.HasPrefix(link.Path, "../") {
					target, ok = cache.ResolveMdLink(link.Target, source)
				} else {
					target, ok = cache.ResolveNote(link.Target)
				}
			case markdownsyntax.StructuredLinkMarkdown:
				target, ok = cache.ResolveMdLink(link.Target, source)
			}
			if ok {
				target = canonicalCachedNotePath(cache, target)
			}
			if ok && strings.HasPrefix(target, legacyPrefix) {
				linksBySource[source] = append(linksBySource[source], target)
			}
		}
		for _, definition := range markdownsyntax.ExtractMarkdownReferenceDefinitions(string(content)) {
			target, ok := cache.ResolveMdLink(definition, source)
			if ok {
				target = canonicalCachedNotePath(cache, target)
			}
			if ok && strings.HasPrefix(target, legacyPrefix) {
				linksBySource[source] = append(linksBySource[source], target)
			}
		}
	}

	// Seed reachability with every external reference and every legacy document
	// that will be retained for local reconciliation. Following legacy-to-legacy
	// edges transitively prevents a retained document from gaining broken links
	// when its exact shipped dependencies would otherwise be deleted.
	referenced := make(map[string]bool)
	queued := make(map[string]bool)
	queue := make([]string, 0)
	enqueue := func(path string) {
		if queued[path] {
			return
		}
		queued[path] = true
		queue = append(queue, path)
	}
	for source, targets := range linksBySource {
		if strings.HasPrefix(source, legacyPrefix) {
			continue
		}
		for _, target := range targets {
			referenced[target] = true
			enqueue(target)
		}
	}
	for path, content := range legacyContent {
		record := classifyLegacyProcessDoc(path, content, catalog)
		if record.Classification != legacyProcessDocExact || strings.Contains(string(content), legacyProcessRetirementSentinel) {
			enqueue(path)
		}
	}
	for len(queue) > 0 {
		source := queue[0]
		queue = queue[1:]
		for _, target := range linksBySource[source] {
			referenced[target] = true
			enqueue(target)
		}
	}
	return referenced, nil
}

func canonicalCachedNotePath(cache *obsidian.NotePathCache, target string) string {
	target = filepath.ToSlash(target)
	if _, exists := cache.NotePaths[target]; !exists && !strings.HasSuffix(strings.ToLower(target), ".md") {
		if _, exists := cache.NotePaths[target+".md"]; exists {
			target += ".md"
		}
	}
	return target
}
