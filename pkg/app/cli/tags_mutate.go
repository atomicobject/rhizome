package actions

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// TagMutationSummary represents the result of a tag mutation operation
type TagMutationSummary struct {
	NotesTouched int            `json:"notesTouched"`
	TagChanges   map[string]int `json:"tagChanges"` // tag -> number of notes where this tag was changed
	FilesChanged []string       `json:"filesChanged,omitempty"`
}

func runTagMutations(ctx context.Context, notes []string, processor fileMutationProcessor, workers int) (TagMutationSummary, error) {
	summary, err := runFileMutations(ctx, notes, processor, workers)
	return TagMutationSummary{
		NotesTouched: summary.notesTouched,
		TagChanges:   summary.changes,
		FilesChanged: summary.filesChanged,
	}, err
}

// processDeleteFile handles deletion of tags from a single file
func processDeleteFile(vaultPaths paths.VaultPaths, tagsToDelete []string, dryRun bool) fileMutationProcessor {
	return func(ctx context.Context, notePath string) fileMutation {
		select {
		case <-ctx.Done():
			return fileMutation{err: ctx.Err()}
		default:
		}

		rel, abs, err := paths.ResolveNoteInputWithVaultPaths(vaultPaths, notePath)
		if err != nil || rel == "" || abs == "" {
			if err == nil {
				err = fmt.Errorf("invalid note path %q", notePath)
			}
			return fileMutation{err: err}
		}
		data, err := os.ReadFile(abs.String())
		if err != nil {
			// Skip files we can't read, don't treat as fatal error
			return fileMutation{}
		}
		content := string(data)

		newContent, changed := obsidian.RemoveTags(content, tagsToDelete)
		if !changed {
			return fileMutation{}
		}

		result := fileMutation{
			notesTouched: true,
			changes:      make(map[string]int),
			fileChanged:  rel.String(),
		}

		// Track which tags were actually changed in this file
		for _, tag := range tagsToDelete {
			if hasTag(content, tag) {
				result.changes[tag] = 1
			}
		}

		// Write the file if not dry run
		if !dryRun {
			if err := ctx.Err(); err != nil {
				return fileMutation{err: err}
			}
			err = obsidian.WriteFileAtomic(abs.String(), []byte(newContent), 0644)
			if err != nil {
				result.err = fmt.Errorf("failed to write file %s: %w", notePath, err)
				return result
			}
		}

		return result
	}
}

// processRenameFile handles renaming of tags in a single file
func processRenameFile(vaultPaths paths.VaultPaths, fromTags []string, toTag string, dryRun bool) fileMutationProcessor {
	return func(ctx context.Context, notePath string) fileMutation {
		select {
		case <-ctx.Done():
			return fileMutation{err: ctx.Err()}
		default:
		}

		rel, abs, err := paths.ResolveNoteInputWithVaultPaths(vaultPaths, notePath)
		if err != nil || rel == "" || abs == "" {
			if err == nil {
				err = fmt.Errorf("invalid note path %q", notePath)
			}
			return fileMutation{err: err}
		}
		data, err := os.ReadFile(abs.String())
		if err != nil {
			// Skip files we can't read, don't treat as fatal error
			return fileMutation{}
		}
		content := string(data)

		newContent, changed := obsidian.ReplaceTags(content, fromTags, toTag)
		if !changed {
			return fileMutation{}
		}

		result := fileMutation{
			notesTouched: true,
			changes:      make(map[string]int),
			fileChanged:  rel.String(),
		}

		// Track which tags were actually changed in this file
		for _, tag := range fromTags {
			if hasTag(content, tag) {
				result.changes[tag] = 1
			}
		}

		// Write the file if not dry run
		if !dryRun {
			if err := ctx.Err(); err != nil {
				return fileMutation{err: err}
			}
			err = obsidian.WriteFileAtomic(abs.String(), []byte(newContent), 0644)
			if err != nil {
				result.err = fmt.Errorf("failed to write file %s: %w", notePath, err)
				return result
			}
		}

		return result
	}
}

