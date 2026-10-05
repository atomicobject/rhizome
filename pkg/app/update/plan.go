package update

import (
	"fmt"
	"path/filepath"
	"strings"
)

// UpdateMode identifies the caller's requested update policy.
type UpdateMode string

const (
	UpdateModeDefault    UpdateMode = "default"
	UpdateModeLatest     UpdateMode = "latest"
	UpdateModePinned     UpdateMode = "pinned"
	UpdateModeSetVersion UpdateMode = "set-version"
)

// UpdateTargetRole identifies the logical installation being updated.
type UpdateTargetRole string

const (
	UpdateTargetGlobal UpdateTargetRole = "global"
	UpdateTargetRepo   UpdateTargetRole = "repo"
)

// UpdateTarget is one ordered logical destination in an update plan.
type UpdateTarget struct {
	Role        UpdateTargetRole
	LogicalPath string
	Version     string
	WriteMarker bool
	WritePin    bool
}

// UpdatePlan is the ordered set of logical destinations to mutate.
type UpdatePlan struct {
	Targets []UpdateTarget
}

// UpdatePlanOptions contains policy inputs that are independent of filesystem
// discovery and user interaction.
type UpdatePlanOptions struct {
	LatestVersion string
	Mode          UpdateMode
	SetVersion    string
	AdvancePin    bool
}

// BuildUpdatePlan applies update policy to a resolved invocation. Target
// ordering is significant: a global installation precedes a repo installation.
// Deduplication uses cleaned logical pathnames only; filesystem identity must
// not collapse distinct installation destinations.
func BuildUpdatePlan(invocation Invocation, opts UpdatePlanOptions) (UpdatePlan, error) {
	mode := opts.Mode
	if mode == "" {
		mode = UpdateModeDefault
	}
	if err := validatePlanInputs(invocation, opts, mode); err != nil {
		return UpdatePlan{}, err
	}

	latest := NormalizeVersion(opts.LatestVersion)
	pin := NormalizeVersion(invocation.RepoPinVersion)
	setVersion := NormalizeVersion(opts.SetVersion)
	globalPath := cleanLogicalPath(invocation.ExecutablePath)
	repoPath := cleanLogicalPath(invocation.RepoTargetPath)

	plan := UpdatePlan{Targets: []UpdateTarget{}}
	addGlobal := func(version string) error {
		return appendUpdateTarget(&plan, UpdateTarget{
			Role:        UpdateTargetGlobal,
			LogicalPath: globalPath,
			Version:     version,
		})
	}
	addRepo := func(version string, writePin bool) error {
		return appendUpdateTarget(&plan, UpdateTarget{
			Role:        UpdateTargetRepo,
			LogicalPath: repoPath,
			Version:     version,
			WriteMarker: true,
			WritePin:    writePin,
		})
	}

	if !invocation.RepoPinned {
		if err := addGlobal(latest); err != nil {
			return UpdatePlan{}, err
		}
		return plan, nil
	}

	switch mode {
	case UpdateModePinned:
		if err := addRepo(pin, false); err != nil {
			return UpdatePlan{}, err
		}
	case UpdateModeSetVersion:
		if err := addRepo(setVersion, true); err != nil {
			return UpdatePlan{}, err
		}
	case UpdateModeLatest:
		if !invocation.RepoScoped {
			if err := addGlobal(latest); err != nil {
				return UpdatePlan{}, err
			}
		}
		if err := addRepo(latest, true); err != nil {
			return UpdatePlan{}, err
		}
	case UpdateModeDefault:
		if !invocation.RepoScoped {
			if err := addGlobal(latest); err != nil {
				return UpdatePlan{}, err
			}
		}
		if pin == latest {
			if err := addRepo(latest, false); err != nil {
				return UpdatePlan{}, err
			}
		} else if opts.AdvancePin {
			if err := addRepo(latest, true); err != nil {
				return UpdatePlan{}, err
			}
		}
	}

	return plan, nil
}

func validatePlanInputs(invocation Invocation, opts UpdatePlanOptions, mode UpdateMode) error {
	switch mode {
	case UpdateModeDefault, UpdateModeLatest, UpdateModePinned, UpdateModeSetVersion:
	default:
		return fmt.Errorf("unknown update mode %q", mode)
	}

	if strings.TrimSpace(invocation.ExecutablePath) == "" {
		return fmt.Errorf("executable path is required")
	}
	if invocation.RepoScoped && !invocation.RepoPinned {
		return fmt.Errorf("repo-scoped invocation requires a pinned repository")
	}
	if invocation.RepoPinned {
		if strings.TrimSpace(invocation.RepoTargetPath) == "" {
			return fmt.Errorf("repo target path is required for a pinned repository")
		}
		pin := NormalizeVersion(invocation.RepoPinVersion)
		if pin == "" {
			return fmt.Errorf("repo pin version is required for a pinned repository")
		}
		if pin == "latest" {
			return fmt.Errorf("repo pin version must identify an exact version")
		}
	} else if strings.TrimSpace(invocation.RepoTargetPath) != "" {
		return fmt.Errorf("repo target path requires a pinned repository")
	}

	setVersion := NormalizeVersion(opts.SetVersion)
	if mode == UpdateModeSetVersion {
		if setVersion == "" {
			return fmt.Errorf("set version is required in set-version mode")
		}
		if setVersion == "latest" {
			return fmt.Errorf("set version must identify an exact version")
		}
		if !invocation.RepoPinned {
			return fmt.Errorf("set-version mode requires a pinned repository")
		}
	} else if setVersion != "" {
		return fmt.Errorf("set version requires set-version mode")
	}

	if mode == UpdateModePinned && !invocation.RepoPinned {
		return fmt.Errorf("pinned mode requires a pinned repository")
	}
	if mode == UpdateModeDefault || mode == UpdateModeLatest {
		latest := NormalizeVersion(opts.LatestVersion)
		if latest == "" || latest == "latest" {
			return fmt.Errorf("latest version is required")
		}
	}

	return nil
}

func cleanLogicalPath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}

func appendUpdateTarget(plan *UpdatePlan, target UpdateTarget) error {
	target.LogicalPath = cleanLogicalPath(target.LogicalPath)
	target.Version = NormalizeVersion(target.Version)
	for i := range plan.Targets {
		if plan.Targets[i].LogicalPath != target.LogicalPath {
			continue
		}
		if plan.Targets[i].Version != target.Version {
			return fmt.Errorf("logical update target %s has conflicting versions %s and %s", target.LogicalPath, plan.Targets[i].Version, target.Version)
		}
		plan.Targets[i].WriteMarker = plan.Targets[i].WriteMarker || target.WriteMarker
		plan.Targets[i].WritePin = plan.Targets[i].WritePin || target.WritePin
		if target.Role == UpdateTargetRepo {
			plan.Targets[i].Role = UpdateTargetRepo
		}
		return nil
	}
	plan.Targets = append(plan.Targets, target)
	return nil
}
