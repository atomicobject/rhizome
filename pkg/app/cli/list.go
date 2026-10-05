// Package actions provides CLI action implementations for rhizome commands.
//
// Docs: [CONTEXT.md](pkg/app/cli/CONTEXT.md)
//
// This package contains the business logic for user-facing commands. Each file
// typically exports one or more action functions that cmd/ calls after flag
// parsing. The thin cmd/ layer handles Cobra setup; real work happens here.
package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
	Inputs                   []ListInput                     // List of inputs to process
	MaxDepth                 int                             // Maximum depth for following links
	SkipAnchors              bool                            // Whether to skip wikilinks with anchors (e.g. [[Note#Section]])
	SkipEmbeds               bool                            // Whether to skip embedded wikilinks (e.g. ![[Embedded Note]])
	AbsolutePaths            bool                            // Whether to return absolute paths
	SuppressedTags           []string                        // Tags to exclude from results
	Expression               *InputExpression                // Optional boolean expression to evaluate
	OnMatch                  func(string)                    // Callback function to report matches as they're found
	IncludeBacklinks         bool                            // Whether to collect first-degree backlinks for matched files
	Backlinks                *map[string][]obsidian.Backlink // Optional output map for backlinks keyed by normalized target path
	PrimaryMatches           *[]string                       // Optional output slice capturing the matches before link following
	SessionStore             *semdb.Store                    // Optional: reuse existing metadata/session store
	MetadataStoreFallback    MetadataStoreFallbackPolicy     // Optional: keep planned one-shot reads live when no store was supplied
	CandidatePaths           []string                        // Optional: restrict store-backed evaluation to this note set; nil means unscoped, empty means scoped to no notes
	obsidian.WikilinkOptions                                 // options influencing backlink parsing
}

// Debug controls whether debug output is printed
var Debug bool

