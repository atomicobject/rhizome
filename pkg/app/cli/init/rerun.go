// Reruns: show the setup and what init would change, ask once in a
// terminal, and apply. First runs share the resolve and apply steps.
//
// Docs:
// - [[init-starter-workflow#^SPEC-0038-US3]]
// - [[init-starter-workflow#^SPEC-0038-US4]]
// - [[init-starter-workflow#^SPEC-0038-US9]]
package init

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"gopkg.in/yaml.v3"
)

// ErrChangesPending reports that `rzm init --check` found changes init would
// make.
var ErrChangesPending = errors.New("rzm init would make changes")

// initRun carries what every step of one init run shares.
type initRun struct {
	opts    RunOptions
	reader  *bufio.Reader
	session *credentials.Session
	ui      ownershipUI
	layout  DetectedLayout
	// detectedAgents are the agents repository markers show, before
	// installed commands and choices change the layout's harnesses.
	detectedAgents AgentHarnesses
}

// setup is what init will write: the config plus choices that live outside
// it. Code that changes a setup replaces slices and pointers instead of
// mutating them, so a shallow copy is a safe snapshot.
type setup struct {
	cfg       obsidian.LocalConfig
	workflows []string // chosen workflow starters
	// includeIgnored lists folders Git ignores that Rhizome should index.
	includeIgnored []string
	// skips are paths to add to the suggested skips in .rhizome/ignore, and
	// keep are paths to index again and never propose.
	skips []skip
	keep  []string
}

// resolved is a setup made concrete: the final config, the starters it
// installs, the agent folders, and the classified generated files.
type resolved struct {
	cfg       obsidian.LocalConfig
	effective []string
	harnesses AgentHarnesses
	files     *preparedFiles
}

// harnessesFor applies agent preferences to what detection found.
func (r *initRun) harnessesFor(cfg obsidian.LocalConfig) AgentHarnesses {
	harnesses, _, err := applyAgentOverrides(r.layout.AgentHarnesses, cfg.Agents, RunOptions{})
	if err != nil {
		return r.layout.AgentHarnesses
	}
	return harnesses
}

// resolve computes the final config and plans every generated file without
// writing anything.
func (r *initRun) resolve(s setup) (resolved, error) {
	root := r.layout.ProjectRoot
	cfg := s.cfg
	cfg.WorkflowTemplates = cloneTemplates(s.workflows)
	suppress := suppressDefaultAddonsForExistingConfig(r.layout, cfg)
	reconcileExplicitWorkflowTemplateAddons(&cfg, s.workflows)
	if _, err := persistDefaultWorkflowTemplateAddons(&cfg, s.workflows, suppress); err != nil {
		return resolved{}, err
	}
	effective, err := resolveEffectiveTemplatesWithOptions(s.workflows, &cfg, suppress)
	if err != nil {
		return resolved{}, err
	}
	harnesses, _, err := applyAgentOverrides(r.layout.AgentHarnesses, cfg.Agents, RunOptions{})
	if err != nil {
		return resolved{}, err
	}
	external, err := cfg.Rhizome.UsesExternalBinaryManager()
	if err != nil {
		return resolved{}, err
	}
	if !external && strings.TrimSpace(cfg.Rhizome.Version) == "" && strings.TrimSpace(cfg.Rhizome.DevBinaryDir) == "" {
		cfg.Rhizome.Version = version.Version
	}
	// Notes includes must cover installed starter docs before the managed
	// AGENTS.md block, which describes them, is rendered.
	if _, _, err := ensureInstalledTemplateIncludes(root, &cfg, plannedStarterNotePaths(effective)); err != nil {
		return resolved{}, err
	}
	section, err := renderRhizomeMd(cfg)
	if err != nil {
		return resolved{}, err
	}
	frozen := frozenStarters(cfg.WorkflowTemplateManagement.Ejected, effective)
	files, err := prepareGeneratedFiles(root, generatedInputs{
		harnesses: harnesses,
		section:   section,
		templates: effective,
		surfaceOpts: AgentSurfaceOptions{
			PreserveTemplateBlocks:       frozen,
			PreserveLegacyTemplateBlocks: ejectedAgenticEngineering(frozen),
			PreserveSkillTemplates:       frozen,
		},
		refreshDocs: r.opts.RefreshDocs,
	})
	if err != nil {
		return resolved{}, err
	}
	return resolved{cfg: cfg, effective: effective, harnesses: harnesses, files: files}, nil
}

