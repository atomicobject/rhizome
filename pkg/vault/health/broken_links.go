package health

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"runtime"

	"sync"
)

// FindBrokenLinks scans the vault and returns all wikilinks whose target
// cannot be fully resolved. The check covers two classes:
//
//   - the target note does not exist (`note_missing`)
//   - the target note exists but the trailing `^block-id` or heading-text
//     fragment does not resolve in that note (`block_missing` or
//     `heading_missing`)
//
// By default, excludes links to image files unless IncludeImages is true.
//
// WHY: SPEC-0023's orphan-block-id check needs fragment-level reference
// accounting so a typo'd inbound `[[note#^typo]]` keeps the real `^typo`
// anchor as a soft hold rather than orphaning it. Coderefs:
// [[linkable-embedded-node-identifiers#^spec-0023-us4]]
func FindBrokenLinks(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, options BrokenLinksOptions) ([]BrokenLink, error) {
	return FindBrokenLinksContext(context.Background(), vaultDef, note, options)
}

// FindBrokenLinksContext is the context-aware form of FindBrokenLinks.
func FindBrokenLinksContext(ctx context.Context, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, options BrokenLinksOptions) ([]BrokenLink, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allNotes, err := obsidian.NotesListContext(ctx, vaultDef, note)
	if err != nil {
		return nil, err
	}

	var aliasesByPath map[string][]string
	if provider, ok := note.(obsidian.NoteEntriesProvider); ok {
		if entries, snapErr := provider.NoteEntriesSnapshot(ctx); snapErr == nil {
			aliasesByPath = obsidian.AliasesFromNoteEntries(entries)
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	contentByPath, readErrByPath, loadErr := loadNoteContents(ctx, vaultDef, note, allNotes)
	if loadErr != nil {
		return nil, loadErr
	}
	getContent := func(p string) string { return contentByPath[p] }
	if aliasesByPath == nil {
		aliasesByPath = aliasesFromNotePaths(allNotes, getContent)
	}
	cache := obsidian.BuildNotePathCacheWithAliases(allNotes, aliasesByPath)
	fragmentByPath := make(map[string]fragmentIndex, len(allNotes))
	for _, notePath := range allNotes {
		fragmentByPath[notePath] = buildFragmentIndex(getContent(notePath))
	}
	resolver := &linkResolver{
		cache:    cache,
		files:    obsidian.LazyVaultFileIndex(vaultDef, allNotes),
		markdown: vaultDef.SupportsMarkdownLinks(),
		wikilink: vaultDef.SupportsWikilinks(),
		root:     vaultDef.BasePath(),
	}

	brokenByNote := make([][]BrokenLink, len(allNotes))
	readErrorsByNote := make([]NoteReadErrors, len(allNotes))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < brokenLinkWorkerCount(len(allNotes)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				var idx int
				select {
				case <-ctx.Done():
					return
				case jobIdx, ok := <-jobs:
					if !ok {
						return
					}
					idx = jobIdx
				}
				notePath := allNotes[idx]
				content := getContent(notePath)
				if readErr := readErrByPath[notePath]; readErr != nil {
					readErrorsByNote[idx] = NoteReadErrors{&NoteReadError{Source: notePath, Err: readErr}}
					continue
				}
				broken, readErrors := scanBrokenLinksInNote(notePath, content, resolver, fragmentByPath, readErrByPath, options)
				brokenByNote[idx] = broken
				readErrorsByNote[idx] = readErrors
			}
		}()
	}
sendJobs:
	for idx := range allNotes {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var brokenLinks []BrokenLink
	var readErrors NoteReadErrors
	for idx, links := range brokenByNote {
		brokenLinks = append(brokenLinks, links...)
		readErrors = append(readErrors, readErrorsByNote[idx]...)
	}
	if len(readErrors) > 0 {
		targetReadErrors := make(map[string]struct{})
		for _, readErr := range readErrors {
			if readErr.Target != "" {
				targetReadErrors[readErr.Target] = struct{}{}
			}
		}
		filteredReadErrors := make(NoteReadErrors, 0, len(readErrors))
		for _, readErr := range readErrors {
			if readErr.Target == "" {
				if _, linked := targetReadErrors[readErr.Source]; linked {
					continue
				}
			}
			filteredReadErrors = append(filteredReadErrors, readErr)
		}
		readErrors = filteredReadErrors
	}

	if len(readErrors) > 0 {
		return brokenLinks, readErrors
	}
	return brokenLinks, nil
}

func loadNoteContents(ctx context.Context, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, allNotes []string) (map[string]string, map[string]error, error) {
	contents := make([]string, len(allNotes))
	readErrors := make([]error, len(allNotes))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < brokenLinkWorkerCount(len(allNotes)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				var idx int
				select {
				case <-ctx.Done():
					return
				case jobIdx, ok := <-jobs:
					if !ok {
						return
					}
					idx = jobIdx
				}
				body, err := getContentsContext(ctx, vaultDef, note, allNotes[idx])
				if err != nil {
					readErrors[idx] = err
					continue
				}
				contents[idx] = body
			}
		}()
	}
sendJobs:
	for idx := range allNotes {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	contentByPath := make(map[string]string, len(allNotes))
	readErrByPath := make(map[string]error)
	for idx, notePath := range allNotes {
		contentByPath[notePath] = contents[idx]
		if readErrors[idx] != nil {
			readErrByPath[notePath] = readErrors[idx]
		}
	}
	return contentByPath, readErrByPath, nil
}

func getContentsContext(ctx context.Context, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, notePath string) (string, error) {
	if reader, ok := note.(obsidian.ContextNoteReader); ok {
		return reader.GetContentsContext(ctx, vaultDef, notePath)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	content, err := note.GetContents(vaultDef, notePath)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	return content, err
}

func brokenLinkWorkerCount(total int) int {
	if total <= 1 {
		return 1
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 4 {
		workers = 4
	}
	if workers > 16 {
		workers = 16
	}
	if workers > total {
		workers = total
	}
	return workers
}
