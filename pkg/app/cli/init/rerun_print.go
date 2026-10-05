package init

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// summaryField is one row of the setup summary.
type summaryField struct {
	label string
	value string
}

func (r *initRun) summaryFields(s setup) []summaryField {
	labels := []string{"Docs", "Code", "Agents", "Search", "Workflow"}
	out := make([]summaryField, 0, len(labels))
	for _, label := range labels {
		out = append(out, summaryField{label: label, value: r.summaryValue(s, label)})
	}
	return out
}

func (r *initRun) summaryValue(s setup, label string) string {
	switch label {
	case "Docs":
		return docsFinding(s.cfg.Notes, r.layout)
	case "Code":
		return codeStatus(s.cfg.Code, r.layout)
	case "Agents":
		return agentsFinding(r.harnessesFor(s.cfg))
	case "Search":
		return searchStatus(s.cfg, r.session)
	default:
		return workflowLabel(s.workflows, s.cfg.WorkflowTemplateManagement.Ejected)
	}
}

// codeStatus names the code languages Rhizome indexes and any folder
// limits.
func codeStatus(code obsidian.LocalCodeConfig, layout DetectedLayout) string {
	if !code.IndexesCode() {
		return "off"
	}
	what := strings.Join(languageNames(layout.Code.Languages), ", ")
	if what == "" {
		what = "on, no code yet"
	}
	if limits := codeFolderLimits(code); len(limits) > 0 {
		return what + " (limited to " + joinHumanPaths(folderNames(limits)) + ")"
	}
	return what
}

// searchStatus describes semantic search in plain words.
func searchStatus(cfg obsidian.LocalConfig, session *credentials.Session) string {
	if cfg.NoteEmbeddings == nil {
		if searchReady(searchVoyage, session, nil) || searchReady(searchOpenAI, session, nil) {
			return "off (a key is available)"
		}
		return "off until a key is available"
	}
	provider := searchProviderOf(cfg)
	switch {
	case provider == searchOff:
		return "off"
	case !searchReady(provider, session, cfg.NoteEmbeddings):
		return providerDisplayName(provider) + " (" + readyLabel(provider == searchOllama, "not installed", "key missing") + ")"
	default:
		return providerDisplayName(provider)
	}
}

// workflowLabel names the managed workflow the way the menu does, then the
// starters whose files stay unmanaged: ejected ones and what they require.
func workflowLabel(workflows, ejected []string) string {
	ejected = normalizeMetadataIDs(ejected)
	workflows = removeTemplateIDs(workflows, stringSet(ejected))
	label := "Search and agent guidance only"
	switch {
	case contains(workflows, templateComplexDomain):
		label = "Agentic Engineering with domain modeling"
	case contains(workflows, templateAgenticEngineering):
		label = "Agentic Engineering"
	case len(workflows) > 0:
		label = strings.Join(workflows, ", ")
	}
	if len(ejected) > 0 {
		var managed []string
		if registry, err := loadStarterTemplateMetadataRegistry(); err == nil {
			if set, err := resolveTemplateSet(workflows, obsidian.WorkflowTemplateAddons{}, registry); err == nil {
				managed = set.Effective
			}
		}
		label += "; " + joinHumanPaths(starterNames(frozenStarters(ejected, managed))) + " files kept for your team"
	}
	return label
}

// starterNames returns the display names of starter ids.
func starterNames(ids []string) []string {
	registry, _ := loadStarterTemplateMetadataRegistry()
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, firstNonEmpty(registry[id].Name, id))
	}
	return names
}

// printRerun shows the setup and what a run would change, in three groups:
// changes, files that need a person's decision, and suggestions.
func (r *initRun) printRerun(plan rerunPlan, s setup) {
	out := r.opts.Stdout
	base := filepath.Base(r.layout.ProjectRoot)
	fmt.Fprintln(out)
	if plan.proposed {
		fmt.Fprintln(out, styleHeading(out, "Proposed setup for "+base))
	} else {
		fmt.Fprintln(out, styleHeading(out, "Rhizome is set up in "+base))
	}
	for _, field := range r.summaryFields(s) {
		printFinding(out, field.label, field.value)
	}
	if changes := append(append([]planLine{}, plan.settings...), plan.files...); len(changes) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, styleHeading(out, "Changes"))
		printPlanLines(out, changes, r.opts.Check)
	}
	if len(plan.decisions) > 0 {
		note := "(run rzm init in a terminal to decide)"
		if r.opts.Interactive {
			note = "(asked after you apply)"
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, styleHeading(out, "Needs a decision")+" "+styleDim(out, note))
		printPlanLines(out, plan.decisions, r.opts.Check)
	}
	if len(plan.suggestions) > 0 {
		note := "(not applied; rzm init --accept-suggestions applies them)"
		switch {
		case plan.accepted && r.opts.Interactive:
			note = "(applied with the changes)"
		case plan.accepted:
			note = "(applied: --accept-suggestions)"
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, styleHeading(out, "Suggestions")+" "+styleDim(out, note))
		printPlanLines(out, plan.suggestions, r.opts.Check)
	}
}