// debugf prints debug output if Debug is true
func debugf(format string, args ...interface{}) {
	if Debug {
		fmt.Fprintf(os.Stderr, format, args...)
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

// FilterSuppressedFiles removes files that contain suppressed tags.
func FilterSuppressedFiles(files []string, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, suppressedTags []string) []string {
	if len(suppressedTags) == 0 {
		return files
	}

	var filtered []string
	facts := NoteFactsFromReader(note)
	for _, file := range files {
		fact, ok := facts.LookupFact(file)
		if !ok || !hasAnySuppressedTags(fact.Tags, suppressedTags) {
			filtered = append(filtered, file)
		} else {
			debugf("Suppressing file %s due to suppressed tags\n", file)
		}
	}

	return filtered
}

// ListFiles is the main function that lists files based on the provided parameters
func ListFiles(vault obsidian.VaultManager, note obsidian.NoteReader, params ListParams) ([]string, error) {
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
	debugf("Found %d initial matching files\n", len(matches))

	// Filter out suppressed files
	matches = FilterSuppressedFiles(matches, vaultDef, note, params.SuppressedTags)
	debugf("After suppression filtering: %d files\n", len(matches))

	if params.PrimaryMatches != nil {
		copied := make([]string, len(matches))
		copy(copied, matches)
		*params.PrimaryMatches = copied
	}

	// If following links, get all connected files
	if params.MaxDepth > 0 {
		linkedFiles := followMatchedFiles(matches, vaultDef, note, params)
		debugf("Found %d total files after following links\n", len(linkedFiles))

		// Apply suppression filter to linked files as well
		linkedFiles = FilterSuppressedFiles(linkedFiles, vaultDef, note, params.SuppressedTags)
		debugf("After suppression filtering linked files: %d files\n", len(linkedFiles))

		if err := loadBacklinks(vaultDef, note, matches, params); err != nil {
			return nil, err
		}

		// Call OnMatch for each linked file
		debugf("Notifying OnMatch with %d files\n", len(linkedFiles))
		notifyMatches(linkedFiles, params.OnMatch)
		return linkedFiles, nil
	}

	if err := loadBacklinks(vaultDef, note, matches, params); err != nil {
		return nil, err
	}

	// Call OnMatch for each matched file - this only happens when not following links
	debugf("Notifying OnMatch with %d files (no link following)\n", len(matches))
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
	backlinks, err := obsidian.CollectBacklinks(vaultDef, note, matches, params.WikilinkOptions, params.SuppressedTags)
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
		debugf("Error getting notes list: %v\n", err)
		return matches
	}

	debugf("Found %d total notes in vault\n", len(allNotes))

	// Build the note path cache, including frontmatter aliases when
	// the reader exposes them so wikilinks like [[SPEC-001]] resolve.
	var aliasesByPath map[string][]string
	if provider, ok := note.(obsidian.NoteEntriesProvider); ok {
		if entries, snapErr := provider.NoteEntriesSnapshot(context.Background()); snapErr == nil {
			aliasesByPath = obsidian.AliasesFromNoteEntries(entries)
		}
	}
	cache := obsidian.BuildNotePathCacheWithAliases(allNotes, aliasesByPath)
	debugf("Built cache with %d entries\n", len(cache.Paths))

	visited := make(map[string]bool)
	var result []string

	// Create wikilinks options from parameters
	options := obsidian.CreateWikilinksOptions(params.MaxDepth, params.SkipAnchors, params.SkipEmbeds)

	for _, notePath := range matches {
		debugf("Following links for note: %s\n", notePath)
		files, err := obsidian.FollowWikilinks(vaultDef, note, notePath, visited, cache, options)
		if err != nil {
			debugf("Error following links for %s: %v\n", notePath, err)
			continue
		}
		debugf("Found %d linked files for %s\n", len(files), notePath)
		result = append(result, files...)
	}

	return obsidian.DeduplicateResults(result)
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

func evaluateExpressionMatches(allNotes []string, note obsidian.NoteReader, expr *InputExpression) []string {
	if expr == nil {
		return nil
	}

	if len(allNotes) == 0 {
		return nil
	}

	results := make(chan string, len(allNotes))
	numWorkers := determineWorkerCount(len(allNotes))
	if numWorkers == 0 {
		return nil
	}
	batchSize := (len(allNotes) + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > len(allNotes) {
			end = len(allNotes)
		}
		if start >= len(allNotes) {
			continue
		}

		wg.Add(1)
		go func(files []string) {
			defer wg.Done()
			facts := NoteFactsFromReader(note)
			for _, path := range files {
				ctx := &noteContext{
					path:         path,
					allowContent: true,
					facts:        facts,
				}
				if evaluateExpression(expr, ctx) {
					results <- path
				}
			}
		}(allNotes[start:end])
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var matches []string
	for path := range results {
		matches = append(matches, path)
	}
	return matches
}

func evaluateExpression(expr *InputExpression, ctx *noteContext) bool {
	if expr == nil {
		return false
	}
	switch expr.Type {
	case exprLeaf:
		return ctx.matchesInput(expr.Input)
	case exprAnd:
		return evaluateExpression(expr.Left, ctx) && evaluateExpression(expr.Right, ctx)
	case exprOr:
		return evaluateExpression(expr.Left, ctx) || evaluateExpression(expr.Right, ctx)
	case exprNot:
		return !evaluateExpression(expr.Left, ctx)
	default:
		return false
	}
}

// MatchesExpressionForPath evaluates an input expression against a single path.
// When allowContent is false, tag/property matches always return false.
func MatchesExpressionForPath(expr *InputExpression, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, path string, allowContent bool) bool {
	if expr == nil {
		return false
	}
	ctx := &noteContext{
		path:         path,
		allowContent: allowContent,
		facts:        NoteFactsFromReader(note),
	}
	return evaluateExpression(expr, ctx)
}

type noteContext struct {
	path         string
	allowContent bool
	facts        NoteFacts
}

func (c *noteContext) matchesInput(input *ListInput) bool {
	if input == nil {
		return false
	}
	switch input.Type {
	case InputTypeFile:
		return matchFilePath(c.path, input.Value)
	case InputTypeFind:
		return obsidian.FuzzyMatch(input.Value, c.path)
	case InputTypeTag:
		if !c.allowContent {
			return false
		}
		fact, ok := c.facts.LookupFact(c.path)
		if !ok {
			return false
		}
		for _, tag := range fact.Tags {
			if tagMatchesParent(tag, input.Value) {
				return true
			}
		}
		return false
	case InputTypeProperty:
		if !c.allowContent {
			return false
		}
		return c.propertyHasValue(input.Property, input.Value)
	default:
		return false
	}
}

func matchFilePath(notePath, input string) bool {
	normalizedInputPath := string(paths.Normalize(input))

	if normalizedInputPath == "*" {
		return true
	}

	normalizedNotePath := string(paths.Normalize(notePath))
	dirPrefix := normalizedInputPath + "/"
	return normalizedNotePath == normalizedInputPath || strings.HasPrefix(normalizedNotePath, dirPrefix)
}

func (c *noteContext) propertyHasValue(property string, target string) bool {
	if strings.TrimSpace(property) == "" || strings.TrimSpace(target) == "" {
		return false
	}

	fact, ok := c.facts.LookupFact(c.path)
	if !ok {
		return false
	}
	return propertyFactsHaveValue(fact.Frontmatter, fact.InlineProps, property, target)
}

// propertyFactsHaveValue checks provider-projected metadata for a property value.
func propertyFactsHaveValue(frontmatter map[string]any, inline map[string][]string, property string, target string) bool {
	if strings.TrimSpace(property) == "" || strings.TrimSpace(target) == "" {
		return false
	}

	targetNorm := normalizePropertyValue(target)
	propKey := strings.ToLower(strings.TrimSpace(property))

	checkValues := func(vals []string) bool {
		for _, v := range vals {
			if normalizePropertyValue(v) == targetNorm {
				return true
			}
		}
		return false
	}

	for k, v := range frontmatter {
		if strings.ToLower(strings.TrimSpace(k)) != propKey {
			continue
		}
		info := obsidian.AnalyzePropertyValue(v)
		if checkValues(info.Values) {
			return true
		}
	}

	for k, vals := range inline {
		if strings.ToLower(strings.TrimSpace(k)) != propKey {
			continue
		}
		if checkValues(vals) {
			return true
		}
	}

	return false
}

// normalizePropertyValue lowercases, trims, and removes wikilink brackets for comparison.
func normalizePropertyValue(v string) string {
	val := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(val, "[[") && strings.HasSuffix(val, "]]") {
		val = strings.TrimSuffix(strings.TrimPrefix(val, "[["), "]]")
	}
	if strings.Contains(val, "|") {
		if parts := strings.SplitN(val, "|", 2); len(parts) > 0 {
			val = strings.TrimSpace(parts[0])
		}
	}
	return val
}

func evaluateExpressionMatchesWithStore(ctx context.Context, store *semdb.Store, expr *InputExpression, candidatePaths []string) ([]string, error) {
	if expr == nil || store == nil {
		return nil, nil
	}
	planned := buildPlannedExpression(expr)
	// Prefer the store-backed planner so broad boolean/find queries can shrink to
	// indexed candidate sets before we touch note metadata rows in Go.
	if matches, ok, err := evaluatePlannedExpressionMatchesWithStore(ctx, store, planned, candidatePaths); err != nil {
		return nil, err
	} else if ok {
		return matches, nil
	}
	pathsList := candidatePaths
	var err error
	if pathsList == nil {
		pathsList, err = store.CurrentNoteMetadataPaths(ctx)
		if err != nil {
			return nil, err
		}
	}
	rowsByPath := map[string]semdb.NoteMetadataRow{}
	if expressionNeedsRows(expr) {
		rowsByPath, err = store.CurrentNoteMetadataRowsByPaths(ctx, pathsList)
		if err != nil {
			return nil, err
		}
	}
	universe := make(map[string]struct{}, len(pathsList))
	for _, path := range pathsList {
		universe[path] = struct{}{}
	}
	matches, err := evalExpressionSet(ctx, store, expr, rowsByPath, universe)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for path := range matches {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func expressionNeedsRows(expr *InputExpression) bool {
	if expr == nil {
		return false
	}
	switch expr.Type {
	case exprLeaf:
		return expr.Input != nil && (expr.Input.Type == InputTypeFind || expr.Input.Type == InputTypeFile)
	case exprAnd, exprOr:
		return expressionNeedsRows(expr.Left) || expressionNeedsRows(expr.Right)
	case exprNot:
		return expressionNeedsRows(expr.Left)
	default:
		return false
	}
}

func evalExpressionSet(ctx context.Context, store *semdb.Store, expr *InputExpression, rowsByPath map[string]semdb.NoteMetadataRow, universe map[string]struct{}) (map[string]struct{}, error) {
	if expr == nil {
		return map[string]struct{}{}, nil
	}
	switch expr.Type {
	case exprLeaf:
		return evalLeafSet(ctx, store, expr.Input, rowsByPath, universe), nil
	case exprAnd:
		left, err := evalExpressionSet(ctx, store, expr.Left, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		right, err := evalExpressionSet(ctx, store, expr.Right, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		return intersectPathSets(left, right), nil
	case exprOr:
		left, err := evalExpressionSet(ctx, store, expr.Left, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		right, err := evalExpressionSet(ctx, store, expr.Right, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		return unionPathSets(left, right), nil
	case exprNot:
		child, err := evalExpressionSet(ctx, store, expr.Left, rowsByPath, universe)
		if err != nil {
			return nil, err
		}
		return subtractPathSet(universe, child), nil
	default:
		return map[string]struct{}{}, nil
	}
}

func evalLeafSet(ctx context.Context, store *semdb.Store, input *ListInput, rowsByPath map[string]semdb.NoteMetadataRow, universe map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	if input == nil {
		return out
	}
	switch input.Type {
	case InputTypeFile:
		for path := range universe {
			if matchFilePath(path, input.Value) {
				out[path] = struct{}{}
			}
		}
	case InputTypeFind:
		for path, row := range rowsByPath {
			if obsidian.FuzzyMatch(input.Value, path) || obsidian.FuzzyMatch(input.Value, row.Title) {
				out[path] = struct{}{}
			}
		}
		// Alias shortcut: a `find:` token that matches a note's
		// frontmatter alias (e.g. `find:SPEC-001`) resolves directly to
		// the owning note even when fuzzy matching would miss the long
		// filename. The DB stores aliases under `value_norm`
		// (lowercased + trimmed by normalizePropertyValue at index
		// time), so we normalize the token the same way before the
		// lookup — otherwise mixed-case inputs like `SPEC-001` never
		// hit alias rows. This is a find/search shortcut only; wikilink
		// target resolution follows Obsidian and does not resolve aliases.
		if token := strings.TrimSpace(input.Value); token != "" {
			if aliasPaths, err := store.CurrentNotePathsByPropertyValue(ctx, "aliases", normalizePropertyValue(token), semdb.NotePropertySourceFrontmatter); err == nil {
				for _, path := range aliasPaths {
					out[path] = struct{}{}
				}
			}
		}
	case InputTypeTag:
		if pathsList, err := store.CurrentNotePathsByTag(ctx, strings.ToLower(strings.TrimSpace(strings.TrimPrefix(input.Value, "#")))); err == nil {
			for _, path := range pathsList {
				out[path] = struct{}{}
			}
		}
	case InputTypeProperty:
		if pathsList, err := store.CurrentNotePathsByPropertyValue(ctx, strings.ToLower(strings.TrimSpace(input.Property)), normalizePropertyValue(input.Value), 0); err == nil {
			for _, path := range pathsList {
				out[path] = struct{}{}
			}
		}
	}
	return out
}

func unionPathSets(left, right map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(left)+len(right))
	for path := range left {
		out[path] = struct{}{}
	}
	for path := range right {
		out[path] = struct{}{}
	}
	return out
}

func intersectPathSets(left, right map[string]struct{}) map[string]struct{} {
	if len(left) > len(right) {
		left, right = right, left
	}
	out := make(map[string]struct{})
	for path := range left {
		if _, ok := right[path]; ok {
			out[path] = struct{}{}
		}
	}
	return out
}

func subtractPathSet(universe, remove map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(universe))
	for path := range universe {
		if _, ok := remove[path]; ok {
			continue
		}
		out[path] = struct{}{}
	}
	return out
}
