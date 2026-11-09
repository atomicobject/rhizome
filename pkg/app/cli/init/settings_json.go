// Machine reruns: on a configured folder, `rzm init --check --json` reports
// the repository's settings as the given options would leave them, and
// `rzm init --json` applies those options the way a run without a terminal
// does. The desktop app's repository settings are built from these documents.
//
// Docs: [[desktop-repository-setup#^SPEC-0118-US3]]
package init

import (
	"os"
	"path/filepath"
)

// settingsPlan reports a configured folder's settings with the run's options
// applied, and what applying them would write.
func (r *initRun) settingsPlan(s setup) (SetupPlan, error) {
	plan, err := r.planSettings(s)
	if err != nil {
		return SetupPlan{}, err
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return SetupPlan{}, err
	}
	root := r.layout.ProjectRoot
	cfg := plan.res.cfg
	provider, ready := currentSearch(r, plan.setup)
	doc := SetupPlan{
		Schema:              SetupSchema,
		Root:                root,
		Name:                filepath.Base(root),
		Configured:          true,
		Findings:            SetupFindings{Docs: docsFinding(cfg.Notes, r.layout), Code: codeStatus(cfg.Code, r.layout)},
		Workflow:            workflowChoiceFor(plan.setup.workflows).id,
		Addons:              allAddonOptions(registry, plan.res.effective),
		Agents:              r.agentOptions(plan.setup.cfg),
		Search:              r.searchOptions(firstRunOutcome{provider: provider, ready: ready}),
		IgnoredRepositories: []IgnoredRepository{},
		Pin:                 pinVersion(cfg),
		HooksBlocked:        hooksBlockedReason(plan.res),
		Writes:              r.settingsWrites(plan),
	}
	for i, choice := range workflowChoices {
		doc.Workflows = append(doc.Workflows, WorkflowOption{ID: choice.id, Label: choice.label, Description: choice.description, Recommended: i == 0})
	}
	return doc, nil
}

// applySettings applies a configured folder's options without asking: edited
// files are kept and opt-in suggestions are left for a terminal.
func (r *initRun) applySettings(s setup) (SetupResult, error) {
	result := SetupResult{Schema: SetupSchema, Root: r.layout.ProjectRoot, Created: []string{}, Updated: []string{}, Kept: []string{}, Commit: []string{}, Warnings: []string{}}
	plan, err := r.planSettings(s)
	if err != nil {
		return result, err
	}
	result.Pin.Version = pinVersion(plan.res.cfg)
	if !plan.pending() {
		result.Summary = []string{"Everything is up to date."}
		return result, nil
	}
	report, err := r.apply(plan.setup, plan.res)
	if err != nil {
		return result, err
	}
	result.Created = orEmpty(report.Created)
	result.Updated = orEmpty(report.Updated)
	result.Kept = orEmpty(report.Kept)
	result.Warnings = orEmpty(report.Warnings)
	result.Summary = orEmpty(rerunDoneLines(plan, report))
	result.Commit = commitPaths(report)
	result.HooksBlocked = hooksBlockedReason(plan.res)
	if err := ensurePinnedBinary(r.layout.ProjectRoot, plan.res.cfg, r.opts); err != nil {
		result.Pin.Error = err.Error()
	}
	return result, nil
}

// planSettings applies the run's options and plans the rerun they make,
// leaving opt-in suggestions out.
func (r *initRun) planSettings(s setup) (rerunPlan, error) {
	before, s, err := r.applyRerunOptions(s)
	if err != nil {
		return rerunPlan{}, err
	}
	return r.planRerun(before, s, false)
}

// settingsWrites lists the change lines a terminal rerun prints, every file
// they touch, and the edited files a run without a terminal keeps.
func (r *initRun) settingsWrites(plan rerunPlan) SetupWrites {
	writes := SetupWrites{Summary: []string{}, Files: []string{}}
	seen := map[string]bool{}
	addFile := func(path string) {
		if !seen[path] {
			seen[path] = true
			writes.Files = append(writes.Files, path)
		}
	}
	for _, line := range append(append([]planLine{}, plan.settings...), plan.files...) {
		writes.Summary = append(writes.Summary, line.text)
		for _, path := range line.paths {
			addFile(path)
		}
	}
	if plan.pending() {
		for _, path := range r.configFiles(plan.res.cfg) {
			addFile(path)
		}
		// Apply also writes these support files when they differ.
		root := r.layout.ProjectRoot
		if data, err := os.ReadFile(filepath.Join(root, ".rhizome", ".gitignore")); err != nil || string(data) != rhizomeGitIgnoreBody() {
			addFile(".rhizome/.gitignore")
		}
		if planned, current, exists, err := plannedIgnore(root, plan.setup); err == nil && (!exists || planned != current) {
			addFile(".rhizome/ignore")
		}
	}
	for _, line := range plan.decisions {
		writes.Kept = append(writes.Kept, line.paths...)
	}
	return writes
}

// currentSearch is the provider a setup uses and whether it can run now.
// Without a search setting, search is off until a key is available.
func currentSearch(r *initRun, s setup) (string, bool) {
	if s.cfg.NoteEmbeddings == nil {
		return searchOff, false
	}
	provider := searchProviderOf(s.cfg)
	return provider, provider != searchOff && searchReady(provider, r.session, s.cfg.NoteEmbeddings)
}

// allAddonOptions lists every add-on, optional ones included, so an
// installed add-on is never hidden from a configured folder's settings.
func allAddonOptions(registry map[string]starterTemplateMetadata, effective []string) []AddonOption {
	out := []AddonOption{}
	for _, id := range addonIDs(registry) {
		meta := registry[id]
		out = append(out, AddonOption{ID: id, Label: firstNonEmpty(meta.Name, id), Description: meta.Description, DefaultFor: defaultForWorkflows(id, registry), Enabled: contains(effective, id)})
	}
	return out
}
