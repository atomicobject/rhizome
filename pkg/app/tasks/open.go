package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// OpenOptions are the inputs Open composes a Service from.
type OpenOptions struct {
	// Dir is any checkout of the repository, or a folder inside one.
	Dir string
	// Checkout identifies the selected checkout; defaults to Dir.
	Checkout string
	// Drivers are the person's harnesses; Settings reads their agent
	// settings. Together they run prompts.
	Drivers  map[harness.Kind]harness.Harness
	Settings func() (agentchat.Settings, error)
	// Trusted reports whether the person trusted a checkout. It is required.
	Trusted func(checkout string) (bool, error)
	// Rzm runs code-mode check scripts; empty means this executable.
	Rzm string
	Now func() time.Time
}

// Open composes the Service for the repository holding opts.Dir, reading
// its tasks configuration from the main checkout's .rhizome/config.yml.
func Open(ctx context.Context, opts OpenOptions) (*Service, error) {
	if opts.Trusted == nil {
		return nil, errors.New("tasks.Open needs a trust check")
	}
	repo, err := OpenRepo(ctx, opts.Dir)
	if err != nil {
		return nil, err
	}
	cfg := repoconfig.LocalTasksConfig{}
	configRoot := repo.MainCheckout
	if configRoot == "" {
		configRoot = opts.Dir
	}
	local, err := repoconfig.LoadLocalConfig(configRoot)
	switch {
	case errors.Is(err, repoconfig.ErrNoLocalConfig):
	case err != nil:
		return nil, fmt.Errorf("read .rhizome/config.yml: %w", err)
	case local.Tasks != nil:
		cfg = *local.Tasks
	}
	if opts.Rzm == "" {
		if opts.Rzm, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	checkout := opts.Checkout
	if checkout == "" {
		checkout = opts.Dir
	}
	checkout, err = gitOutput(ctx, checkout, "rev-parse", "--show-toplevel")
	if err != nil {
		if repo.MainCheckout != "" || opts.Checkout != "" {
			return nil, err
		}
		checkout = "" // Bare repositories can still run their primary-ref tasks.
	}
	if checkout != "" {
		selected, err := OpenRepo(ctx, checkout)
		if err != nil {
			return nil, err
		}
		if selected.CommonDir != repo.CommonDir {
			return nil, errors.New("selected checkout belongs to a different repository")
		}
	}
	return New(Options{
		Checkout: checkout,
		Repo:     repo,
		Config:   cfg,
		Rzm:      opts.Rzm,
		Trusted:  opts.Trusted,
		Agent:    HarnessAgent{Drivers: opts.Drivers, Settings: opts.Settings},
		Now:      opts.Now,
	})
}
