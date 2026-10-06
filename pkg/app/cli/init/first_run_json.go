// Machine-readable first runs: `rzm init --check --json` reports what a
// first run would do and the choices it offers, and `rzm init --json`
// applies the choices it is given. The desktop app's setup sheet is built
// from these documents. Display text comes from the same functions the
// terminal prints with.
//
// Docs: [[desktop-repository-setup#^SPEC-0118-US1]]
package init

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// SetupSchema versions the JSON documents.
const SetupSchema = 1

// SetupPlan is what a first run would do with the choices it was given,
// and every choice it offers.
type SetupPlan struct {
	Schema     int           `json:"schema"`
	Root       string        `json:"root"`
	Name       string        `json:"name"`
	Configured bool          `json:"configured"`
	Findings   SetupFindings `json:"findings"`
	// Workflow is the id of the chosen workflow.
	Workflow            string              `json:"workflow"`
	Workflows           []WorkflowOption    `json:"workflows"`
	Addons              []AddonOption       `json:"addons"`
	Agents              []AgentOption       `json:"agents"`
	Search              SearchOptions       `json:"search"`
	IgnoredRepositories []IgnoredRepository `json:"ignoredRepositories"`
	// Pin is the Rhizome version setup pins, or "" when none is pinned.
	Pin    string      `json:"pin"`
	Scope  Scope       `json:"scope"`
	Writes SetupWrites `json:"writes"`
}

// SetupFindings are the findings rows the terminal prints.
type SetupFindings struct {
	Docs string `json:"docs"`
	Code string `json:"code"`
	Skip string `json:"skip,omitempty"`
}

type WorkflowOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended"`
}

// AddonOption is a starter that workflows can activate by default.
type AddonOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// DefaultFor lists the workflow ids that activate it by default.
	DefaultFor []string `json:"defaultFor"`
	Enabled    bool     `json:"enabled"`
}

type AgentOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	// Detected says why init recommends it: marker (the repository has its
	// files) or installed (its command is on PATH).
	Detected string `json:"detected,omitempty"`
}

type SearchOptions struct {
	// Provider is the chosen provider, and Ready whether semantic search
	// will be on: false for off and for a provider that needs a key.
	Provider  string           `json:"provider"`
	Ready     bool             `json:"ready"`
	Hint      string           `json:"hint,omitempty"`
	Providers []SearchProvider `json:"providers"`
}

type SearchProvider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Ready bool   `json:"ready"`
	// Key names the environment variable the provider's key is saved under,
	// and KeyLabel describes it.
	Key      string `json:"key,omitempty"`
	KeyLabel string `json:"keyLabel,omitempty"`
	// TeamKey is true when the Atomic Object Rhizome key also unlocks it.
	TeamKey bool   `json:"teamKey,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// IgnoredRepository is a nested repository .gitignore excludes that init
// offers to index.
type IgnoredRepository struct {
	Path     string `json:"path"`
	Included bool   `json:"included"`
	Source   string `json:"source,omitempty"`
	Line     int    `json:"line,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
}

// SetupWrites summarizes the writes, grouped as the terminal summary groups
// them, and lists every generated file.
type SetupWrites struct {
	Summary []string `json:"summary"`
	Files   []string `json:"files"`
}

// SetupResult is what a first run wrote.
type SetupResult struct {
	Schema   int      `json:"schema"`
	Root     string   `json:"root"`
	Created  []string `json:"created"`
	Updated  []string `json:"updated"`
	Kept     []string `json:"kept"`
	Summary  []string `json:"summary"`
	Commit   []string `json:"commit"`
	Warnings []string `json:"warnings"`
	// SavedKey is the label of a key saved during setup, never its value.
	SavedKey   string    `json:"savedKey,omitempty"`
	SearchHint string    `json:"searchHint,omitempty"`
	Pin        PinResult `json:"pin"`
}