// apply writes a resolved setup: generated files first, then config, so a
// failed run leaves the old config replayable.
func (r *initRun) apply(s setup, res resolved) (syncReport, error) {
	root := r.layout.ProjectRoot
	if err := ensureRhizomeGitIgnore(root); err != nil {
		return syncReport{}, err
	}
	if err := checkSkillTemplates(res.effective); err != nil {
		return syncReport{}, err
	}
	report, err := res.files.apply(r.ui)
	if err != nil {
		return report, err
	}
	if hasLegacyAgenticEngineeringState(r.layout.ExistingLocal) ||
		hasLegacyProcessMigrationPending(root) ||
		needsComplexDomainLegacyProcessMigration(root, res.effective) {
		if _, err := retireLegacyProcessDocs(root); err != nil {
			return report, err
		}
	}
	if !r.layout.HasExistingConfig {
		if err := writeConfigNew(root, res.cfg); err != nil {
			return report, err
		}
	} else if changes := r.configChanges(res.cfg); changes.any() {
		if err := writeConfigPatched(root, res.cfg, changes); err != nil {
			return report, err
		}
	}
	if err := writeIgnore(root, s); err != nil {
		return report, fmt.Errorf("write .rhizome/ignore: %w", err)
	}
	return report, nil
}

// configChanges lists the config sections that differ from the existing
// config, plus workflow sections that still live in a legacy location.
func (r *initRun) configChanges(cfg obsidian.LocalConfig) changeSet {
	before := r.layout.ExistingLocal
	changes := changeSet{}
	for section, pair := range map[string][2]any{
		sectionRhizome:        {before.Rhizome, cfg.Rhizome},
		sectionNotes:          {before.Notes, cfg.Notes},
		sectionCode:           {before.Code, cfg.Code},
		sectionNoteEmbeddings: {before.NoteEmbeddings, cfg.NoteEmbeddings},
		sectionCodeEmbeddings: {before.CodeEmbeddings, cfg.CodeEmbeddings},
		sectionAgents:         {emptyAgentsAsNil(before.Agents), emptyAgentsAsNil(cfg.Agents)},
		sectionWorkflowTmpls:  {before.WorkflowTemplates, cfg.WorkflowTemplates},
		sectionWorkflowAddons: {before.WorkflowTemplateAddons, cfg.WorkflowTemplateAddons},
		sectionWorkflowMgmt:   {before.WorkflowTemplateManagement, cfg.WorkflowTemplateManagement},
	} {
		if !sameYAML(pair[0], pair[1]) {
			changes.mark(section)
		}
	}
	if r.layout.HasLegacyWorkflowConfig {
		changes.mark(sectionWorkflowTmpls)
		changes.mark(sectionWorkflowAddons)
		changes.mark(sectionWorkflowMgmt)
	}
	return changes
}

func emptyAgentsAsNil(prefs *obsidian.AgentPreferences) *obsidian.AgentPreferences {
	if prefs == nil || prefs.Empty() {
		return nil
	}
	return prefs
}

// sameYAML compares two config values the way they would be written, so nil
// and empty values that write the same count as equal.
func sameYAML(a, b any) bool {
	ya, errA := yaml.Marshal(a)
	yb, errB := yaml.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ya, yb)
}

