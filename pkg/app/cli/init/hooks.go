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
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// starterHookMetadata is one hook a starter declares in template.yaml.
type starterHookMetadata struct {
	ID       string   `yaml:"id"`
	Delivers string   `yaml:"delivers"`
	Events   []string `yaml:"events"`
	Tools    []string `yaml:"tools,omitempty"`
	// Skill names the starter skill whose installed folder {skill} stands
	// for in Command.
	Skill string `yaml:"skill,omitempty"`
	// Command is an argument list relative to the repository root.
	Command []string `yaml:"command"`
	Timeout string   `yaml:"timeout"`
	// CommandPrefixes names the handler's published prefix file, relative to
	// the repository root.
	CommandPrefixes string `yaml:"commandPrefixes,omitempty"`
	// GuardRules declares Rhizome's built-in guard instead of a command: the
	// folder, relative to the repository root, of finding notes whose
	// approved guard rules refuse tool calls before they run.
	GuardRules string `yaml:"guardRules,omitempty"`
}

func (m starterHookMetadata) handler(starter string) (hook.Handler, error) {
	fail := func(format string, args ...any) (hook.Handler, error) {
		return hook.Handler{}, fmt.Errorf("starter %q hook %q: %s", starter, m.ID, fmt.Sprintf(format, args...))
	}
	for _, tool := range m.Tools {
		switch tool {
		case hook.ToolRead, hook.ToolEdit, hook.ToolShell, hook.ToolPatch:
		default:
			return fail("unknown tool class %q", tool)
		}
	}
	if m.GuardRules != "" {
		if strings.TrimSpace(m.ID) == "" || len(m.Command) > 0 || strings.TrimSpace(m.Delivers) == "" {
			return fail("a guard needs an id and what it delivers, and no command")
		}
		if !slices.Equal(m.Events, []string{hook.PreToolUse}) || len(m.Tools) == 0 {
			return fail("a guard handles only %s and names its tool classes", hook.PreToolUse)
		}
		return hook.Handler{Starter: starter, ID: m.ID, Delivers: m.Delivers, Events: m.Events, Tools: m.Tools, GuardRules: m.GuardRules}, nil
	}
	if strings.TrimSpace(m.ID) == "" || len(m.Command) == 0 || strings.TrimSpace(m.Delivers) == "" {
		return fail("needs an id, a command, and what it delivers")
	}
	if len(m.Events) == 0 {
		return fail("declares no events")
	}
	for _, event := range m.Events {
		if !slices.Contains(hook.HandlerEvents, event) {
			return fail("unknown event %q (use %s)", event, strings.Join(hook.HandlerEvents, ", "))
		}
	}
	for _, arg := range m.Command {
		if strings.Contains(arg, skillPlaceholder) && strings.TrimSpace(m.Skill) == "" {
			return fail("uses %s in its command without naming a skill", skillPlaceholder)
		}
	}
	timeout, err := time.ParseDuration(m.Timeout)
	if err != nil || timeout <= 0 || timeout > hook.Deadline {
		return fail("timeout %q must be a duration up to %s", m.Timeout, hook.Deadline)
	}
	return hook.Handler{
		Starter: starter, ID: m.ID, Delivers: m.Delivers, Events: m.Events, Tools: m.Tools,
		Skill: m.Skill, Command: m.Command, Timeout: timeout, CommandPrefixes: m.CommandPrefixes,
	}, nil
}

// skillPlaceholder in a handler command stands for the folder its skill is
// installed in.
const skillPlaceholder = "{skill}"

// skillDir is the folder a starter skill is installed in: the first enabled
// skill folder holding it, .agents/skills before .claude/skills, as init
// writes them. A folder the person turned off is skipped even if an old copy
// remains there, so ok is false when no enabled folder holds the skill.
func skillDir(root string, prefs *repoconfig.AgentPreferences, skill string) (dir string, ok bool) {
	for _, folder := range enabledSkillFolders(prefs) {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(folder), skill)); err == nil && info.IsDir() {
			return folder + "/" + skill, true
		}
	}
	return "", false
}

func enabledSkillFolders(prefs *repoconfig.AgentPreferences) []string {
	var folders []string
	if prefs == nil || !strings.EqualFold(prefs.AgentSkills, agentModeOff) {
		folders = append(folders, ".agents/skills")
	}
	if prefs == nil || !strings.EqualFold(prefs.Claude, agentModeOff) {
		folders = append(folders, ".claude/skills")
	}
	return folders
}

