package taskdef

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	// LandInPlace commits a run's declared files in the open checkout.
	LandInPlace = "in-place"
	// LandPR commits a run's changes to a branch in a worktree Rhizome makes.
	LandPR = "pr"
	// SlotKey starts the commit line naming the slot a task commit ran for.
	SlotKey = "Rhizome-Task-Slot"
)

// parseLanding resolves land's default and checks writes. land defaults to
// in-place when the task declares writes and to pr otherwise, so task files
// from before writes existed keep landing in a worktree.
func (d *Definition) parseLanding() error {
	switch d.Land {
	case "":
		d.Land = LandPR
		if len(d.Writes) > 0 {
			d.Land = LandInPlace
		}
	case LandInPlace:
		if len(d.Writes) == 0 {
			return errors.New("land: in-place needs writes, the repository-relative patterns of files the task may commit")
		}
	case LandPR:
		if len(d.Writes) > 0 {
			return errors.New("writes applies only to land: in-place")
		}
	default:
		return fmt.Errorf("land must be %q or %q", LandInPlace, LandPR)
	}
	for _, p := range d.Writes {
		if err := checkWritePattern(p); err != nil {
			return fmt.Errorf("writes: %w", err)
		}
	}
	return nil
}

func checkWritePattern(p string) error {
	if p == "" || strings.TrimSpace(p) != p {
		return fmt.Errorf("pattern %q must be non-empty without surrounding spaces", p)
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return fmt.Errorf("pattern %q must be repository-relative with forward slashes", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("pattern %q must not have empty, . or .. segments", p)
		}
	}
	if !doublestar.ValidatePattern(p) {
		return fmt.Errorf("pattern %q is not a valid glob", p)
	}
	return nil
}

// MayWrite reports whether a repository-relative, slash-separated path
// matches one of the task's writes patterns (`*` stays within a folder,
// `**` crosses folders).
func (d Definition) MayWrite(path string) bool {
	for _, p := range d.Writes {
		if ok, _ := doublestar.Match(p, path); ok {
			return true
		}
	}
	return false
}