// PinResult reports installing the pinned Rhizome executable.
type PinResult struct {
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Plan reports what a first run would do with opts' choices, and the
// choices it offers, without asking or writing.
func Plan(opts RunOptions) (SetupPlan, error) {
	opts.Check = true
	r, s, workflowChosen, err := startMachineRun(opts)
	if err != nil {
		return SetupPlan{}, err
	}
	s, res, outcome, err := r.prepareFirstRun(s, workflowChosen)
	if err != nil {
		return SetupPlan{}, err
	}
	return r.setupPlan(s, res, outcome)
}

// Apply sets up a first run with opts' choices without asking. The result
// carries a saved key's label even with an error. A failed install of the
// pinned executable is reported in the result, because setup was written.
func Apply(opts RunOptions) (SetupResult, error) {
	result := SetupResult{Schema: SetupSchema}
	r, s, workflowChosen, err := startMachineRun(opts)
	if err != nil {
		return result, err
	}
	result.Root = r.layout.ProjectRoot
	s, res, outcome, err := r.prepareFirstRun(s, workflowChosen)
	result.SavedKey = outcome.savedKey
	if err != nil {
		return result, err
	}
	created, updated, err := supportWrites(r.layout.ProjectRoot, s)
	if err != nil {
		return result, err
	}
	report, err := r.apply(s, res)
	if err != nil {
		return result, err
	}
	result.Created = append(created, report.Created...)
	result.Updated = append(updated, report.Updated...)
	result.Kept = orEmpty(report.Kept)
	result.Warnings = orEmpty(report.Warnings)
	result.Summary = setupSummaryLines(report, res.effective, s.skips, outcome.savedKey)
	result.Commit = commitPaths(report)
	result.SearchHint = searchHint(outcome.provider, outcome.ready)
	result.Pin.Version = pinVersion(res.cfg)
	if err := ensurePinnedBinary(r.layout.ProjectRoot, res.cfg, r.opts); err != nil {
		result.Pin.Error = err.Error()
	}
	return result, nil
}

// startMachineRun starts a run that never asks or prints, for a folder that
// is not set up yet.
func startMachineRun(opts RunOptions) (*initRun, setup, bool, error) {
	opts.Interactive = false
	opts.Stdin = strings.NewReader("")
	opts.Stdout, opts.Stderr = io.Discard, io.Discard
	r, s, workflowChosen, err := start(opts)
	if err != nil {
		return nil, setup{}, false, err
	}
	if r.layout.HasExistingConfig {
		return nil, setup{}, false, ErrNotFirstRun
	}
	return r, s, workflowChosen, nil
}

func (r *initRun) setupPlan(s setup, res resolved, outcome firstRunOutcome) (SetupPlan, error) {
	root := r.layout.ProjectRoot
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return SetupPlan{}, err
	}
	scope, err := plannedScope(root, s)
	if err != nil {
		return SetupPlan{}, err
	}
	plan := SetupPlan{
		Schema:              SetupSchema,
		Root:                root,
		Name:                filepath.Base(root),
		Findings:            SetupFindings{Docs: docsFinding(s.cfg.Notes, r.layout), Code: codeFinding(r.layout)},
		Workflow:            workflowChoiceFor(s.workflows).id,
		Addons:              addonOptions(registry, res.effective),
		Agents:              r.agentOptions(s.cfg),
		Search:              r.searchOptions(outcome),
		IgnoredRepositories: []IgnoredRepository{},
		Pin:                 pinVersion(res.cfg),
		Scope:               scope,
	}
	if len(s.skips) > 0 {
		plan.Findings.Skip = skipSummary(s.skips)
	}
	for i, choice := range workflowChoices {
		plan.Workflows = append(plan.Workflows, WorkflowOption{ID: choice.id, Label: choice.label, Description: choice.description, Recommended: i == 0})
	}
	for _, candidate := range r.layout.IgnoredRepoCandidates {
		repo := IgnoredRepository{Path: candidate.Rel, Included: contains(s.includeIgnored, candidate.Rel)}
		if candidate.Rule != nil {
			repo.Source, repo.Line, repo.Pattern = candidate.Rule.Source, candidate.Rule.Line, candidate.Rule.Pattern
		}
		plan.IgnoredRepositories = append(plan.IgnoredRepositories, repo)
	}
	created, updated, err := supportWrites(root, s)
	if err != nil {
		return SetupPlan{}, err
	}
	pending := res.files.pending()
	plan.Writes = SetupWrites{
		Summary: setupSummaryLines(syncReport{Created: pending.Create, Updated: pending.Update}, res.effective, s.skips, ""),
		Files:   append(append(append(created, updated...), pending.Create...), pending.Update...),
	}
	return plan, nil
}