func tagMutationInputs(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, dryRun bool) (paths.VaultPaths, []string, func(), error) {
	vaultDef, err := resolveVaultDefinition(vault)
	if err != nil {
		return paths.VaultPaths{}, nil, nil, err
	}
	release, err := acquireMutationLock(ctx, vaultDef.BasePath(), dryRun)
	if err != nil {
		return paths.VaultPaths{}, nil, nil, err
	}
	vaultPaths, err := resolveVaultPaths(vaultDef)
	if err != nil {
		release()
		return paths.VaultPaths{}, nil, nil, err
	}

	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		release()
		return paths.VaultPaths{}, nil, nil, fmt.Errorf("failed to get notes list: %w", err)
	}

	return vaultPaths, allNotes, release, nil
}

// isValidTagForOperation checks if a tag is valid for mutation operations
func isValidTagForOperation(tag string) bool {
	if tag == "" {
		return false
	}

	cleanTag := normalizeTagForComparison(tag)
	return obsidian.IsValidTag(cleanTag)
}

// normalizeTagForComparison normalizes a tag for comparison (removes # prefix, trims, lowercases)
func normalizeTagForComparison(tag string) string {
	tag = strings.TrimPrefix(tag, "#")
	return obsidian.NormalizeTag(tag)
}

// hasTag checks if content contains a specific tag (case insensitive)
func hasTag(content, tag string) bool {
	normalizedTag := normalizeTagForComparison(tag)

	// Check frontmatter
	frontmatter, err := obsidian.ExtractFrontmatter(content)
	if err == nil && frontmatter != nil {
		if tags, ok := frontmatter["tags"]; ok {
			tagList := normalizeFrontmatterTags(tags)
			for _, fmTag := range tagList {
				if obsidian.NormalizeTag(fmTag) == normalizedTag {
					return true
				}
			}
		}
	}

	// Check hashtags
	hashtags := obsidian.ExtractHashtags(content)
	for _, hashtag := range hashtags {
		cleanHashtag := hashtag
		if cleanHashtag != "" && cleanHashtag[0] == '#' {
			cleanHashtag = cleanHashtag[1:]
		}
		if obsidian.NormalizeTag(cleanHashtag) == normalizedTag {
			return true
		}
	}

	return false
}

// normalizeFrontmatterTags normalizes tag values from various formats into a clean string slice
// This is a local copy of the normalizeTags function from obsidian package
func normalizeFrontmatterTags(tags interface{}) []string {
	var result []string

	switch t := tags.(type) {
	case string:
		// Handle comma-separated tags directly in a string
		for _, tag := range strings.Split(t, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				result = append(result, tag)
			}
		}
	case []interface{}:
		// Process array of tags, potentially nested
		for _, item := range t {
			// Recursively normalize each item in the array and append
			result = append(result, normalizeFrontmatterTags(item)...)
		}
	case []string:
		// Handle simple string array
		for _, tag := range t {
			if tag = strings.TrimSpace(tag); tag != "" {
				result = append(result, tag)
			}
		}
	}

	return result
}

// processAddFile handles addition of tags to a single file
func processAddFile(vaultPaths paths.VaultPaths, tagsToAdd []string, dryRun bool) fileMutationProcessor {
	return func(ctx context.Context, notePath string) fileMutation {
		select {
		case <-ctx.Done():
			return fileMutation{err: ctx.Err()}
		default:
		}

		rel, abs, err := paths.ResolveNoteInputWithVaultPaths(vaultPaths, notePath)
		if err != nil || rel == "" || abs == "" {
			if err == nil {
				err = fmt.Errorf("invalid note path %q", notePath)
			}
			return fileMutation{err: err}
		}
		data, err := os.ReadFile(abs.String())
		if err != nil {
			// Skip files we can't read, don't treat as fatal error
			return fileMutation{}
		}
		content := string(data)

		newContent, changed := obsidian.AddTags(content, tagsToAdd)
		if !changed {
			return fileMutation{}
		}

		result := fileMutation{
			notesTouched: true,
			changes:      make(map[string]int),
			fileChanged:  rel.String(),
		}

		// Track which tags were actually added to this file
		for _, tag := range tagsToAdd {
			normalizedTag := normalizeTagForComparison(tag)
			if !hasTag(content, tag) { // Tag wasn't present before
				result.changes[normalizedTag] = 1
			}
		}

		// Write the file if not dry run
		if !dryRun {
			if err := ctx.Err(); err != nil {
				return fileMutation{err: err}
			}
			err = obsidian.WriteFileAtomic(abs.String(), []byte(newContent), 0644)
			if err != nil {
				result.err = fmt.Errorf("failed to write file %s: %w", notePath, err)
				return result
			}
		}

		return result
	}
}
