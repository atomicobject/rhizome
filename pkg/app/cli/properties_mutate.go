package actions

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// PropertyMutationSummary captures the result of a property mutation operation.
type PropertyMutationSummary struct {
	NotesTouched    int            `json:"notesTouched"`
	PropertyChanges map[string]int `json:"propertyChanges"`
	FilesChanged    []string       `json:"filesChanged,omitempty"`
}

func runPropertyMutations(ctx context.Context, notes []string, processor fileMutationProcessor, workers int) (PropertyMutationSummary, error) {
	summary, err := runFileMutations(ctx, notes, processor, workers)
	return PropertyMutationSummary{
		NotesTouched:    summary.notesTouched,
		PropertyChanges: summary.changes,
		FilesChanged:    summary.filesChanged,
	}, err
}

func findExistingKey(fm map[string]interface{}, target string) string {
	for key := range fm {
		if strings.EqualFold(key, target) {
			return key
		}
	}
	return ""
}

func processSetProperty(vaultPaths paths.VaultPaths, property string, value interface{}, overwrite, dryRun bool) fileMutationProcessor {
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
			return fileMutation{err: fmt.Errorf("failed to read file %s: %w", notePath, err)}
		}

		newContent, changed, err := obsidian.SetFrontmatterProperty(string(data), property, value, overwrite)
		if err != nil || !changed {
			return fileMutation{err: err}
		}

		if !dryRun {
			if err := ctx.Err(); err != nil {
				return fileMutation{err: err}
			}
			if err := obsidian.WriteFileAtomic(abs.String(), []byte(newContent), 0644); err != nil {
				return fileMutation{err: fmt.Errorf("failed to write file %s: %w", notePath, err)}
			}
		}

		return fileMutation{
			notesTouched: true,
			changes:      map[string]int{property: 1},
			fileChanged:  rel.String(),
		}
	}
}

func processDeleteProperties(vaultPaths paths.VaultPaths, properties []string, dryRun bool) fileMutationProcessor {
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
			return fileMutation{err: fmt.Errorf("failed to read file %s: %w", notePath, err)}
		}

		content := string(data)
		fm, _ := obsidian.ExtractFrontmatter(content)
		var presentProps []string
		if fm != nil {
			for _, p := range properties {
				if key := findExistingKey(fm, p); key != "" {
					presentProps = append(presentProps, p)
				}
			}
		}

		newContent, changed, err := obsidian.DeleteFrontmatterProperties(content, properties)
		if err != nil || !changed {
			return fileMutation{err: err}
		}

		if !dryRun {
			if err := ctx.Err(); err != nil {
				return fileMutation{err: err}
			}
			if err := obsidian.WriteFileAtomic(abs.String(), []byte(newContent), 0644); err != nil {
				return fileMutation{err: fmt.Errorf("failed to write file %s: %w", notePath, err)}
			}
		}

		changes := make(map[string]int, len(presentProps))
		for _, p := range presentProps {
			changes[p] = 1
		}

		return fileMutation{
			notesTouched: true,
			changes:      changes,
			fileChanged:  rel.String(),
		}
	}
}

func processRenameProperties(vaultPaths paths.VaultPaths, from []string, to string, merge, dryRun bool) fileMutationProcessor {
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
			return fileMutation{err: fmt.Errorf("failed to read file %s: %w", notePath, err)}
		}

		content := string(data)
		fm, _ := obsidian.ExtractFrontmatter(content)
		var presentSources []string
		destExists := false
		if fm != nil {
			for key := range fm {
				if strings.EqualFold(key, to) {
					destExists = true
				}
			}
			for _, p := range from {
				if key := findExistingKey(fm, p); key != "" {
					presentSources = append(presentSources, p)
				}
			}
		}

		newContent, changed, err := obsidian.RenameFrontmatterProperties(content, from, to, merge)
		if err != nil || !changed {
			return fileMutation{err: err}
		}

		if !dryRun {
			if err := ctx.Err(); err != nil {
				return fileMutation{err: err}
			}
			if err := obsidian.WriteFileAtomic(abs.String(), []byte(newContent), 0644); err != nil {
				return fileMutation{err: fmt.Errorf("failed to write file %s: %w", notePath, err)}
			}
		}

		changes := make(map[string]int, len(from)+1)
		for _, p := range presentSources {
			changes[p] = 1
		}
		destChanged := !destExists || merge
		if destChanged {
			changes[to]++
		}

		return fileMutation{
			notesTouched: true,
			changes:      changes,
			fileChanged:  rel.String(),
		}
	}
}

