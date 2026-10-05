package actions

import (
	"context"
	"fmt"
	"runtime"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// DeleteTags removes specified tags from all notes in the vault
func DeleteTags(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToDelete []string, dryRun bool) (TagMutationSummary, error) {
	return DeleteTagsWithWorkers(ctx, vault, note, tagsToDelete, dryRun, runtime.NumCPU())
}

// DeleteTagsWithWorkers removes specified tags from all notes in the vault using specified worker count
func DeleteTagsWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToDelete []string, dryRun bool, workers int) (TagMutationSummary, error) {
	if len(tagsToDelete) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no tags specified for deletion")
	}

	// Validate tags
	for _, tag := range tagsToDelete {
		if !isValidTagForOperation(tag) {
			return TagMutationSummary{}, fmt.Errorf("invalid tag: %s", tag)
		}
	}

	vaultPaths, allNotes, release, err := tagMutationInputs(ctx, vault, note, dryRun)
	if err != nil {
		return TagMutationSummary{}, err
	}
	defer release()

	return runTagMutations(ctx, allNotes, processDeleteFile(vaultPaths, tagsToDelete, dryRun), workers)
}

// DeleteTagsFromFiles deletes tags from a provided list of files.
func DeleteTagsFromFiles(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToDelete []string, files []string, dryRun bool) (TagMutationSummary, error) {
	return DeleteTagsFromFilesWithWorkers(ctx, vault, note, tagsToDelete, files, dryRun, runtime.NumCPU())
}

// DeleteTagsFromFilesWithWorkers deletes tags from specific files using workerCount workers.
func DeleteTagsFromFilesWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToDelete []string, files []string, dryRun bool, workers int) (TagMutationSummary, error) {
	if len(tagsToDelete) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no tags specified for deletion")
	}
	if len(files) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no files specified")
	}
	for _, tag := range tagsToDelete {
		if !isValidTagForOperation(tag) {
			return TagMutationSummary{}, fmt.Errorf("invalid tag: %s", tag)
		}
	}

	vaultDef, err := resolveVaultDefinition(vault)
	if err != nil {
		return TagMutationSummary{}, err
	}

	release, err := acquireMutationLock(ctx, vaultDef.BasePath(), dryRun)
	if err != nil {
		return TagMutationSummary{}, err
	}
	defer release()
	vaultPaths, err := resolveVaultPaths(vaultDef)
	if err != nil {
		return TagMutationSummary{}, err
	}

	return runTagMutations(ctx, files, processDeleteFile(vaultPaths, tagsToDelete, dryRun), workers)
}

// RenameTags replaces specified tags with a new tag in all notes in the vault
func RenameTags(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, fromTags []string, toTag string, dryRun bool) (TagMutationSummary, error) {
	return RenameTagsWithWorkers(ctx, vault, note, fromTags, toTag, dryRun, runtime.NumCPU())
}

// RenameTagsWithWorkers replaces specified tags with a new tag in all notes in the vault using specified worker count
func RenameTagsWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, fromTags []string, toTag string, dryRun bool, workers int) (TagMutationSummary, error) {
	if len(fromTags) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no source tags specified for rename")
	}

	if toTag == "" {
		return TagMutationSummary{}, fmt.Errorf("destination tag cannot be empty")
	}

	// Validate all tags
	for _, tag := range fromTags {
		if !isValidTagForOperation(tag) {
			return TagMutationSummary{}, fmt.Errorf("invalid source tag: %s", tag)
		}
	}

	if !isValidTagForOperation(toTag) {
		return TagMutationSummary{}, fmt.Errorf("invalid destination tag: %s", toTag)
	}

	// Check for circular rename (trying to rename to one of the source tags)
	normalizedTo := normalizeTagForComparison(toTag)
	for _, fromTag := range fromTags {
		if normalizeTagForComparison(fromTag) == normalizedTo {
			return TagMutationSummary{}, fmt.Errorf("cannot rename tag %s to itself", fromTag)
		}
	}

	vaultPaths, allNotes, release, err := tagMutationInputs(ctx, vault, note, dryRun)
	if err != nil {
		return TagMutationSummary{}, err
	}
	defer release()

	return runTagMutations(ctx, allNotes, processRenameFile(vaultPaths, fromTags, toTag, dryRun), workers)
}

