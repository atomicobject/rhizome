// Package operations provides shared application operations for rhizome commands.
//
// Docs: [CONTEXT.md](pkg/app/operations/CONTEXT.md)
//
// This package contains the business logic shared by CLI, MCP, and web callers. Each file
// typically exports one or more action functions that cmd/ calls after flag
// parsing. The thin cmd/ layer handles Cobra setup; real work happens here.
package operations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// InputType represents the type of input for listing files
type InputType int

const (
	InputTypeFile     InputType = iota // File path input
	InputTypeTag                       // Tag search input
	InputTypeFind                      // Fuzzy find input
	InputTypeProperty                  // Property key:value input
)

// ListInput represents a single input for listing files
type ListInput struct {
	Type     InputType // Type of the input
	Value    string    // Value of the input
	Property string    // Property name for InputTypeProperty
}

// ListParams represents parameters for listing files
type ListParams struct {
	DebugOutput                    io.Writer                       // Optional diagnostic destination owned by this invocation
	Inputs                         []ListInput                     // List of inputs to process
	MaxDepth                       int                             // Maximum depth for following links
	SkipAnchors                    bool                            // Whether to skip wikilinks with anchors (e.g. [[Note#Section]])
	SkipEmbeds                     bool                            // Whether to skip embedded wikilinks (e.g. ![[Embedded Note]])
	AbsolutePaths                  bool                            // Whether to return absolute paths
	SuppressedTags                 []string                        // Tags to exclude from results
	Expression                     *InputExpression                // Optional boolean expression to evaluate
	OnMatch                        func(string)                    // Callback function to report matches as they're found
	IncludeBacklinks               bool                            // Whether to collect first-degree backlinks for matched files
	Backlinks                      *map[string][]obsidian.Backlink // Optional output map for backlinks keyed by normalized target path
	PrimaryMatches                 *[]string                       // Optional output slice capturing the matches before link following
	SessionStore                   *semdb.Store                    // Optional: reuse existing metadata/session store
	MetadataStoreFallback          MetadataStoreFallbackPolicy     // Optional: keep planned one-shot reads live when no store was supplied
	CandidatePaths                 []string                        // Optional: restrict store-backed evaluation to this note set; nil means unscoped, empty means scoped to no notes
	markdownsyntax.WikilinkOptions                                 // options influencing backlink parsing
}

func (params ListParams) debugf(format string, args ...any) {
	if params.DebugOutput != nil {
		fmt.Fprintf(params.DebugOutput, format, args...)
	}
}

// hasAnySuppressedTags checks if a file contains any of the suppressed tags
func hasAnySuppressedTags(tags []string, suppressedTags []string) bool {
	if len(suppressedTags) == 0 {
		return false
	}
	for _, suppressed := range suppressedTags {
		for _, tag := range tags {
			if tagMatchesParent(tag, suppressed) {
				return true
			}
		}
	}
	return false
}

func tagMatchesParent(tag, parent string) bool {
	tag = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(tag), "#"))
	parent = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(parent), "#"))
	return parent != "" && (tag == parent || strings.HasPrefix(tag, parent+"/"))
}

// FilterSuppressedFiles removes files with suppressed provider-projected tags.
func FilterSuppressedFiles(files []string, note obsidian.NoteReader, suppressedTags []string, debugOutput io.Writer) []string {
	if len(suppressedTags) == 0 {
		return files
	}

	var filtered []string
	facts := NoteFactsFromReader(note)
	for _, file := range files {
		fact, ok := facts.LookupFact(file)
		if !ok || !hasAnySuppressedTags(fact.Tags, suppressedTags) {
			filtered = append(filtered, file)
		} else if debugOutput != nil {
			fmt.Fprintf(debugOutput, "Suppressing file %s due to suppressed tags\n", file)
		}
	}

	return filtered
}

