package actions

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// TagSummary represents a tag with its individual and aggregate counts
type TagSummary struct {
	Name            string `json:"name"`            // e.g. "a/c"
	IndividualCount int    `json:"individualCount"` // notes that contain this exact tag
	AggregateCount  int    `json:"aggregateCount"`  // notes that contain this tag OR any descendant
}

// TagsOptions allows restricting the scan to a subset of notes.
type TagsOptions struct {
	Notes []string // optional subset of note paths to analyze; if empty, scans all notes
}

// tagNode represents a node in the tag hierarchy tree
type tagNode struct {
	name            string
	individualCount int
	aggregateCount  int
	children        []*tagNode
}

// Tags returns all tags in the vault (or provided notes) with their counts, sorted by aggregate count descending
func Tags(vault obsidian.VaultManager, note obsidian.NoteReader, opts ...TagsOptions) ([]TagSummary, error) {
	_, err := vault.Definition()
	if err != nil {
		return nil, err
	}

	facts := NoteFactsFromReader(note)
	var scanNotes []string
	var filter map[string]struct{}
	if len(opts) > 0 && len(opts[0].Notes) > 0 {
		scanNotes = opts[0].Notes
		filter = make(map[string]struct{})
		for _, n := range scanNotes {
			filter[string(paths.NormalizeNotePath(n))] = struct{}{}
		}
	} else {
		scanNotes = facts.Paths()
	}

	individualCount := make(map[string]int)
	aggregateCount := make(map[string]int)
	for _, notePath := range scanNotes {
		if filter != nil {
			if _, ok := filter[paths.NormalizeNotePath(notePath).String()]; !ok {
				continue
			}
		}
		fact, ok := facts.LookupFact(notePath)
		if !ok {
			continue
		}
		tags := fact.Tags
		perNoteTags := make(map[string]struct{}, len(tags))
		for _, tag := range tags {
			normalizedTag := normalizeTag(tag)
			if normalizedTag != "" && isValidTag(normalizedTag) {
				perNoteTags[normalizedTag] = struct{}{}
			}
		}
		for tag := range perNoteTags {
			individualCount[tag]++
		}
		perNotePrefixes := make(map[string]struct{}, len(perNoteTags))
		for tag := range perNoteTags {
			for _, prefix := range getAllPrefixes(tag) {
				perNotePrefixes[prefix] = struct{}{}
			}
		}
		for prefix := range perNotePrefixes {
			aggregateCount[prefix]++
		}
	}

	// Step 3: Build hierarchy tree
	root := buildTagTree(individualCount, aggregateCount)

	// Step 4: Sort and flatten
	sortTagTree(root)
	return flattenTagTree(root), nil
}

// normalizeTag normalizes a tag by trimming spaces and converting to lowercase
func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// isValidTag checks if a tag is valid according to Obsidian's rules
func isValidTag(tag string) bool {
	if tag == "" {
		return false
	}

	// Tags cannot contain spaces
	if strings.Contains(tag, " ") {
		return false
	}

	// Tags cannot be purely numeric
	if isNumeric(tag) {
		return false
	}

	// Tags must contain at least one letter
	if !containsLetter(tag) {
		return false
	}

	return true
}

// isNumeric checks if a string is purely numeric
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// containsLetter checks if a string contains at least one letter
func containsLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// getAllPrefixes returns all prefixes of a hierarchical tag
// e.g. "a/b/c" returns ["a", "a/b", "a/b/c"]
func getAllPrefixes(tag string) []string {
	parts := strings.Split(tag, "/")
	var prefixes []string

	for i := 1; i <= len(parts); i++ {
		prefix := strings.Join(parts[:i], "/")
		prefixes = append(prefixes, prefix)
	}

	return prefixes
}

// buildTagTree builds a tree structure from the tag counts
func buildTagTree(individualCount, aggregateCount map[string]int) *tagNode {
	root := &tagNode{name: "", children: []*tagNode{}}
	nodeMap := make(map[string]*tagNode)
	nodeMap[""] = root

	// Create all nodes first
	for tag := range aggregateCount {
		parts := strings.Split(tag, "/")
		currentPath := ""

		for _, part := range parts {
			if currentPath == "" {
				currentPath = part
			} else {
				currentPath = currentPath + "/" + part
			}

			if _, exists := nodeMap[currentPath]; !exists {
				node := &tagNode{
					name:            currentPath,
					individualCount: individualCount[currentPath],
					aggregateCount:  aggregateCount[currentPath],
					children:        []*tagNode{},
				}
				nodeMap[currentPath] = node
			}
		}
	}

	// Build parent-child relationships
	for tag, node := range nodeMap {
		if tag == "" {
			continue // Skip root
		}

		// Find parent
		lastSlash := strings.LastIndex(tag, "/")
		var parentPath string
		if lastSlash == -1 {
			parentPath = "" // Parent is root
		} else {
			parentPath = tag[:lastSlash]
		}

		parent := nodeMap[parentPath]
		parent.children = append(parent.children, node)
	}

	return root
}

// sortTagTree sorts the tree nodes by aggregate count descending, then lexicographically
func sortTagTree(node *tagNode) {
	// Sort children
	sort.Slice(node.children, func(i, j int) bool {
		a, b := node.children[i], node.children[j]
		if a.aggregateCount != b.aggregateCount {
			return a.aggregateCount > b.aggregateCount // Descending by aggregate count
		}
		return a.name < b.name // Ascending lexicographically for ties
	})

	// Recursively sort children
	for _, child := range node.children {
		sortTagTree(child)
	}
}

// flattenTagTree flattens the sorted tree into a slice of TagSummary
func flattenTagTree(node *tagNode) []TagSummary {
	result := make([]TagSummary, 0)

	// Add current node if it's not the root
	if node.name != "" {
		result = append(result, TagSummary{
			Name:            node.name,
			IndividualCount: node.individualCount,
			AggregateCount:  node.aggregateCount,
		})
	}

	// Add children in order
	for _, child := range node.children {
		result = append(result, flattenTagTree(child)...)
	}

	return result
}
