package agentchat

import (
	"context"
	"slices"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func (s *Service) harnessStatuses(ctx context.Context) []HarnessStatus {
	statuses := make([]HarnessStatus, 0, len(s.harnesses))
	for _, kind := range harnessOrder {
		driver := s.harnesses[kind]
		if driver == nil {
			continue
		}
		statuses = append(statuses, HarnessStatus{Kind: kind, Status: statusForAPI(s.harnessStatus(ctx, kind, driver))})
	}
	return statuses
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
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status, err := driver.Status(probeCtx)
	status.Kind = kind
	if err != nil {
		status.LastError = err.Error()
	}
	s.mu.Lock()
	s.status[kind] = statusCacheEntry{status: status, checkedAt: now}
	s.mu.Unlock()
	return status
}

func (s *Service) cachedHarnessStatus(kind harness.Kind) harness.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status[kind].status
}
