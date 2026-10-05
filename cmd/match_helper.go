package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// resolveMatches converts find/tag/path patterns into a note list. Returns nil if no patterns provided.
func resolveMatches(vault *obsidian.Vault, note obsidian.NoteReader, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	inputs, expr, err := actions.ParseInputsWithExpression(patterns)
	if err != nil {
		return nil, fmt.Errorf("error parsing match criteria: %w", err)
	}

	matchingFiles, err := actions.ListFiles(vault, note, actions.ListParams{
		Inputs:         inputs,
		Expression:     expr,
		MaxDepth:       0,
		SkipAnchors:    false,
		SkipEmbeds:     false,
		AbsolutePaths:  false,
		SuppressedTags: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get matching files: %w", err)
	}

	return matchingFiles, nil
}