// resolveSkillCommand points a handler command at the folder its skill is
// installed in, or reports false when no enabled folder holds it.
func resolveSkillCommand(root string, prefs *repoconfig.AgentPreferences, h hook.Handler) ([]string, bool) {
	if h.Skill == "" {
		return h.Command, true
	}
	dir, ok := skillDir(root, prefs, h.Skill)
	if !ok {
		return nil, false
	}
	out := make([]string, len(h.Command))
	for i, arg := range h.Command {
		out[i] = strings.ReplaceAll(arg, skillPlaceholder, dir)
	}
	return out, true
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
// and every ejected one, including those retained after leaving the
// selection, since an ejected starter keeps its files and its handler.
func hookStarters(cfg repoconfig.LocalConfig, registry map[string]starterTemplateMetadata) ([]string, error) {
	set, err := resolveTemplateSet(cfg.WorkflowTemplates, cfg.WorkflowTemplateAddons, registry)
	if err != nil {
		return nil, err
	}
	return append(set.Effective, frozenStarters(cfg.WorkflowTemplateManagement.Ejected, set.Effective)...), nil
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
	handlers, err := handlersFor(starters, registry)
	if err != nil {
		return nil, err
	}
	// A handler whose skill no enabled folder holds does not run; status
	// says so.
	var out []hook.Handler
	for _, h := range handlers {
		if command, ok := resolveSkillCommand(root, cfg.Agents, h); ok {
			h.Command = command
			out = append(out, h)
		}
	}
	return out, nil
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
func hookStatusLines(root string, res resolved, applied bool) []string {
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
	if reason := hooksBlockedReason(res); errors.Is(res.hooksBlocked, errNoGuard) {
		lines = append(lines, "Guard rules do not run: "+reason)
	} else if reason != "" {
		lines = append(lines, "Hook entries were left as they are: "+reason)
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
		case applied && c.take && c.removal:
			state = "Rhizome's entries were removed"
		case applied && c.take:
			state = "installed and current"
		case c.removal && c.take:
			state = "Rhizome's entries will be removed with these changes"
		case c.state == stateUnknown && c.removal:
			state = "Rhizome has no record of writing these entries, so removing them needs a decision"
		case c.state == stateUnknown:
			state = "Rhizome has no record of writing these entries, so replacing them needs a decision"
		case c.removal:
			state = "installed; you edited the entries, so they need a decision before removal"
		case c.state == stateCurrent:
			state = "installed and current"
		case c.state == stateMissing:
			state = "not installed yet; these changes install them"
		case c.state == stateUnedited:
			state = "installed; these changes update them"
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
			if h.GuardRules != "" {
				lines = append(lines, guardStatusLines(root, h, status.Refused[file.harness])...)
			} else if hooksOn(res.cfg) && !skillAvailable(root, res, applied, h.Skill) {
				lines = append(lines, fmt.Sprintf("  %s does not run: no enabled skill folder holds its %s skill", h.Key(), h.Skill))
			} else if at, ok := status.Delivered[file.harness][h.Key()]; ok {
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

// hooksBlockedReason says why hook entries are left as they are though
// hooks are on, or "" when nothing blocks them.
func hooksBlockedReason(res resolved) string {
	if !hooksOn(res.cfg) || res.hooksBlocked == nil {
		return ""
	}
	return res.hooksBlocked.Error()
}

// guardStatusLines list a guard's active rules for one harness and how many
// calls each refused there, and the rules it skips.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6-AC5]]
func guardStatusLines(root string, h hook.Handler, refused map[string]int) []string {
	rules, problems := hook.LoadGuardRules(context.Background(), root, h.GuardRules)
	var lines []string
	if len(rules) == 0 {
		lines = append(lines, fmt.Sprintf("  %s has no approved guard rules in %s; the before-tool-call entry is installed once one exists", h.Key(), h.GuardRules))
	}
	for _, r := range rules {
		n := refused[r.Source]
		lines = append(lines, fmt.Sprintf("  guard rule %s (%s) refused %s", r.Source, r.Match(), countNoun(n, "call")))
	}
	for _, p := range problems {
		lines = append(lines, "  guard rule skipped: "+p)
	}
	return lines
}

// skillAvailable reports whether a handler's skill is in an enabled skill
// folder, or, before res is applied, will be once its changes are written.
func skillAvailable(root string, res resolved, applied bool, skill string) bool {
	if skill == "" {
		return true
	}
	if _, ok := skillDir(root, res.cfg.Agents, skill); ok {
		return true
	}
	if applied {
		return false
	}
	for _, c := range res.files.changes {
		for _, folder := range enabledSkillFolders(res.cfg.Agents) {
			if !c.removal && strings.HasPrefix(filepath.ToSlash(c.rel), folder+"/"+skill+"/") {
				return true
			}
		}
	}
	return false
}

// printHookStatus prints hookStatusLines under a Hooks heading, before or
// after res is applied.
func printHookStatus(out io.Writer, root string, res resolved, applied bool) {
	lines := hookStatusLines(root, res, applied)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Hooks"))
	for _, line := range lines {
		fmt.Fprintln(out, "  "+line)
	}
}

// planHookFiles decides the hook entries of each harness settings file:
// installed when hooks are on, the harness is enabled, and an active starter
// declares handlers; removed otherwise. The before-tool-call entry is
// installed only while a guard declaration has an approved rule and the
// binary that runs it applies guard rules. When the selected binary cannot
// run the entries, the files are left as they are; blocked says why entries
// were left out.
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
	guard := want && guarded(root, handlers)
	var unsupported error
	if want {
		unsupported = checkHookSupport(root, cfg)
	}
	// A binary without guard rules still runs the other entries; only a
	// needed before-tool-call entry makes its absence worth reporting.
	if errors.Is(unsupported, errNoGuard) {
		if guard {
			blocked = unsupported
		}
		guard, unsupported = false, nil
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
			file.desired = desiredHookEntries(harness, guard)
		}
		files = append(files, file)
	}
	if unsupported != nil {
		blocked = unsupported
	}
	return files, blocked, nil
}
