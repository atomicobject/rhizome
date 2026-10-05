package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

// PathCheck is an ignore decision, not a note-selection or language-support test.
type PathCheck struct {
	Input           string          `json:"input"`
	Path            string          `json:"path,omitempty"`
	Status          string          `json:"status"`
	Rule            *ignore.RuleRef `json:"rule,omitempty"`
	IgnoredAncestor string          `json:"ignoredAncestor,omitempty"`
	Boundary        *ignore.RuleRef `json:"boundary,omitempty"`
	Error           string          `json:"error,omitempty"`
}

// CheckPathsTool evaluates a collection with one shared matcher. Entries specify
// isDir so nonexistent paths can be checked without guessing their type.
func CheckPathsTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, call mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Paths []struct {
				Path  string `json:"path"`
				IsDir bool   `json:"isDir"`
			} `json:"paths"`
		}
		data, err := json.Marshal(call.GetArguments())
		if err != nil {
			return mcp.NewToolResultError("invalid path inputs"), nil
		}
		if err = json.Unmarshal(data, &input); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(input.Paths) == 0 || len(input.Paths) > 10000 {
			return mcp.NewToolResultError("paths requires 1 to 10000 entries"), nil
		}
		vp, err := paths.NewVaultPaths(config.VaultPath)
		if err != nil || vp.Root() == "" {
			return mcp.NewToolResultError("vault root unavailable"), nil
		}
		matcher := obsidian.LoadVaultIgnoreMatcher(vp.Root(), config.VaultDef.Excludes)
		results := make([]PathCheck, len(input.Paths))
		for i, item := range input.Paths {
			if err := ctx.Err(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			result := PathCheck{Input: item.Path, Status: "invalid"}
			rel, err := vp.RelStrict(item.Path)
			if strings.TrimSpace(item.Path) == "" || strings.ContainsRune(item.Path, 0) {
				err = fmt.Errorf("path must be nonempty and contain no NUL")
			}
			if err == nil {
				// Check physical containment too, including symlinks in existing ancestors.
				err = checkPathContainment(vp, filepath.Join(vp.Root(), rel.String()))
			}
			if err != nil {
				result.Error = err.Error()
			} else {
				d := matcher.Explain(rel.String(), item.IsDir)
				result.Path = rel.String()
				result.Status = "allowed"
				if d.Ignored {
					result.Status = "ignored"
				}
				result.Rule = d.Rule
				result.IgnoredAncestor = d.IgnoredAncestor
				result.Boundary = d.Boundary
			}
			results[i] = result
		}
		return respondJSON(struct {
			Paths []PathCheck `json:"paths"`
		}{results}, "marshal path checks failed")
	}
}

// Resolve the nearest existing ancestor so a new file below an outward symlink
// is rejected too. Missing leaf names do not require the file to exist.
func checkPathContainment(vp paths.VaultPaths, absolute string) error {
	for current := absolute; ; current = filepath.Dir(current) {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			_, err = vp.RelStrict(resolved)
			return err
		}
		if !os.IsNotExist(err) {
			return err
		}
		if _, statErr := os.Lstat(current); statErr == nil {
			return err
		}
		if filepath.Dir(current) == current {
			return err
		}
	}
}
