package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
)

// Runtime states. Starting means a live process holds the worktree's runtime
// but it has not verified yet: it is booting, busy, shutting down, or being
// replaced. Callers must not start another runtime while one is starting.
const (
	StateRunning  = "running"
	StateStarting = "starting"
	StateStopped  = "stopped"
)

// statusTimeout bounds each worktree's health probe so one wedged runtime
// cannot delay the rest of the app's presence view.
const statusTimeout = time.Second

type RepositoryRef struct {
	Folder  string `json:"folder"`
	Primary string `json:"primary,omitempty"`
}

// RuntimeStatus never carries a control token. URL is present only for a
// running runtime whose identity and loopback origin were verified.
type RuntimeStatus struct {
	State string `json:"state"`
	Mode  string `json:"mode,omitempty"`
	Ready bool   `json:"ready,omitempty"`
	PID   int    `json:"pid,omitempty"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

type Discovered struct {
	Repository *Repository `json:"repository,omitempty"`
	Error      *Problem    `json:"error,omitempty"`
}

type StatusResult struct {
	// Repositories follows the request order.
	Repositories []Discovered `json:"repositories,omitempty"`
	// Runtimes is keyed by the requested folder paths and every discovered worktree.
	Runtimes map[string]RuntimeStatus `json:"runtimes"`
}

// Status discovers the requested repositories, then probes every requested
// folder and discovered worktree concurrently. It reads files and probes
// loopback health endpoints only; it never runs repository code.
func (s *Service) Status(ctx context.Context, req Request) (StatusResult, error) {
	result := StatusResult{Runtimes: map[string]RuntimeStatus{}}
	paths := append([]string(nil), req.Folders...)
	for _, ref := range req.Repositories {
		repo, err := s.Repository(ref.Folder, ref.Primary)
		if err != nil {
			result.Repositories = append(result.Repositories, Discovered{Error: asProblem(err)})
			continue
		}
		result.Repositories = append(result.Repositories, Discovered{Repository: &repo})
		for _, w := range repo.Worktrees {
			paths = append(paths, w.Path)
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	statuses := make([]RuntimeStatus, len(paths))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = s.runtimeStatus(ctx, path)
		}()
	}
	wg.Wait()
	for i, path := range paths {
		result.Runtimes[path] = statuses[i]
	}
	return result, nil
}

func (s *Service) runtimeStatus(ctx context.Context, folder string) RuntimeStatus {
	stopped := RuntimeStatus{State: StateStopped}
	if !filepath.IsAbs(folder) {
		stopped.Error = "Choose an absolute folder path."
		return stopped
	}
	if info, err := s.Inspect(folder); err == nil {
		folder = info.Path
	}
	if manifest, err := appruntime.ReadManifest(folder); err == nil {
		if _, err := runtimeOrigin(manifest.HTTPURL); err != nil {
			stopped.Error = err.Error()
			return stopped
		}
		if !repoexec.SamePath(manifest.VaultPath, folder) {
			stopped.Error = "The runtime manifest belongs to another folder."
			return stopped
		}
		ctx, cancel := context.WithTimeout(ctx, statusTimeout)
		defer cancel()
		client, health, err := appruntime.LiveManifest(ctx, folder)
		if err == nil {
			opened, err := openResult(folder, appruntime.EnsureResult{Client: client, Health: health})
			if err != nil {
				stopped.Error = err.Error()
				return stopped
			}
			return RuntimeStatus{State: StateRunning, Mode: opened.Mode, Ready: health.Ready, PID: opened.PID, URL: opened.URL}
		}
		if errors.Is(err, appruntime.ErrRuntimeUnresponsive) {
			return RuntimeStatus{State: StateStarting}
		}
	}
	if _, live := appruntime.LockOwner(folder); live {
		return RuntimeStatus{State: StateStarting}
	}
	return stopped
}