// printPlanLines prints each line and the paths under it: up to five, or all
// of them when all is set.
func printPlanLines(out io.Writer, lines []planLine, all bool) {
	for _, line := range lines {
		fmt.Fprintf(out, "  %s %s\n", line.mark, line.text)
		shown := line.paths
		if !all && len(shown) > 5 {
			shown = shown[:5]
		}
		for _, path := range shown {
			fmt.Fprintf(out, "      %s\n", styleDim(out, path))
		}
		if more := len(line.paths) - len(shown); more > 0 {
			fmt.Fprintf(out, "      %s\n", styleDim(out, fmt.Sprintf("and %d more", more)))
		}
	}
}

// printRerunResult reports what a rerun did, in the past tense.
func (r *initRun) printRerunResult(plan rerunPlan, report syncReport) {
	out := r.opts.Stdout
	var done []string
	for _, line := range append(append([]planLine{}, plan.settings...), plan.files...) {
		if line.done != "" {
			done = append(done, line.done)
		}
	}
	if plan.accepted {
		for _, line := range plan.suggestions {
			done = append(done, line.done)
		}
	}
	for _, group := range []struct {
		verb, noun string
		paths      []string
	}{{"Created", "Rhizome file", report.Created}, {"Updated", "Rhizome file", report.Updated}, {"Removed", "file Rhizome no longer ships", report.Removed}} {
		if n := len(dedupePreserveOrder(group.paths)); n > 0 {
			done = append(done, group.verb+" "+countNoun(n, group.noun))
		}
	}
	done = append(done, keptYourVersion(report.Declined)...)
	fmt.Fprintln(out)
	if len(done) == 0 && len(report.Kept) == 0 {
		fmt.Fprintln(out, "Nothing changed.")
	}
	for _, line := range done {
		fmt.Fprintf(out, "  %s %s\n", styleOK(out, "✓"), line)
	}
	printKeptAndWarnings(out, report)
	warnEmbeddingsReadiness(plan.res.cfg, out)
	warnCompressionReadiness(plan.res.cfg, out)
}

// keptYourVersion reports files a person kept, once per skill across agent
// folders.
func keptYourVersion(paths []string) []string {
	copies := map[string]int{}
	var order []string
	for _, path := range paths {
		key := strings.TrimPrefix(strings.TrimPrefix(path, ".agents/"), ".claude/")
		if copies[key] == 0 {
			order = append(order, path)
		}
		copies[key]++
	}
	var out []string
	for _, path := range order {
		line := "Kept your version of " + path
		if n := copies[strings.TrimPrefix(strings.TrimPrefix(path, ".agents/"), ".claude/")] - 1; n > 0 {
			line += " (and " + countNoun(n, "copy") + ")"
		}
		out = append(out, line)
	}
	return out
}

// rerunNextSteps lists what to run after a rerun applied changes.
func rerunNextSteps(plan rerunPlan, report syncReport) [][2]string {
	if len(plan.settings) == 0 && len(report.changedPaths()) == 0 && !(plan.accepted && len(plan.suggestions) > 0) {
		return nil
	}
	steps := [][2]string{{"rzm index", "pick up the changes"}}
	if selector := starterValidationSelector(report.changedPaths()); selector != "" {
		steps = append(steps, [2]string{"rzm validate " + selector, "check the updated schema, saved queries, and views"})
	}
	return steps
}

// printNext prints a Next block of commands with aligned descriptions.
func printNext(out io.Writer, steps [][2]string, notes ...string) {
	if len(steps) == 0 && len(notes) == 0 {
		return
	}
	width := 0
	for _, step := range steps {
		width = max(width, len(step[0]))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Next"))
	for _, step := range steps {
		fmt.Fprintf(out, "  %-*s  %s\n", width, step[0], step[1])
	}
	for _, note := range notes {
		fmt.Fprintf(out, "  %s\n", note)
	}
}
