package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
)

type SeedResult struct {
	Seeded bool   `json:"seeded"`
	Reason string `json:"reason,omitempty"`
}

const seedTimeout = 20 * time.Minute

// Seed copies the primary worktree's database into a worktree that has none by
// running the worktree's own selected executable with `new-worktree <primary>`.
func (s *Service) Seed(ctx context.Context, req Request) (SeedResult, error) {
	info, plan, err := s.inspect(req.Folder)
	if err != nil {
		return SeedResult{}, err
	}
	if !info.Configured {
		return SeedResult{}, problem("not_configured", "This worktree needs Rhizome setup before it can open.")
	}
	if info.TrustRequired {
		return SeedResult{}, problem("trust_required", "Confirm trust for this worktree before running its selected Rhizome executable.")
	}
	primary, err := repositorytrust.CanonicalCheckout(req.Primary)
	if err != nil || !filepath.IsAbs(req.Primary) {
		return SeedResult{}, problem("folder_missing", "The primary worktree is unavailable. Choose another primary worktree or start without copying.")
	}
	_, hasDatabase := databasePath(info.Path)
	_, primaryHasDatabase := databasePath(primary)
	switch {
	case primary == info.Path:
		return SeedResult{Reason: "primary"}, nil
	case hasDatabase:
		return SeedResult{Reason: "exists"}, nil
	case !primaryHasDatabase:
		return SeedResult{Reason: "primary_unindexed"}, nil
	}
	target, err := s.prepare(ctx, info, plan, req)
	if err != nil {
		return SeedResult{}, err
	}
	if _, err := repoexec.Probe(ctx, target, info.Path, "new-worktree", "--help"); err != nil {
		return SeedResult{}, problem("seed_unsupported", "This worktree's Rhizome version cannot copy another worktree's index.")
	}
	ctx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, target, "new-worktree", primary)
	cmd.Dir = info.Path
	cmd.Env = append(os.Environ(), "RZM_REPO_DELEGATED=1", "RZM_SKIP_REPO_DELEGATE=1")
	cmd.WaitDelay = time.Second
	stderr := &tail{limit: 4096}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		message := fmt.Sprintf("Copying the index from %s failed: %v", primary, err)
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			message += "\n" + detail
		}
		return SeedResult{}, problem("seed_error", message)
	}
	return SeedResult{Seeded: true}, nil
}

// tail keeps the last limit bytes written to it.
type tail struct {
	data  []byte
	limit int
}

func (t *tail) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > t.limit {
		t.data = t.data[len(t.data)-t.limit:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.data) }