// ListFiles is the main function that lists files based on the provided parameters
func ListFiles(vault repoconfig.VaultManager, note obsidian.NoteReader, params ListParams) ([]string, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}

	vaultPath := vaultDef.BasePath()
	if vaultPath == "" {
		return nil, errors.New(obsidian.RhizomeVaultPathInvalidError)
	}

	var (
		allNotes     []string
		allNotesErr  error
		allNotesOnce sync.Once
	)
	loadAllNotes := func() ([]string, error) {
		allNotesOnce.Do(func() {
			allNotes, allNotesErr = note.GetNotesList(vaultDef)
		})
		return allNotes, allNotesErr
	}

	// Process all inputs to get matching files
	expr := params.Expression
	if expr == nil {
		expr = buildOrExpression(params.Inputs)
	}
	if expr == nil {
		if _, err := loadAllNotes(); err != nil {
			return nil, err
		}
	}

	var matches []string
	if expr != nil {
		store := params.SessionStore
		var cleanup func()
		if store == nil && params.MetadataStoreFallback.allowsStoreOpen() {
			store, cleanup, err = openMetadataStore(vaultDef)
			if cleanup != nil {
				defer cleanup()
			}
		}
		// Explicit file inputs are the caller's assertion about the filesystem;
		// the store's path universe can lag a rename or creation the watcher
		// has not published yet (a live runtime mid-batch, a preempted job), and
		// the index adds nothing to a plain path match. Only expressions that
		// need indexed rows or titles consult the store.
		if err == nil && !expressionUsesOnlyFileInputs(expr) && metadataStoreReady(context.Background(), vaultDef, note, store) {
			if metadataMatches, err := evaluateExpressionMatchesWithStore(context.Background(), store, expr, params.CandidatePaths); err == nil {
				matches = metadataMatches
			}
		}
		if matches == nil {
			allNotes, err := loadAllNotes()
			if err != nil {
				return nil, err
			}
			matches = evaluateExpressionMatches(allNotes, note, expr)
		}
	}
	params.debugf("Found %d initial matching files\n", len(matches))

	// Filter out suppressed files
	matches = FilterSuppressedFiles(matches, note, params.SuppressedTags, params.DebugOutput)
	params.debugf("After suppression filtering: %d files\n", len(matches))

	if params.PrimaryMatches != nil {
		copied := make([]string, len(matches))
		copy(copied, matches)
		*params.PrimaryMatches = copied
	}

	// If following links, get all connected files
	if params.MaxDepth > 0 {
		linkedFiles := followMatchedFiles(matches, vaultDef, note, params)
		params.debugf("Found %d total files after following links\n", len(linkedFiles))

		// Apply suppression filter to linked files as well
		linkedFiles = FilterSuppressedFiles(linkedFiles, note, params.SuppressedTags, params.DebugOutput)
		params.debugf("After suppression filtering linked files: %d files\n", len(linkedFiles))

		if err := loadBacklinks(vaultDef, note, matches, params); err != nil {
			return nil, err
		}

		// Call OnMatch for each linked file
		params.debugf("Notifying OnMatch with %d files\n", len(linkedFiles))
		notifyMatches(linkedFiles, params.OnMatch)
		return linkedFiles, nil
	}

	if err := loadBacklinks(vaultDef, note, matches, params); err != nil {
		return nil, err
	}

	// Call OnMatch for each matched file - this only happens when not following links
	params.debugf("Notifying OnMatch with %d files (no link following)\n", len(matches))
	notifyMatches(matches, params.OnMatch)
	return matches, nil
}

func loadBacklinks(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, matches []string, params ListParams) error {
	if !params.IncludeBacklinks || params.Backlinks == nil {
		return nil
	}

	// Backlink fragments are not persisted in graph_doc_edges. Always derive
	// backlinks from the live source snapshot so heading/block identity is not
	// silently collapsed when a session store is available.
	backlinks, err := vaultgraph.CollectBacklinks(vaultDef, note, matches, params.WikilinkOptions, params.SuppressedTags)
	if err != nil {
		return err
	}
	*params.Backlinks = backlinks
	return nil
}

// notifyMatches calls the OnMatch callback for each file if a callback is provided
func notifyMatches(files []string, onMatch func(string)) {
	if onMatch == nil {
		return
	}
	for _, file := range files {
		onMatch(file)
	}
}

// followMatchedFiles follows wikilinks for matched files
func followMatchedFiles(matches []string, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, params ListParams) []string {
	// Get all notes first
	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		params.debugf("Error getting notes list: %v\n", err)
		return matches
	}

	params.debugf("Found %d total notes in vault\n", len(allNotes))

	// Build the note path cache, including frontmatter aliases when
	// the reader exposes them so wikilinks like [[SPEC-001]] resolve.
	var aliasesByPath map[string][]string
	if provider, ok := note.(obsidian.NoteEntriesProvider); ok {
		if entries, snapErr := provider.NoteEntriesSnapshot(context.Background()); snapErr == nil {
			aliasesByPath = obsidian.AliasesFromNoteEntries(entries)
		}
	}
	cache := obsidian.BuildNotePathCacheWithAliases(allNotes, aliasesByPath)
	params.debugf("Built cache with %d entries\n", len(cache.Paths))

	visited := make(map[string]bool)
	var result []string

	// Create wikilinks options from parameters
	options := obsidian.CreateWikilinksOptions(params.MaxDepth, params.SkipAnchors, params.SkipEmbeds)

	for _, notePath := range matches {
		params.debugf("Following links for note: %s\n", notePath)
		files, err := obsidian.FollowWikilinks(vaultDef, note, notePath, visited, cache, options)
		if err != nil {
			params.debugf("Error following links for %s: %v\n", notePath, err)
			continue
		}
		params.debugf("Found %d linked files for %s\n", len(files), notePath)
		result = append(result, files...)
	}

	seen := make(map[string]struct{}, len(result))
	unique := result[:0]
	for _, path := range result {
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			unique = append(unique, path)
		}
	}
	return unique
}

// expressionUsesOnlyFileInputs reports whether every leaf is an explicit file
// input, so the expression is decidable from the filesystem alone.
func expressionUsesOnlyFileInputs(expr *InputExpression) bool {
	if expr == nil {
		return false
	}
	if expr.Type == exprLeaf {
		return expr.Input != nil && expr.Input.Type == InputTypeFile
	}
	left := expr.Left == nil || expressionUsesOnlyFileInputs(expr.Left)
	right := expr.Right == nil || expressionUsesOnlyFileInputs(expr.Right)
	return left && right
}

func buildOrExpression(inputs []ListInput) *InputExpression {
	if len(inputs) == 0 {
		return nil
	}
	var expr *InputExpression
	for i := len(inputs) - 1; i >= 0; i-- {
		current := &InputExpression{
			Type:  exprLeaf,
			Input: &inputs[i],
		}
		if expr == nil {
			expr = current
			continue
		}
		expr = &InputExpression{
			Type:  exprOr,
			Left:  current,
			Right: expr,
		}
	}
	return expr
}

func determineWorkerCount(noteCount int) int {
	numWorkers := runtime.NumCPU()
	if noteCount < numWorkers {
		numWorkers = noteCount
	}
	return numWorkers
}
