package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/atomicobject/rhizome/pkg/logging"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// MachineSettings is how this repository's tasks run on this machine for
// the person (SPEC-0127 US10), from the user configuration file.
type MachineSettings struct {
	CommonDir string `json:"commonDir"`
	// Timezone is the person-wide default schedule zone, or "".
	Timezone   string                           `json:"timezone,omitempty"`
	Repository repoconfig.TasksRepositoryConfig `json:"repository"`
}

// errNoUserConfig means the Service was built without a user
// configuration, as tests are.
var errNoUserConfig = errors.New("no user configuration is available")

// MachineSettings reads this repository's settings on this machine. A
// Service without a user configuration has none: tasks do not run here.
func (s *Service) MachineSettings() (MachineSettings, error) {
	out := MachineSettings{CommonDir: s.opts.Repo.CommonDir}
	if s.opts.Env.IsZero() {
		return out, nil
	}
	cfg, err := repoconfig.LoadTasksUserConfig(s.opts.Env)
	if err != nil {
		return out, err
	}
	out.Timezone, out.Repository = cfg.Timezone, cfg.Repositories[s.opts.Repo.CommonDir]
	return out, nil
}

// SetRunHere turns scheduled task runs on or off for this repository on
// this machine.
func (s *Service) SetRunHere(ctx context.Context, on bool) error {
	return s.updateMachine(func(r *repoconfig.TasksRepositoryConfig) error {
		r.RunHere = on
		return nil
	})
}

// SetHostCheckout registers the checkout tasks run from on this machine.
// It must be a checkout of this repository; "" means the main checkout.
func (s *Service) SetHostCheckout(ctx context.Context, checkout string) error {
	if checkout != "" {
		top, err := gitOutput(ctx, checkout, "rev-parse", "--show-toplevel")
		if err != nil {
			return fmt.Errorf("%s is not a Git checkout: %w", checkout, err)
		}
		repo, err := OpenRepo(ctx, top)
		if err != nil {
			return err
		}
		if repo.CommonDir != s.opts.Repo.CommonDir {
			return fmt.Errorf("%s is a checkout of a different repository", checkout)
		}
		if resolved, err := filepath.EvalSymlinks(top); err == nil {
			top = resolved
		}
		if top == s.opts.Repo.MainCheckout {
			top = ""
		}
		checkout = top
	}
	return s.updateMachine(func(r *repoconfig.TasksRepositoryConfig) error {
		r.Checkout = checkout
		return nil
	})
}

func (s *Service) updateMachine(edit func(*repoconfig.TasksRepositoryConfig) error) error {
	if s.opts.Env.IsZero() {
		return errNoUserConfig
	}
	return repoconfig.UpdateTasksRepository(s.opts.Env, s.opts.Repo.CommonDir, edit)
}

// personZone is the person's schedule zone, or nil for the host's. An
// unknown zone falls back to the host's, with a warning for status.
func (s *Service) personZone(m MachineSettings) (*time.Location, string) {
	if m.Timezone == "" {
		return nil, ""
	}
	loc, err := time.LoadLocation(m.Timezone)
	if err != nil {
		return nil, fmt.Sprintf("tasks.timezone %q in your user configuration is not a known IANA zone name; schedules use this machine's zone", m.Timezone)
	}
	return loc, ""
}

// taskAgent is the person's harness, model, and effort for one task.
func taskAgent(env vaultconfig.Env, commonDir, task string) (repoconfig.TaskAgentConfig, error) {
	if env.IsZero() {
		return repoconfig.TaskAgentConfig{}, nil
	}
	cfg, err := repoconfig.LoadTasksUserConfig(env)
	return cfg.Repositories[commonDir].Tasks[task], err
}

// Host is a repository whose scheduled tasks run on this machine.
type Host struct {
	CommonDir string `json:"commonDir"`
	Checkout  string `json:"checkout"`
	RunHere   bool   `json:"runHere"`
}

// Hosts lists the repositories with runHere on, for the Desktop host loop.
// A repository without a registered checkout runs from its main checkout.
func Hosts(env vaultconfig.Env) ([]Host, error) {
	cfg, err := repoconfig.LoadTasksUserConfig(env)
	if err != nil {
		return nil, err
	}
	hosts := []Host{}
	for common, r := range cfg.Repositories {
		if !r.RunHere {
			continue
		}
		checkout := r.Checkout
		if checkout == "" && filepath.Base(common) == ".git" {
			checkout = filepath.Dir(common)
		}
		hosts = append(hosts, Host{CommonDir: common, Checkout: checkout, RunHere: true})
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].CommonDir < hosts[j].CommonDir })
	return hosts, nil
}

const (
	runDueLogFile = "run-due.log"
	maxRunDueLog  = 1 << 20
)

// OpenRunDueLog opens the task-state log that detached `rzm tasks run-due`
// processes write their output to, trimmed to its last 1 MB first.
func OpenRunDueLog(ctx context.Context, dir string) (*os.File, error) {
	repo, err := OpenRepo(ctx, dir)
	if err != nil {
		return nil, err
	}
	stateDir, err := repo.StateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(stateDir, runDueLogFile)
	if err := logging.TrimOutputTail(path, maxRunDueLog); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