// SetPropertyOnFiles sets a property on the provided files. files must be non-empty (pre-filtered by caller).
func SetPropertyOnFiles(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, property string, value interface{}, files []string, overwrite, dryRun bool) (PropertyMutationSummary, error) {
	return SetPropertyOnFilesWithWorkers(ctx, vault, note, property, value, files, overwrite, dryRun, runtime.NumCPU())
}

// SetPropertyOnFilesWithWorkers sets a property on the provided files using workerCount workers.
func SetPropertyOnFilesWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, property string, value interface{}, files []string, overwrite, dryRun bool, workerCount int) (PropertyMutationSummary, error) {
	if len(files) == 0 {
		return PropertyMutationSummary{}, fmt.Errorf("no files specified")
	}

	vaultDef, err := resolveVaultDefinition(vault)
	if err != nil {
		return PropertyMutationSummary{}, err
	}

	release, err := acquireMutationLock(ctx, vaultDef.BasePath(), dryRun)
	if err != nil {
		return PropertyMutationSummary{}, err
	}
	defer release()

	vaultPaths, err := resolveVaultPaths(vaultDef)
	if err != nil {
		return PropertyMutationSummary{}, err
	}
	return runPropertyMutations(ctx, files, processSetProperty(vaultPaths, property, value, overwrite, dryRun), workerCount)
}

// DeleteProperties removes properties across the vault or provided files.
func DeleteProperties(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, properties []string, files []string, dryRun bool) (PropertyMutationSummary, error) {
	return DeletePropertiesWithWorkers(ctx, vault, note, properties, files, dryRun, runtime.NumCPU())
}

func DeletePropertiesWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, properties []string, files []string, dryRun bool, workerCount int) (PropertyMutationSummary, error) {
	if len(properties) == 0 {
		return PropertyMutationSummary{}, fmt.Errorf("no properties specified for deletion")
	}

	vaultDef, err := resolveVaultDefinition(vault)
	if err != nil {
		return PropertyMutationSummary{}, err
	}

	release, err := acquireMutationLock(ctx, vaultDef.BasePath(), dryRun)
	if err != nil {
		return PropertyMutationSummary{}, err
	}
	defer release()

	vaultPaths, err := resolveVaultPaths(vaultDef)
	if err != nil {
		return PropertyMutationSummary{}, err
	}
	targetFiles := files
	if len(targetFiles) == 0 {
		targetFiles, err = note.GetNotesList(vaultDef)
		if err != nil {
			return PropertyMutationSummary{}, fmt.Errorf("failed to get notes list: %w", err)
		}
	}

	return runPropertyMutations(ctx, targetFiles, processDeleteProperties(vaultPaths, properties, dryRun), workerCount)
}

// RenameProperties renames one or more properties to a single destination across the vault or provided files.
func RenameProperties(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, from []string, to string, merge bool, files []string, dryRun bool) (PropertyMutationSummary, error) {
	return RenamePropertiesWithWorkers(ctx, vault, note, from, to, merge, files, dryRun, runtime.NumCPU())
}

func RenamePropertiesWithWorkers(ctx context.Context, vault obsidian.VaultManager, note obsidian.NoteReader, from []string, to string, merge bool, files []string, dryRun bool, workerCount int) (PropertyMutationSummary, error) {
	if len(from) == 0 {
		return PropertyMutationSummary{}, fmt.Errorf("no source properties specified")
	}
	if to == "" {
		return PropertyMutationSummary{}, fmt.Errorf("destination property cannot be empty")
	}

	vaultDef, err := resolveVaultDefinition(vault)
	if err != nil {
		return PropertyMutationSummary{}, err
	}

	release, err := acquireMutationLock(ctx, vaultDef.BasePath(), dryRun)
	if err != nil {
		return PropertyMutationSummary{}, err
	}
	defer release()

	vaultPaths, err := resolveVaultPaths(vaultDef)
	if err != nil {
		return PropertyMutationSummary{}, err
	}
	targetFiles := files
	if len(targetFiles) == 0 {
		targetFiles, err = note.GetNotesList(vaultDef)
		if err != nil {
			return PropertyMutationSummary{}, fmt.Errorf("failed to get notes list: %w", err)
		}
	}

	return runPropertyMutations(ctx, targetFiles, processRenameProperties(vaultPaths, from, to, merge, dryRun), workerCount)
}
