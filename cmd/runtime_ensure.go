package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

// vaultRuntimeAutostart resolves whether this invocation may start a headless
// runtime (RZM_RUNTIME_AUTOSTART, then `runtime.autostart`, then true).
func vaultRuntimeAutostart(vaultPath string) bool {
	cfg, err := obsidian.LoadLocalConfig(vaultPath)
	if err != nil {
		cfg = nil
	}
	return appruntime.AutostartEnabled(cfg)
}

// vaultRuntimeEnsureOptions describes how this command finds or starts the
// vault's runtime. out receives short progress lines; nil discards them.
func vaultRuntimeEnsureOptions(vaultPath string, wait bool, out io.Writer) (appruntime.EnsureOptions, error) {
	executable, err := os.Executable()
	if err != nil {
		return appruntime.EnsureOptions{}, fmt.Errorf("resolve executable for vault runtime: %w", err)
	}
	logf := func(string, ...any) {}
	if out != nil {
		logf = func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	}
	return appruntime.EnsureOptions{
		VaultPath:  vaultPath,
		Executable: executable,
		BuildID:    appruntime.BuildID(version.Version, executable),
		Autostart:  vaultRuntimeAutostart(vaultPath),
		Wait:       wait,
		Logf:       logf,
	}, nil
}

// ensureVaultRuntime attaches to the vault's runtime, starting a headless one
// when auto-start allows it. With wait false it returns as soon as the spawn is
// under way and the client is nil.
func ensureVaultRuntime(ctx context.Context, vaultDef obsidian.VaultDefinition, wait bool, out io.Writer) (*appruntime.Client, appruntime.Health, error) {
	opts, err := vaultRuntimeEnsureOptions(vaultDef.BasePath(), wait, out)
	if err != nil {
		return nil, appruntime.Health{}, err
	}
	result, err := appruntime.Ensure(ctx, opts)
	if err != nil {
		return nil, appruntime.Health{}, err
	}
	return result.Client, result.Health, nil
}

// backgroundEnsure is the work startVaultRuntimeInBackground hands to a
// goroutine; tests replace it to prove the caller never waits.
var backgroundEnsure = func(ctx context.Context, vaultDef obsidian.VaultDefinition) {
	// Readers only need a runtime to exist eventually, so readiness is never
	// awaited and every failure is silent: this must not change what the
	// command prints or how fast it answers.
	_, _, _ = ensureVaultRuntime(ctx, vaultDef, false, nil)
}

// startVaultRuntimeInBackground triggers ensure without blocking. Read-only
// commands (`rzm agent start`, `rzm mcp serve`) use it so the vault keeps a
// runtime warm without paying for it. The vault definition is resolved on the
// caller's goroutine: resolution reads process-wide configuration state that
// the command (and its tests) may change once it returns, and it is cheap.
func startVaultRuntimeInBackground(ctx context.Context, resolve func(context.Context) (obsidian.VaultDefinition, error)) {
	vaultDef, err := resolve(ctx)
	if err != nil {
		return
	}
	// Detached from the command's context: the ensure outlives a short command.
	go backgroundEnsure(context.WithoutCancel(ctx), vaultDef)
}
