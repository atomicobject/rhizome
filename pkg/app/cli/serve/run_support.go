package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func resolveVault(ctx context.Context, name string) (obsidian.VaultDefinition, string, error) {
	if name == "" {
		defaultName, err := (&repoconfig.Vault{Env: vaultconfig.FromContext(ctx)}).DefaultName()
		if err != nil {
			return obsidian.VaultDefinition{}, "", err
		}
		name = defaultName
	}
	def, err := (&repoconfig.Vault{Env: vaultconfig.FromContext(ctx), Name: name}).Definition()
	if err != nil {
		return obsidian.VaultDefinition{}, "", err
	}
	return def, name, nil
}

var (
	buildIDOnce  sync.Once
	buildIDValue string
)

func resolveIdleTimeout(cfg *repoconfig.LocalConfig, override time.Duration) (time.Duration, error) {
	if override > 0 {
		return override, nil
	}
	return appruntime.IdleTimeout(cfg)
}

func bindListener(vaultPath string, opts Options) (net.Listener, error) {
	listen := opts.Listen
	if listen == nil {
		listen = net.Listen
	}
	port := ResolvePort(vaultPath, opts.Port, opts.ReuseDiscoveryPort)
	allowFallback := opts.Port == 0 && opts.ReuseDiscoveryPort
	listener, err := listen("tcp", fmt.Sprintf("%s:%d", opts.Host, port))
	if err == nil {
		return listener, nil
	}
	if !allowFallback || port == 0 || !IsAddrInUse(err) {
		return nil, err
	}
	return listen("tcp", fmt.Sprintf("%s:0", opts.Host))
}

func heartbeatRegistry(ctx context.Context, registry *appruntime.Registry, state *manifestState) {
	if registry == nil {
		return
	}
	ticker := time.NewTicker(registryHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = registry.Write(state.Snapshot())
		}
	}
}

func waitForReady(ctx context.Context, out io.Writer, registry *appruntime.Registry, rt *bootstrap.LiveRuntime, state *manifestState, fail func(reason string)) {
	// A vault with embeddings turned off is fully usable; only a semantic
	// capability that was asked for and failed blocks readiness.
	if err := rt.WaitForSemantic(ctx); err != nil && !errors.Is(err, bootstrap.ErrEmbeddingsDisabled) {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(out, "Rhizome readiness blocked\n  stage: semantic\n  error: %v\n", err)
		}
		return
	}
	if err := rt.WaitForCodeIndex(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(out, "Rhizome readiness blocked\n  stage: code-index\n  error: %v\n", err)
		}
		return
	}
	manifest := state.SetReady(true)
	if registry != nil {
		_ = registry.Write(manifest)
	}
	// The Ready flag is informational (health serves readiness live), and on
	// Windows a rewrite can lose a sharing race with a concurrent reader, so
	// retry briefly and then warn rather than stop a working runtime.
	var writeErr error
	for attempt := 0; attempt < 5; attempt++ {
		if writeErr = appruntime.WriteManifest(rt.VaultPath, manifest); writeErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if writeErr != nil {
		fmt.Fprintf(out, "warning: could not record readiness in the runtime manifest: %v\n", writeErr)
	}
	fmt.Fprintf(out, "Rhizome ready\n  url:   %s\n", manifest.HTTPURL)
}

// ServeURL renders the address a user can open.
func ServeURL(host string, port int) string {
	return "http://" + net.JoinHostPort(DisplayHost(host), strconv.Itoa(port))
}

// DisplayHost turns a wildcard bind address into one a browser can reach.
func DisplayHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

// ResolvePort reuses the last port this worktree served on, so a restarted
// runtime keeps its URL stable for open browser tabs.
func ResolvePort(vaultPath string, requestedPort int, reuseDiscoveryPort bool) int {
	if requestedPort != 0 || !reuseDiscoveryPort {
		return requestedPort
	}
	return appruntime.ReadLastPort(vaultPath)
}

// IsAddrInUse reports whether a listen error means the port is taken.
func IsAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
