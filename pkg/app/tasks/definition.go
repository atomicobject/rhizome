package tasks

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// Task file parsing lives in pkg/vault/taskdef so validation can share it.
const (
	TasksDir    = taskdef.TasksDir
	TagKey      = taskdef.TagKey
	ScopePerson = taskdef.ScopePerson
	ScopeTeam   = taskdef.ScopeTeam
)

type (
	Check      = taskdef.Check
	Definition = taskdef.Definition
	Problem    = taskdef.Problem
)

// ParseDefinition parses and validates one task file.
func ParseDefinition(filePath string, data []byte) (Definition, error) {
	return taskdef.ParseDefinition(filePath, data)
}

// ParseDuration accepts Go durations plus whole days (`7d`) and weeks (`2w`).
func ParseDuration(s string) (time.Duration, error) { return taskdef.ParseDuration(s) }

// LoadDefinitions reads every task file at a commit. A file that fails to
// parse is reported as a problem and does not affect the others.
func LoadDefinitions(ctx context.Context, repo Repo, commit string) ([]Definition, []Problem, error) {
	files, err := repo.TaskFiles(ctx, commit)
	if err != nil {
		return nil, nil, err
	}
	defs, problems := taskdef.Load(files, func(file string) ([]byte, error) {
		return repo.ShowFile(ctx, commit, file)
	})
	return defs, problems, nil
}