// supportWrites lists the Rhizome support files a first run creates or
// updates besides its generated files: the configuration, workflow state,
// and .rhizome/ignore when it changes.
func supportWrites(root string, s setup) (created, updated []string, err error) {
	created = []string{".rhizome/config.yml", ".rhizome/workflows.yml"}
	planned, current, exists, err := plannedIgnore(root, s)
	switch {
	case err != nil:
		return nil, nil, err
	case !exists:
		created = append(created, ".rhizome/ignore")
	case planned != current:
		updated = append(updated, ".rhizome/ignore")
	}
	return created, orEmpty(updated), nil
}

func addonOptions(registry map[string]starterTemplateMetadata, effective []string) []AddonOption {
	out := []AddonOption{}
	for _, id := range addonIDs(registry) {
		meta := registry[id]
		option := AddonOption{ID: id, Label: firstNonEmpty(meta.Name, id), Description: meta.Description, DefaultFor: []string{}, Enabled: contains(effective, id)}
		for _, choice := range workflowChoices {
			set, err := resolveTemplateSet(choice.starters, obsidian.WorkflowTemplateAddons{}, registry)
			if err == nil && set.Causes[id] == templateCauseDefaultAddon {
				option.DefaultFor = append(option.DefaultFor, choice.id)
			}
		}
		out = append(out, option)
	}
	return out
}

func (r *initRun) agentOptions(cfg obsidian.LocalConfig) []AgentOption {
	enabled := r.harnessesFor(cfg)
	agents := []struct {
		id, label         string
		detected, enabled bool
	}{
		{"claude", "Claude Code", r.detectedAgents.HasClaude, enabled.HasClaude},
		{"codex", "Codex", r.detectedAgents.HasCodex, enabled.HasCodex},
		{"cursor", "Cursor", r.detectedAgents.HasCursor, enabled.HasCursor},
	}
	out := make([]AgentOption, 0, len(agents))
	for _, agent := range agents {
		option := AgentOption{ID: agent.id, Label: agent.label, Enabled: agent.enabled}
		if agent.detected {
			option.Detected = "marker"
		} else if _, err := installedAgentCommand(agent.id); err == nil {
			option.Detected = "installed"
		}
		out = append(out, option)
	}
	return out
}

func (r *initRun) searchOptions(outcome firstRunOutcome) SearchOptions {
	options := SearchOptions{Provider: outcome.provider, Ready: outcome.ready, Hint: searchHint(outcome.provider, outcome.ready)}
	for _, id := range []string{searchVoyage, searchOpenAI, searchOllama, searchOff} {
		provider := SearchProvider{ID: id, Label: providerDisplayName(id), Ready: searchReady(id, r.session, nil)}
		if need, ok := searchNeed(id); ok {
			provider.Key, provider.KeyLabel, provider.TeamKey = need.Key, need.Label, r.session.OffersTeamKey(need)
		}
		switch {
		case id == searchOff:
			provider.Label, provider.Ready = "Off", true
		case id == searchOllama && !provider.Ready:
			provider.Hint = embeddings.OllamaInstallHint()
		}
		options.Providers = append(options.Providers, provider)
	}
	return options
}

func pinVersion(cfg obsidian.LocalConfig) string {
	if external, _ := cfg.Rhizome.UsesExternalBinaryManager(); external {
		return ""
	}
	return strings.TrimSpace(cfg.Rhizome.Version)
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
