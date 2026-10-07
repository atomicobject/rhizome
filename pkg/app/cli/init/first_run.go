// First-run setup: one screen of findings, at most three questions, and a
// quiet summary.
//
// Docs:
// - [[init-starter-workflow#^SPEC-0038-US1]]
// - [[agent-surface-integration-modes#^SPEC-0045-US3]]
package init

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// errSetupCancelled ends a first run that the person declined; nothing is
// written.
var errSetupCancelled = errors.New("setup cancelled")

const (
	searchVoyage = "voyage"
	searchOpenAI = "openai"
	searchOllama = "ollama"
	searchOff    = "off"
)

// installedAgentCommand finds an agent's command on PATH. Tests replace it so
// results do not depend on the machine.
var installedAgentCommand = exec.LookPath

// parseWorkflowOption turns --workflow into starter ids: the three menu
// choices plus explicit starter ids.
func parseWorkflowOption(raw string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none":
		return nil, nil
	case "domain":
		return []string{templateComplexDomain}, nil
	}
	return normalizeTemplateList(raw)
}

func normalizeSearchOption(raw string) (string, error) {
	switch provider := strings.ToLower(strings.TrimSpace(raw)); provider {
	case "", searchVoyage:
		return searchVoyage, nil
	case searchOpenAI, searchOllama, searchOff:
		return provider, nil
	default:
		return "", fmt.Errorf("unknown --search %q; use voyage, openai, ollama, or off", raw)
	}
}

// searchNeed returns the credential a provider needs, if any.
func searchNeed(provider string) (credentials.Need, bool) {
	return credentials.NeedForProvider(provider, "semantic search")
}

// searchReady reports whether a provider can run now: its key resolves, or
// for Ollama, it is installed or current configures a remote server.
func searchReady(provider string, session *credentials.Session, current *embeddings.Config) bool {
	switch provider {
	case searchVoyage, searchOpenAI:
		need, _ := searchNeed(provider)
		return session.Satisfied(need)
	case searchOllama:
		probe := &embeddings.Config{Provider: searchOllama}
		if current != nil && strings.EqualFold(strings.TrimSpace(current.Provider), searchOllama) {
			probe = current
		}
		ok, _ := embeddingsProviderReady(probe)
		return ok
	default:
		return false
	}
}

// applySearchChoice writes semantic search settings: only the provider and
// whether it is on. Models and endpoints come from provider defaults at load
// time, and code search follows the notes setting. Off on purpose is recorded
// so reruns do not offer it again; not ready leaves no setting, so a rerun can
// offer it once a key is available.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US1-AC7]]
func applySearchChoice(cfg *obsidian.LocalConfig, provider string, ready bool) {
	// Keeping the provider keeps its model and endpoint settings.
	if current := cfg.NoteEmbeddings; current != nil && provider != searchOff && (ready || !current.Enabled) &&
		strings.EqualFold(strings.TrimSpace(current.Provider), provider) {
		if ready {
			next := *current
			next.Enabled = true
			cfg.NoteEmbeddings = &next
		}
		return
	}
	cfg.CodeEmbeddings = nil
	switch {
	case provider == searchOff:
		cfg.NoteEmbeddings = &embeddings.Config{Provider: searchVoyage}
	case !ready:
		cfg.NoteEmbeddings = nil
	default:
		cfg.NoteEmbeddings = &embeddings.Config{Enabled: true, Provider: provider}
	}
}

// addInstalledAgents turns on harnesses whose command is installed, and
// stores them as on so later runs do not depend on this machine.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US1]]
func addInstalledAgents(cfg *obsidian.LocalConfig, layout *DetectedLayout) {
	if cfg.Agents == nil {
		cfg.Agents = &obsidian.AgentPreferences{}
	}
	for _, tool := range []struct {
		command string
		enabled *bool
		mode    *string
	}{
		{"claude", &layout.AgentHarnesses.HasClaude, &cfg.Agents.Claude},
		{"codex", &layout.AgentHarnesses.HasCodex, &cfg.Agents.Codex},
		{"cursor", &layout.AgentHarnesses.HasCursor, &cfg.Agents.Cursor},
	} {
		if *tool.enabled || strings.TrimSpace(*tool.mode) == agentModeOff {
			continue
		}
		if _, err := installedAgentCommand(tool.command); err == nil {
			*tool.enabled = true
			*tool.mode = agentModeOn
		}
	}
}

// runFirstRun builds the recommended setup, shows it, and asks only what
// detection cannot decide. Without a terminal it applies the recommendations.
// It returns the label of a key saved during setup, for the summary.
// firstRunOutcome is what a first run decided beyond the setup itself.
type firstRunOutcome struct {
	savedKey string // label of a key saved during setup, for the summary
	provider string // the semantic search provider chosen
	ready    bool   // the provider can run now
}

