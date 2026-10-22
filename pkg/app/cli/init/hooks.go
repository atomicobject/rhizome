// Starter hooks: declarations in the built-in starter registry, the
// handlers `rzm hook` runs, the --hooks choice, and what init reports.
//
// Docs:
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC1]]
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC2]]
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC9]]
package init

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

// starterHookMetadata is one hook a starter declares in template.yaml.
type starterHookMetadata struct {
	ID       string   `yaml:"id"`
	Delivers string   `yaml:"delivers"`
	Events   []string `yaml:"events"`
	Tools    []string `yaml:"tools,omitempty"`
	// Command is an argument list relative to the repository root.
	Command []string `yaml:"command"`
	Timeout string   `yaml:"timeout"`
	// CommandPrefixes names the handler's published prefix file, relative to
	// the repository root.
	CommandPrefixes string `yaml:"commandPrefixes,omitempty"`
}

func (m starterHookMetadata) handler(starter string) (hook.Handler, error) {
	fail := func(format string, args ...any) (hook.Handler, error) {
		return hook.Handler{}, fmt.Errorf("starter %q hook %q: %s", starter, m.ID, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(m.ID) == "" || len(m.Command) == 0 || strings.TrimSpace(m.Delivers) == "" {
		return fail("needs an id, a command, and what it delivers")
	}
	if len(m.Events) == 0 {
		return fail("declares no events")
	}
	for _, event := range m.Events {
		if hook.HostEvent(event) == "" {
			return fail("unknown event %q (use %s)", event, strings.Join(hook.HookPoints, ", "))
		}
	}
	for _, tool := range m.Tools {
		switch tool {
		case hook.ToolRead, hook.ToolEdit, hook.ToolShell, hook.ToolPatch:
		default:
			return fail("unknown tool class %q", tool)
		}
	}
	timeout, err := time.ParseDuration(m.Timeout)
	if err != nil || timeout <= 0 || timeout > hook.Deadline {
		return fail("timeout %q must be a duration up to %s", m.Timeout, hook.Deadline)
	}
	return hook.Handler{
		Starter: starter, ID: m.ID, Delivers: m.Delivers, Events: m.Events, Tools: m.Tools,
		Command: m.Command, Timeout: timeout, CommandPrefixes: m.CommandPrefixes,
	}, nil
}

// hooksOn reports whether the person turned hooks on.
func hooksOn(cfg repoconfig.LocalConfig) bool {
	return cfg.Agents != nil && strings.EqualFold(strings.TrimSpace(cfg.Agents.Hooks), agentModeOn)
}

// normalizeHooksOption validates --hooks.
func normalizeHooksOption(raw string) (string, error) {
	switch value := strings.ToLower(strings.TrimSpace(raw)); value {
	case "", agentModeOn, agentModeOff:
		return value, nil
	default:
		return "", fmt.Errorf("invalid --hooks value %q (use on or off)", raw)
	}
}

// applyHooksOption records --hooks in the agent preferences.
func applyHooksOption(cfg *repoconfig.LocalConfig, raw string) {
	value, _ := normalizeHooksOption(raw)
	if value == "" {
		return
	}
	prefs := repoconfig.AgentPreferences{}
	if cfg.Agents != nil {
		prefs = *cfg.Agents
	}
	prefs.Hooks = value
	cfg.Agents = &prefs
}

// handlersFor returns the hook handlers of starters, in starter order.
func handlersFor(starters []string, registry map[string]starterTemplateMetadata) ([]hook.Handler, error) {
	starters = append([]string(nil), starters...)
	sort.Strings(starters)
	var out []hook.Handler
	for _, id := range starters {
		for _, m := range registry[id].Hooks {
			h, err := m.handler(id)
			if err != nil {
				return nil, err
			}
			out = append(out, h)
		}
	}
	return out, nil
}

// hookStarters lists the starters whose handlers run: every active starter,
// ejected ones included, since an ejected starter keeps its handler.
func hookStarters(cfg repoconfig.LocalConfig, registry map[string]starterTemplateMetadata) ([]string, error) {
	set, err := resolveTemplateSet(cfg.WorkflowTemplates, cfg.WorkflowTemplateAddons, registry)
	if err != nil {
		return nil, err
	}
	return set.Effective, nil
}

// HookHandlers returns the handlers `rzm hook` runs for a repository: none
// unless hooks are on, and only those of enabled starters. Declarations come
// only from the registry built into this binary.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC1]]
func HookHandlers(root string) ([]hook.Handler, error) {
	cfg, err := repoconfig.LoadLocalConfig(root)
	if errors.Is(err, repoconfig.ErrNoLocalConfig) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !hooksOn(*cfg) {
		return nil, nil
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil, err
	}
	starters, err := hookStarters(*cfg, registry)
	if err != nil {
		return nil, err
	}
	return handlersFor(starters, registry)
}

// hooksRelevant reports whether a summary shows hooks: an active starter
// declares them, or they are on.
func hooksRelevant(cfg repoconfig.LocalConfig) bool {
	if hooksOn(cfg) {
		return true
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return false
	}
	starters, err := hookStarters(cfg, registry)
	if err != nil {
		return false
	}
	handlers, _ := handlersFor(starters, registry)
	return len(handlers) > 0
}

// hookStatusLines report hooks per harness for the first-run and rerun
// summaries and --check: what each starter delivers, whether the entries
// are installed and current, when each handler last returned context, and
// Codex's trust step. They are empty when no active starter declares hooks
// and no entries are installed.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC9]]
func hookStatusLines(root string, res resolved) []string {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil
	}
	starters, err := hookStarters(res.cfg, registry)
	if err != nil {
		return nil
	}
	handlers, _ := handlersFor(starters, registry)
	var lines []string
	if !hooksOn(res.cfg) && len(handlers) > 0 {
		lines = append(lines, "Hooks are off. Turn them on with rzm init --hooks on to get:")
	}
	for _, h := range handlers {
		name := firstNonEmpty(registry[h.Starter].Name, h.Starter)
		if contains(res.cfg.WorkflowTemplateManagement.Ejected, h.Starter) {
			name += " (ejected; its handler runs your team's files)"
		}
		lines = append(lines, "  "+name+": "+h.Delivers)
	}
	if hooksOn(res.cfg) && res.hooksBlocked != nil {
		lines = append(lines, "Hook entries were left as they are: "+res.hooksBlocked.Error())
	}
	byKey := map[string]*change{}
	for _, c := range res.files.changes {
		byKey[c.key] = c
	}
	status := hook.ReadStatus(root)
	for _, file := range res.files.plan.hookFiles {
		c := byKey[file.key()]
		if c == nil {
			continue
		}
		label := "Claude Code"
		if file.harness == hook.HarnessCodex {
			label = "Codex"
		}
		var state string
		switch {
		case c.removal && c.take:
			state = "Rhizome's entries are removed"
		case c.removal:
			state = "installed; you edited the entries, so they need a decision before removal"
		case c.state == stateCurrent:
			state = "installed and current"
		case c.state == stateMissing:
			state = "not installed yet; installed with these changes"
		case c.state == stateUnedited:
			state = "installed; updated with these changes"
		case c.state == stateLocal || c.state == stateDeclined:
			state = "installed; you edited the entries, and your version is kept"
		default:
			state = "installed; you edited the entries, so the update needs a decision"
		}
		lines = append(lines, fmt.Sprintf("%s hooks (%s): %s", label, file.rel, state))
		if c.removal {
			continue
		}
		for _, h := range handlers {
			if at, ok := status[file.harness][h.Key()]; ok {
				lines = append(lines, fmt.Sprintf("  %s last returned context %s", h.Key(), at.Local().Format("2006-01-02 15:04")))
			} else {
				lines = append(lines, fmt.Sprintf("  %s has not returned context yet", h.Key()))
			}
		}
		if file.harness == hook.HarnessCodex {
			lines = append(lines, "  Codex asks you to trust a new or changed hook entry: review it with /hooks in Codex.")
		}
	}
	return lines
}

// printHookStatus prints hookStatusLines under a Hooks heading.
func printHookStatus(out io.Writer, root string, res resolved) {
	lines := hookStatusLines(root, res)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Hooks"))
	for _, line := range lines {
		fmt.Fprintln(out, "  "+line)
	}
}

// checkHookSupport tells whether the rzm this repository selects can run
// installed entries. It is a variable so tests need no second binary.
var checkHookSupport = selectedBinarySupportsHooks

// selectedBinarySupportsHooks probes the selected rzm with `rzm hook
// --check` unless it is this binary.
func selectedBinarySupportsHooks(root string, cfg repoconfig.LocalConfig) error {
	external, err := cfg.Rhizome.UsesExternalBinaryManager()
	if err != nil {
		return err
	}
	pin := strings.TrimSpace(cfg.Rhizome.Version)
	if !external && strings.TrimSpace(cfg.Rhizome.DevBinaryDir) == "" && (pin == "" || pin == version.Version) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	target := exe
	if external {
		if target, err = exec.LookPath("rzm"); err != nil {
			return fmt.Errorf("no rzm on PATH for the external binary manager")
		}
	} else if plan, err := repoexec.Select(root, exe); err == nil && plan.Target != "" {
		target = plan.Target
	}
	if repoexec.SameExecutablePath(target, exe) {
		return nil
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("the selected rzm (%s) is not installed yet; run rzm init again once it is", target)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := repoexec.Probe(ctx, target, root, "hook", "--check")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(out), "rzm-hook ") {
		return fmt.Errorf("the selected rzm (%s) does not support rzm hook; update it with rzm update", target)
	}
	return nil
}

// planHookFiles decides the hook entries of each harness settings file:
// installed when hooks are on, the harness is enabled, and an active starter
// declares handlers; removed otherwise. When the selected binary cannot run
// them, the files are left as they are and blocked says why.
func planHookFiles(root string, cfg repoconfig.LocalConfig, harnesses AgentHarnesses) (files []generatedHookFile, blocked error, err error) {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil, nil, err
	}
	starters, err := hookStarters(cfg, registry)
	if err != nil {
		return nil, nil, err
	}
	handlers, err := handlersFor(starters, registry)
	if err != nil {
		return nil, nil, err
	}
	want := hooksOn(cfg) && len(handlers) > 0
	var unsupported error
	if want {
		unsupported = checkHookSupport(root, cfg)
	}
	for _, harness := range []string{hook.HarnessClaude, hook.HarnessCodex} {
		enabled := harnesses.HasClaude
		if harness == hook.HarnessCodex {
			enabled = harnesses.HasCodex
		}
		file := generatedHookFile{rel: hookSettingsFiles[harness], harness: harness}
		if want && enabled {
			if unsupported != nil {
				continue
			}
			file.desired = desiredHookEntries(harness)
		}
		files = append(files, file)
	}
	return files, unsupported, nil
}