// rerun updates an existing setup. It shows the setup and the change list,
// asks once in a terminal, and applies. Without a terminal it applies
// everything except opt-in changes; --check only reports.
//
// Docs:
// - [[init-starter-workflow#^SPEC-0038-US3-AC3]]
// - [[init-starter-workflow#^SPEC-0038-US4]]
func (r *initRun) rerun(s setup) error {
	out := r.opts.Stdout
	before := s
	cfg := &s.cfg
	if _, err := normalizeLegacyAgenticEngineeringState(cfg); err != nil {
		return err
	}
	clearRetiredWorkflowManagement(cfg)
	if _, err := applyBinaryManagerOption(cfg, r.opts); err != nil {
		return err
	}
	if strings.TrimSpace(r.opts.Agents) != "" {
		_, prefs, err := applyAgentOverrides(r.layout.AgentHarnesses, cfg.Agents, r.opts)
		if err != nil {
			return err
		}
		cfg.Agents = prefs
	}
	if strings.TrimSpace(r.opts.Search) != "" {
		provider, _ := normalizeSearchOption(r.opts.Search)
		ready := searchReady(provider, r.session, cfg.NoteEmbeddings)
		applySearchChoice(cfg, provider, ready)
		noticeSearchNotReady(out, provider, ready)
	}
	// The change list shows ejection and restore, so nothing prints here.
	if _, err := applyStarterManagementOptions(cfg, r.opts, nil); err != nil {
		return err
	}
	// Restoring a starter the chosen workflow no longer installs selects it
	// again, so its files resume updates instead of being removed.
	if restored, _ := normalizeTemplateNames(splitList(r.opts.Restore)); len(restored) > 0 &&
		!contains(r.starterClosure(s, s.workflows), restored[0]) {
		s.workflows = append(cloneTemplates(s.workflows), restored[0])
	}

	accept := r.opts.Interactive || r.opts.AcceptSuggestions
	var plan rerunPlan
confirm:
	for {
		var err error
		if plan, err = r.planRerun(before, s, accept); err != nil {
			return err
		}
		r.printRerun(plan, s)
		switch {
		case r.opts.Check && plan.pending():
			return ErrChangesPending
		case !r.opts.Interactive && !plan.pending():
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Everything is up to date.")
			return nil
		case !r.opts.Interactive:
			break confirm
		}
		fmt.Fprintln(out)
		if !plan.pending() && len(plan.decisions) == 0 {
			if promptChoice(r.reader, out, "Everything is up to date. Press s for settings or Enter to exit: ", "") != "s" {
				return nil
			}
		} else {
			switch askChoice(r.reader, out, "Apply? [Y/n/s for settings]: ", "y", "y", "n", "s") {
			case "n":
				fmt.Fprintln(out, "Nothing changed.")
				return nil
			case "y":
				break confirm
			}
		}
		if err := r.editSettings(&s); err != nil {
			return err
		}
	}
	report, err := r.apply(plan.setup, plan.res)
	if err != nil {
		return err
	}
	r.printRerunResult(plan, report)
	if err := ensurePinnedBinary(r.layout.ProjectRoot, plan.res.cfg, r.opts); err != nil {
		return err
	}
	steps := rerunNextSteps(plan, report)
	var indexErr error
	if len(steps) > 0 {
		var indexed bool
		if indexed, indexErr = r.offerIndex("Update the search index now? [Y/n]: "); indexed {
			steps = steps[1:]
		}
	}
	printNext(out, steps)
	return indexErr
}

// offerIndex asks, in a terminal, whether to build the search index now, and
// builds it on yes. It reports whether the index was built.
func (r *initRun) offerIndex(prompt string) (bool, error) {
	if !r.opts.Interactive || r.opts.IndexNow == nil {
		return false, nil
	}
	out := r.opts.Stdout
	fmt.Fprintln(out)
	if askChoice(r.reader, out, prompt, "y", "y", "n") == "n" {
		return false, nil
	}
	if err := r.opts.IndexNow(r.layout.ProjectRoot); err != nil {
		fmt.Fprintf(out, "%s: %v\n", styleWarn(out, "Indexing stopped"), err)
		return false, fmt.Errorf("setup is saved, but indexing stopped: %w", err)
	}
	return true, nil
}