func (r *initRun) runFirstRun(s *setup, workflowChosen bool) (firstRunOutcome, error) {
	var outcome firstRunOutcome
	out := r.opts.Stdout
	rec := obsidian.LocalConfig{Rhizome: s.cfg.Rhizome}
	applyFirstRunDefaults(&rec, r.layout)
	enableAgentGuidance(&rec, &r.layout)
	if strings.TrimSpace(r.opts.Agents) == "" {
		addInstalledAgents(&rec, &r.layout)
	} else {
		_, prefs, err := applyAgentOverrides(r.layout.AgentHarnesses, rec.Agents, r.opts)
		if err != nil {
			return outcome, err
		}
		rec.Agents = prefs
	}

	provider, err := normalizeSearchOption(r.opts.Search)
	if err != nil {
		return outcome, err
	}
	ready := searchReady(provider, r.session, nil)
	key := strings.TrimSpace(r.opts.SearchKey)
	need, needsKey := searchNeed(provider)
	if key != "" && !needsKey {
		return outcome, fmt.Errorf("--search-key-stdin needs --search voyage or openai")
	}
	applySearchChoice(&rec, provider, ready)
	s.cfg = rec
	keep := readIgnoreFileState(r.layout.ProjectRoot).keep
	for _, p := range s.keep {
		keep[p] = true
	}
	s.skips = mergeSkips(s.skips, detectSkips(r.layout.ProjectRoot, s.cfg.Notes, r.layout.Code.Files, keep))
	// Saving a key exports it to this process's environment, so it waits until
	// detection has run its git child, as a key pasted at the prompt does.
	if key != "" {
		if ready, outcome.savedKey, err = saveKey(r.session, need, key); err != nil {
			return outcome, err
		}
		applySearchChoice(&s.cfg, provider, ready)
	}

	outcome.provider, outcome.ready = provider, ready
	if !r.opts.Interactive {
		// Agents and scripts see the same findings, with the workflow chosen
		// for them.
		printFindings(out, s.cfg, r.layout, r.harnessesFor(s.cfg), s.skips, provider, ready, workflowLabel(s.workflows, nil))
		return outcome, nil
	}

	printFindings(out, s.cfg, r.layout, r.harnessesFor(s.cfg), s.skips, provider, ready, "")
	selected, err := promptIgnoredSubtreeCandidates(&r.layout, r.reader, out, true)
	if err != nil {
		return outcome, err
	}
	if len(selected) > 0 {
		// Show the findings again with the included folders counted.
		s.includeIgnored = dedupePreserveOrder(append(cloneTemplates(s.includeIgnored), selected...))
		mergeDeferredIgnoredCodeSuggestions(&r.layout, selected)
		s.skips = mergeSkips(s.skips, detectSkips(r.layout.ProjectRoot, s.cfg.Notes, r.layout.Code.Files, keep))
		printFindings(out, s.cfg, r.layout, r.harnessesFor(s.cfg), s.skips, provider, ready, "")
	}
	if !workflowChosen {
		s.workflows = promptWorkflow(r.reader, out, nil)
	}
	if provider != searchOff && !ready {
		outcome.provider, outcome.ready, outcome.savedKey, err = promptSearchKey(r.reader, out, provider, r.session)
		if err != nil {
			return outcome, err
		}
		applySearchChoice(&s.cfg, outcome.provider, outcome.ready)
	}

	for {
		fmt.Fprintln(out)
		switch askChoice(r.reader, out, "Set up Rhizome? [Y/n/e to edit]: ", "y", "y", "n", "e") {
		case "n":
			return outcome, errSetupCancelled
		case "e":
			if err := r.editSettings(s); err != nil {
				return outcome, err
			}
			outcome.provider = searchProviderOf(s.cfg)
			outcome.ready = s.cfg.NoteEmbeddings != nil && s.cfg.NoteEmbeddings.Enabled
			printFindings(out, s.cfg, r.layout, r.harnessesFor(s.cfg), s.skips, outcome.provider, outcome.ready, workflowLabel(s.workflows, nil))
			continue
		}
		return outcome, nil
	}
}

// searchHint says how to turn on semantic search when the chosen provider
// cannot run yet, or "" when nothing is needed.
func searchHint(provider string, ready bool) string {
	switch {
	case ready || provider == searchOff:
		return ""
	case provider == searchOllama:
		return "Install Ollama, then run rzm init, to turn on semantic search."
	}
	need, _ := searchNeed(provider)
	return "Set " + need.Key + ", then run rzm init, to turn on semantic search."
}

func searchProviderOf(cfg obsidian.LocalConfig) string {
	if cfg.NoteEmbeddings == nil {
		return searchVoyage
	}
	if !cfg.NoteEmbeddings.Enabled {
		return searchOff
	}
	return strings.ToLower(inferEmbeddingsProvider(cfg.NoteEmbeddings))
}

func noticeSearchNotReady(out io.Writer, provider string, ready bool) {
	if ready || provider == searchOff {
		return
	}
	if provider == searchOllama {
		fmt.Fprintln(out, "Semantic search stays off until Ollama is installed; then run rzm init again.")
		if hint := embeddings.OllamaInstallHint(); hint != "" {
			fmt.Fprintf(out, "  %s\n", styleDim(out, hint))
		}
		return
	}
	need, _ := searchNeed(provider)
	fmt.Fprintf(out, "Semantic search stays off until a key is available: set %s and run rzm init again.\n", need.Key)
}

// applyFirstRunDefaults indexes all Markdown and all supported code.
func applyFirstRunDefaults(cfg *obsidian.LocalConfig, layout DetectedLayout) {
	// Notes cover all Markdown and code covers every supported language, so
	// content added later is indexed; .rhizome/ignore is how things stay out.
	cfg.Notes = obsidian.LocalVaultConfig{Links: layout.SuggestedVault.Links}
	enableAutomaticCodeScope(cfg)
}

// enableAgentGuidance turns on AGENTS.md and .agents/skills, and clears
// harnesses someone turned off, so detection decides them again.
func enableAgentGuidance(cfg *obsidian.LocalConfig, layout *DetectedLayout) {
	prefs := obsidian.AgentPreferences{}
	if cfg.Agents != nil {
		prefs = *cfg.Agents
	}
	prefs.AgentSkills, prefs.AgentsMd = agentModeOn, agentModeOn
	for _, mode := range []*string{&prefs.Cursor, &prefs.Claude, &prefs.Codex} {
		if strings.TrimSpace(*mode) == agentModeOff {
			*mode = ""
		}
	}
	layout.AgentHarnesses.HasAgentSkills = true
	layout.AgentHarnesses.HasAgents = true
	cfg.Agents = &prefs
}