// RenameTagsInFiles renames tags in a specific set of files.
func RenameTagsInFiles(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, fromTags []string, toTag string, files []string, dryRun bool) (TagMutationSummary, error) {
	return RenameTagsInFilesWithWorkers(ctx, vault, note, fromTags, toTag, files, dryRun, runtime.NumCPU())
}

// RenameTagsInFilesWithWorkers renames tags in a specific set of files using workerCount workers.
func RenameTagsInFilesWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, fromTags []string, toTag string, files []string, dryRun bool, workers int) (TagMutationSummary, error) {
	if len(fromTags) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no source tags specified for rename")
	}
	if toTag == "" {
		return TagMutationSummary{}, fmt.Errorf("destination tag cannot be empty")
	}
	if len(files) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no files specified")
	}
	for _, tag := range fromTags {
		if !isValidTagForOperation(tag) {
			return TagMutationSummary{}, fmt.Errorf("invalid source tag: %s", tag)
		}
	}
	if !isValidTagForOperation(toTag) {
		return TagMutationSummary{}, fmt.Errorf("invalid destination tag: %s", toTag)
	}
	normalizedTo := normalizeTagForComparison(toTag)
	for _, fromTag := range fromTags {
		if normalizeTagForComparison(fromTag) == normalizedTo {
			return TagMutationSummary{}, fmt.Errorf("cannot rename tag %s to itself", fromTag)
		}
	}

	vaultDef, err := resolveVaultDefinition(vault)
	if err != nil {
		return TagMutationSummary{}, err
	}

	release, err := acquireMutationLock(ctx, vaultDef.BasePath(), dryRun)
	if err != nil {
		return TagMutationSummary{}, err
	}
	defer release()
	vaultPaths, err := resolveVaultPaths(vaultDef)
	if err != nil {
		return TagMutationSummary{}, err
	}

	return runTagMutations(ctx, files, processRenameFile(vaultPaths, fromTags, toTag, dryRun), workers)
}

// AddTags adds specified tags to all notes in the vault
func AddTags(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToAdd []string, dryRun bool) (TagMutationSummary, error) {
	return AddTagsWithWorkers(ctx, vault, note, tagsToAdd, dryRun, runtime.NumCPU())
}

// AddTagsWithWorkers adds specified tags to all notes in the vault using specified worker count
func AddTagsWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToAdd []string, dryRun bool, workers int) (TagMutationSummary, error) {
	if len(tagsToAdd) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no tags specified for addition")
	}

	// Validate tags
	for _, tag := range tagsToAdd {
		if !isValidTagForOperation(tag) {
			return TagMutationSummary{}, fmt.Errorf("invalid tag: %s", tag)
		}
	}

	vaultPaths, allNotes, release, err := tagMutationInputs(ctx, vault, note, dryRun)
	if err != nil {
		return TagMutationSummary{}, err
	}
	defer release()

	return runTagMutations(ctx, allNotes, processAddFile(vaultPaths, tagsToAdd, dryRun), workers)
}

// AddTagsToFiles adds specified tags to a specific list of files
func AddTagsToFiles(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToAdd []string, files []string, dryRun bool) (TagMutationSummary, error) {
	return AddTagsToFilesWithWorkers(ctx, vault, note, tagsToAdd, files, dryRun, runtime.NumCPU())
}

// AddTagsToFilesWithWorkers adds specified tags to a specific list of files using specified worker count
func AddTagsToFilesWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, tagsToAdd []string, files []string, dryRun bool, workers int) (TagMutationSummary, error) {
	if len(tagsToAdd) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no tags specified for addition")
	}

	if len(files) == 0 {
		return TagMutationSummary{}, fmt.Errorf("no files specified")
	}

	// Validate tags
	for _, tag := range tagsToAdd {
		if !isValidTagForOperation(tag) {
			return TagMutationSummary{}, fmt.Errorf("invalid tag: %s", tag)
		}
	}

	vaultDef, err := resolveVaultDefinition(vault)
	if err != nil {
		return TagMutationSummary{}, err
	}

	release, err := acquireMutationLock(ctx, vaultDef.BasePath(), dryRun)
	if err != nil {
		return TagMutationSummary{}, err
	}
	defer release()
	vaultPaths, err := resolveVaultPaths(vaultDef)
	if err != nil {
		return TagMutationSummary{}, err
	}

	return runTagMutations(ctx, files, processAddFile(vaultPaths, tagsToAdd, dryRun), workers)
}
