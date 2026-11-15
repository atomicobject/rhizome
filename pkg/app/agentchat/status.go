package agentchat

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

const statusProbeTimeout = 5 * time.Second

// Report loads the person's chat settings and probes each harness, for callers
// without a running Service such as the desktop bridge.
func Report(ctx context.Context, env vaultconfig.Env, harnesses map[harness.Kind]harness.Harness) (SettingsResponse, error) {
	settings, err := LoadSettings(env)
	if err != nil {
		return SettingsResponse{}, err
	}
	return settingsResponse(settings, probeStatuses(harnesses, func(kind harness.Kind, driver harness.Harness) harness.Status {
		return probeStatus(ctx, kind, driver)
	})), nil
}

func settingsResponse(settings Settings, statuses []HarnessStatus) SettingsResponse {
	return SettingsResponse{Settings: settings, Resolved: resolveHarness(settings, statuses), Harnesses: statuses}
}

// probeStatuses probes every harness concurrently and reports them in
// harnessOrder.
func probeStatuses(harnesses map[harness.Kind]harness.Harness, probe func(harness.Kind, harness.Harness) harness.Status) []HarnessStatus {
	statuses := make([]HarnessStatus, 0, len(harnesses))
	for _, kind := range harnessOrder {
		if harnesses[kind] != nil {
			statuses = append(statuses, HarnessStatus{Kind: kind})
		}
	}
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() {
			kind := statuses[i].Kind
			statuses[i].Status = statusForAPI(probe(kind, harnesses[kind]))
		})
	}
	wg.Wait()
	return statuses
}

func probeStatus(ctx context.Context, kind harness.Kind, driver harness.Harness) harness.Status {
	ctx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
	defer cancel()
	status, err := driver.Status(ctx)
	status.Kind = kind
	if err != nil {
		status.LastError = err.Error()
	}
	return status
}

func (s *Service) harnessStatuses(ctx context.Context) []HarnessStatus {
	return probeStatuses(s.harnesses, func(kind harness.Kind, driver harness.Harness) harness.Status {
		return s.harnessStatus(ctx, kind, driver)
	})
}

func statusForAPI(status harness.Status) Status {
	models := make([]harness.ModelOption, 0, len(status.Models))
	effortSet := make(map[string]struct{})
	for _, model := range status.Models {
		models = append(models, model)
		for _, effort := range model.Efforts {
			effortSet[effort] = struct{}{}
		}
	}
	efforts := make([]string, 0, len(effortSet))
	for effort := range effortSet {
		efforts = append(efforts, effort)
	}
	slices.Sort(efforts)
	return Status{
		Installed: status.Installed, Version: status.Version, LoggedIn: status.LoggedIn,
		Account: status.Account, Models: models, Efforts: efforts, Capabilities: status.Capabilities,
		LastError: status.LastError, LoginHint: status.LoginHint,
	}
}

func (s *Service) harnessStatus(ctx context.Context, kind harness.Kind, driver harness.Harness) harness.Status {
	now := time.Now()
	s.mu.Lock()
	cached, ok := s.status[kind]
	s.mu.Unlock()
	if ok && now.Sub(cached.checkedAt) < statusCacheTTL {
		return cached.status
	}
	// The probe's result is cached for every caller, so one caller leaving,
	// such as a page that reloads mid-request, must not cancel it into a
	// cached failure that hides an installed harness.
	status := probeStatus(context.WithoutCancel(ctx), kind, driver)
	s.mu.Lock()
	s.status[kind] = statusCacheEntry{status: status, checkedAt: now}
	s.mu.Unlock()
	return status
}

// ForgetHarnessStatus drops every cached probe, so the next read probes again.
func (s *Service) ForgetHarnessStatus() {
	s.mu.Lock()
	clear(s.status)
	s.mu.Unlock()
}

func (s *Service) cachedHarnessStatus(kind harness.Kind) harness.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status[kind].status
}
