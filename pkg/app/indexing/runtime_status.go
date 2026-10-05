package indexing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// writeRuntimeStatus reports the vault runtime a client would attach to, or
// says plainly that there is none. An unresponsive runtime is named because it
// is the state a user has to act on.
func writeRuntimeStatus(ctx context.Context, b *strings.Builder, vaultPath string) {
	fmt.Fprintf(b, "Runtime:\n")
	if pid, live := appruntime.LegacyOwner(vaultPath); live {
		fmt.Fprintf(b, "  older Rhizome serve (pid %d) owns this vault; stop it before using the runtime\n", pid)
		return
	}
	_, health, err := appruntime.LiveManifest(ctx, vaultPath)
	if err != nil {
		manifest, readErr := appruntime.ReadManifest(vaultPath)
		if readErr == nil {
			fmt.Fprintf(b, "  none running (stale manifest for pid %d: %v)\n", manifest.PID, err)
			return
		}
		if pid, live := appruntime.LockOwner(vaultPath); live {
			fmt.Fprintf(b, "  none running; %s is held by pid %d without a manifest (a runtime mid-shutdown, or a reused pid: remove the lock if that process is not Rhizome)\n", appruntime.LockPath(vaultPath), pid)
			return
		}
		fmt.Fprintf(b, "  none running\n")
		return
	}
	fmt.Fprintf(b, "  Mode:    %s\n", health.Mode)
	fmt.Fprintf(b, "  PID:     %d\n", health.PID)
	fmt.Fprintf(b, "  Build:   %s (%s)\n", health.BuildID, health.Version)
	fmt.Fprintf(b, "  Ready:   %v\n", health.Ready)
	fmt.Fprintf(b, "  Lane:    %s\n", laneSummary(health.Lane))
	fmt.Fprintf(b, "  Idle:    %s\n", time.Duration(health.IdleSeconds*float64(time.Second)).Round(time.Second))
}

func laneSummary(state appruntime.LaneState) string {
	var b strings.Builder
	if state.Busy {
		fmt.Fprintf(&b, "busy job %s (%s)", state.JobID, state.JobKind)
	} else {
		b.WriteString("idle")
	}
	if state.Queued > 0 {
		fmt.Fprintf(&b, ", queued %d", state.Queued)
	}
	if state.Held != "" {
		fmt.Fprintf(&b, ", held: %s", state.Held)
	}
	if state.LastError != "" {
		fmt.Fprintf(&b, ", last error: %s", state.LastError)
	}
	return b.String()
}

// writeIndexLockStatus reports who holds .rhizome/index.lock. The file's
// modification time is the holder's heartbeat.
func writeIndexLockStatus(b *strings.Builder, vaultPath string) {
	path := obsidian.IndexLockPath(vaultPath)
	fmt.Fprintf(b, "Index lock:\n")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(b, "  not held\n")
		} else {
			fmt.Fprintf(b, "  unreadable: %v\n", err)
		}
		return
	}
	var held indexlock.LockData
	if err := json.Unmarshal(data, &held); err != nil {
		fmt.Fprintf(b, "  held, but the lock file is unparseable: %v\n", err)
		return
	}
	role := held.Role
	if role == "" {
		role = "unknown"
	}
	fmt.Fprintf(b, "  Role:      %s\n", role)
	fmt.Fprintf(b, "  PID:       %d\n", held.PID)
	fmt.Fprintf(b, "  Host:      %s\n", held.Host)
	fmt.Fprintf(b, "  Started:   %s%s\n", held.Started, ageSuffix(parseLockTime(held.Started)))
	if info, statErr := os.Stat(path); statErr == nil {
		fmt.Fprintf(b, "  Heartbeat: %s ago\n", time.Since(info.ModTime()).Round(time.Second))
	}
}

func parseLockTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func ageSuffix(started time.Time) string {
	if started.IsZero() {
		return ""
	}
	return fmt.Sprintf(" (%s ago)", time.Since(started).Round(time.Second))
}
