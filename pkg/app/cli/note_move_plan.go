package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type noteMoveEndpoint struct {
	rel  string
	abs  string
	info os.FileInfo // nil only for an absent target
}

type noteMovePlan struct {
	source         noteMoveEndpoint
	target         noteMoveEndpoint
	authoredSource string
	caseOnly       bool
}

// Plan the complete namespace change under the caller's write lease. Parent
// aliases and hardlinks must not turn independent moves into destructive ones.
func planNoteMoves(root string, indexer notemeta.Indexer, moves []MoveRequest, overwrite bool) ([]noteMovePlan, error) {
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return nil, err
	}
	plans := make([]noteMovePlan, 0, len(moves))
	for _, move := range moves {
		if strings.TrimSpace(move.Source) == "" || strings.TrimSpace(move.Target) == "" {
			return nil, fmt.Errorf("source and target note names are required")
		}
		sourceRel := obsidian.NormalizeWithDefaultExt(move.Source, ".md")
		targetRel := obsidian.NormalizeWithDefaultExt(move.Target, ".md")
		if err := validateNoteMoveFormats(indexer, sourceRel, targetRel); err != nil {
			return nil, err
		}
		source, err := inspectMoveEndpoint(vaultPaths, sourceRel, true)
		if err != nil {
			return nil, err
		}
		target, err := inspectMoveEndpoint(vaultPaths, targetRel, false)
		if err != nil {
			return nil, err
		}
		plan := noteMovePlan{source: source, target: target, authoredSource: sourceRel}
		if sameMoveEndpoint(source, target) {
			actualName, err := caseOnlySourceName(source, target)
			if err != nil {
				return nil, err
			}
			if actualName == "" {
				return nil, fmt.Errorf("source and target are the same file: %s and %s", source.rel, target.rel)
			}
			plan.caseOnly = true
			plan.source.abs = filepath.Join(filepath.Dir(source.abs), actualName)
			plan.source.rel = filepath.ToSlash(filepath.Join(filepath.Dir(source.rel), actualName))
		}
		if target.info != nil && !overwrite && !plan.caseOnly {
			return nil, fmt.Errorf("target note already exists: %s", target.rel)
		}
		plans = append(plans, plan)
	}
	for i, plan := range plans {
		for j := 0; j < i; j++ {
			other := plans[j]
			switch {
			case sameMoveEndpoint(plan.source, other.source):
				return nil, fmt.Errorf("duplicate source: %s and %s", other.source.rel, plan.source.rel)
			case sameMoveEndpoint(plan.target, other.target):
				return nil, fmt.Errorf("duplicate target %s for sources %s and %s", plan.target.rel, other.source.rel, plan.source.rel)
			case moveTargetsHaveAncestry(plan.target, other.target):
				return nil, fmt.Errorf("move target is another target's directory: %s and %s", other.target.rel, plan.target.rel)
			case sameMoveEndpoint(plan.source, other.target), sameMoveEndpoint(plan.target, other.source):
				return nil, fmt.Errorf("move sources and targets overlap: %s and %s", other.source.rel, plan.source.rel)
			}
		}
	}
	return plans, nil
}

func sameMoveEndpoint(a, b noteMoveEndpoint) bool {
	return paths.CaseEqual(a.abs, b.abs) || (a.info != nil && b.info != nil && os.SameFile(a.info, b.info))
}

func moveTargetsHaveAncestry(a, b noteMoveEndpoint) bool {
	aParts, bParts := strings.Split(filepath.ToSlash(a.abs), "/"), strings.Split(filepath.ToSlash(b.abs), "/")
	if len(aParts) < len(bParts) {
		return paths.CaseEqual(filepath.ToSlash(a.abs), strings.Join(bParts[:len(aParts)], "/"))
	}
	if len(bParts) < len(aParts) {
		return paths.CaseEqual(filepath.ToSlash(b.abs), strings.Join(aParts[:len(bParts)], "/"))
	}
	return false
}

func inspectMoveEndpoint(vaultPaths paths.VaultPaths, input string, source bool) (noteMoveEndpoint, error) {
	rel, err := paths.CleanRelPath(input)
	if err != nil {
		return noteMoveEndpoint{}, err
	}
	// Resolve only the parent: a symlink at the leaf is an entry to reject,
	// not a file whose referent the move owns.
	parentPath, err := vaultPaths.Abs(paths.RelPath(filepath.ToSlash(filepath.Dir(rel.String()))))
	if err != nil {
		return noteMoveEndpoint{}, err
	}
	parent, err := resolveMoveParent(parentPath.String())
	if err != nil {
		return noteMoveEndpoint{}, fmt.Errorf("invalid note parent for %s: %w", input, err)
	}
	parentRel, err := vaultPaths.RelStrict(parent)
	if err != nil {
		return noteMoveEndpoint{}, err
	}
	// Windows resolves an existing leaf to its current entry spelling. Keep the
	// requested leaf for the destination and canonicalize only its parent.
	leaf := filepath.Base(rel.String())
	abs := filepath.Join(parent, leaf)
	canonicalRel := filepath.ToSlash(filepath.Join(parentRel.String(), leaf))
	info, err := os.Lstat(abs)
	if err != nil {
		if !os.IsNotExist(err) || source {
			if source && os.IsNotExist(err) {
				return noteMoveEndpoint{}, fmt.Errorf("source note does not exist: %w", err)
			}
			return noteMoveEndpoint{}, fmt.Errorf("unable to inspect note %s: %w", input, err)
		}
	} else if !info.Mode().IsRegular() {
		return noteMoveEndpoint{}, fmt.Errorf("note move requires a regular file: %s", input)
	}
	if source {
		// Retain the existing canonical source spelling, including for Windows
		// Git, after the leaf has been verified as a regular entry.
		sourceRel, err := vaultPaths.RelStrict(abs)
		if err != nil {
			return noteMoveEndpoint{}, err
		}
		canonicalRel = sourceRel.String()
	}
	return noteMoveEndpoint{rel: canonicalRel, abs: abs, info: info}, nil
}

// Resolve the nearest existing parent before appending missing directories.
// This also validates target parents without creating anything during preflight.
func resolveMoveParent(parent string) (string, error) {
	probe := parent
	var suffix []string
	for {
		_, err := os.Lstat(probe)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || filepath.Dir(probe) == probe {
			return "", err
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = filepath.Dir(probe)
	}
	resolved, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("parent is not a directory: %s", probe)
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return resolved, nil
}

// A real case-only basename transition has one directory entry. Two hardlinks
// with case variants on a sensitive filesystem are distinct authored endpoints.
func caseOnlySourceName(source, target noteMoveEndpoint) (string, error) {
	sourceName, targetName := filepath.Base(source.abs), filepath.Base(target.abs)
	if !paths.CaseEqual(filepath.Dir(source.abs), filepath.Dir(target.abs)) || sourceName == targetName || !strings.EqualFold(sourceName, targetName) {
		return "", nil
	}
	entries, err := os.ReadDir(filepath.Dir(source.abs))
	if err != nil {
		return "", err
	}
	name := ""
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), sourceName) {
			if name != "" {
				return "", nil
			}
			name = entry.Name()
		}
	}
	return name, nil
}

func (plan noteMovePlan) sourceAliases() []string {
	if plan.authoredSource == plan.source.rel {
		return nil
	}
	return []string{plan.authoredSource}
}
