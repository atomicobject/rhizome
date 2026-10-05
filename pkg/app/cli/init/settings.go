// The settings menu: four sections that cover the decisions that matter.
// Expert settings stay in .rhizome/config.yml.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US4]]
package init

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// editSettings shows the settings sections until the person presses Enter.
func (r *initRun) editSettings(s *setup) error {
	out := r.opts.Stdout
	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, styleHeading(out, "Settings"))
		fmt.Fprintf(out, "  1  %-19s %s\n", "What gets indexed", noteCount(s.cfg.Notes, r.layout)+" · "+codeStatus(s.cfg.Code, r.layout))
		fmt.Fprintf(out, "  2  %-19s %s\n", "Semantic search", searchStatus(s.cfg, r.session))
		fmt.Fprintf(out, "  3  %-19s %s\n", "Agents", agentsFinding(r.harnessesFor(s.cfg)))
		fmt.Fprintf(out, "  4  %-19s %s\n", "Workflow", workflowLabel(s.workflows, s.cfg.WorkflowTemplateManagement.Ejected))
		fmt.Fprintln(out, styleDim(out, "  Other settings live in .rhizome/config.yml; the rhizome skill's configuration reference explains them."))
		var err error
		switch promptChoice(r.reader, out, "Choose a number, or press Enter when done: ", "") {
		case "1":
			err = r.editIndexing(s)
		case "2":
			err = r.editSearch(s)
		case "3":
			r.editAgents(s)
		case "4":
			err = r.editWorkflow(s)
		case "":
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// editIndexing changes which docs and code Rhizome indexes.
func (r *initRun) editIndexing(s *setup) error {
	out := r.opts.Stdout
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "What gets indexed"))
	printFinding(out, "Docs", docsFinding(s.cfg.Notes, r.layout))
	printFinding(out, "Code", codeStatus(s.cfg.Code, r.layout))
	if skipped := r.skipped(*s); len(skipped) > 0 {
		printFinding(out, "Skip", skipSummary(skipped))
	}
	if len(s.includeIgnored) > 0 {
		printFinding(out, "Included", joinHumanPaths(folderNames(s.includeIgnored))+" (Git ignores them)")
	}
	limited := notesLimited(s.cfg.Notes) || len(codeFolderLimits(s.cfg.Code)) > 0 || !s.cfg.Code.IndexesCode()
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  1  Skip a folder or file")
	fmt.Fprintln(out, "  2  Stop skipping a folder or file")
	fmt.Fprintln(out, "  3  Index a folder Git ignores")
	if limited {
		fmt.Fprintln(out, "  4  Index all Markdown and code (remove the folder limits in .rhizome/config.yml)")
	}
	switch promptChoice(r.reader, out, "Choose a number, or press Enter to go back: ", "") {
	case "1":
		r.skipPath(s)
	case "2":
		r.stopSkipping(s)
	case "3":
		r.includeIgnoredFolder(s)
	case "4":
		if limited {
			s.cfg.Notes.Includes = withoutNotesLimits(s.cfg.Notes.Includes)
			s.cfg.Code = removeCodeFolderLimits(s.cfg.Code)
		}
	}
	return nil
}

// skipped lists the suggested skips already in .rhizome/ignore plus the
// pending ones, minus paths marked to index again.
func (r *initRun) skipped(s setup) []skip {
	keep := stringSet(s.keep)
	var out []skip
	for _, sk := range readIgnoreFileState(r.layout.ProjectRoot).skipped {
		if !keep[cleanSkipPath(sk.path)] {
			out = append(out, sk)
		}
	}
	return mergeSkips(out, s.skips)
}

// skipPath leaves a folder or file out of the index.
func (r *initRun) skipPath(s *setup) {
	out := r.opts.Stdout
	input := promptLine(r.reader, out, "Folder or file to skip (Enter to go back): ")
	if input == "" {
		return
	}
	rel := cleanSkipPath(input)
	info, err := os.Stat(filepath.Join(r.layout.ProjectRoot, filepath.FromSlash(rel)))
	if rel == "" || err != nil {
		fmt.Fprintf(out, "%s: %s is not a folder or file in this repository\n", styleWarn(out, "Not changed"), input)
		return
	}
	if info.IsDir() {
		rel += "/"
	}
	s.skips = mergeSkips(s.skips, []skip{{path: rel, reason: reasonManual}})
	s.keep = removeString(s.keep, cleanSkipPath(rel))
}

// stopSkipping indexes a skipped path again and records it so init does not
// propose it again. Paths skipped by other rules are explained instead.
func (r *initRun) stopSkipping(s *setup) {
	out := r.opts.Stdout
	for _, sk := range r.skipped(*s) {
		fmt.Fprintf(out, "  %s\n", sk.path)
	}
	p := cleanSkipPath(promptLine(r.reader, out, "Path to index again (Enter to go back): "))
	if p == "" {
		return
	}
	for _, sk := range r.skipped(*s) {
		if cleanSkipPath(sk.path) == p {
			var pending []skip
			for _, other := range s.skips {
				if cleanSkipPath(other.path) != p {
					pending = append(pending, other)
				}
			}
			s.skips = pending
			s.keep = appendUnique(cloneTemplates(s.keep), p)
			return
		}
	}
	info, err := os.Stat(filepath.Join(r.layout.ProjectRoot, filepath.FromSlash(p)))
	decision := initIgnoreMatcher(r.layout.ProjectRoot).Explain(p, err == nil && info.IsDir())
	switch {
	case !decision.Ignored || decision.Rule == nil:
		fmt.Fprintf(out, "%s is not skipped.\n", p)
	case decision.Rule.Layer == ignore.LayerGitignore:
		fmt.Fprintf(out, "Git ignores %s (%s line %d). Choose 3 to index it anyway.\n", p, decision.Rule.Source, decision.Rule.Line)
	case decision.Rule.Source == "":
		fmt.Fprintf(out, "%s is in Rhizome's built-in skip list (%s).\n", p, decision.Rule.Pattern)
	default:
		fmt.Fprintf(out, "%s is skipped by %s line %d (%s). Remove that line to index it.\n", p, decision.Rule.Source, decision.Rule.Line, decision.Rule.Pattern)
	}
}

func removeString(values []string, value string) []string {
	var out []string
	for _, v := range values {
		if v != value {
			out = append(out, v)
		}
	}
	return out
}

// includeIgnoredFolder indexes a folder Git ignores, such as a nested
// repository.
func (r *initRun) includeIgnoredFolder(s *setup) {
	out := r.opts.Stdout
	for _, candidate := range r.layout.IgnoredRepoCandidates {
		fmt.Fprintf(out, "  %s  %s\n", candidate.Rel, styleDim(out, ignoredCandidateSummary(candidate)))
	}
	input := promptLine(r.reader, out, "Folder to index even though Git ignores it (Enter to go back): ")
	if input == "" {
		return
	}
	rel, err := includeIgnoredInput(r.layout.ProjectRoot, input)
	if err != nil {
		fmt.Fprintf(out, "%s: %v\n", styleWarn(out, "Not changed"), err)
		return
	}
	s.includeIgnored = dedupePreserveOrder(append(cloneTemplates(s.includeIgnored), rel))
	mergeDeferredIgnoredCodeSuggestions(&r.layout, []string{rel})
	found := detectCode(filepath.Join(r.layout.ProjectRoot, filepath.FromSlash(rel)))
	notes := 0
	for _, file := range found.Files {
		if globsMatch(allMarkdown, file) {
			notes++
		}
	}
	code := "no code"
	if names := languageNames(found.Languages); len(names) > 0 {
		code = strings.Join(names, ", ") + " code"
	}
	fmt.Fprintf(out, "Rhizome will index %s/ and found %s and %s there.\n", rel, countNoun(notes, "note"), code)
}

// editSearch chooses the semantic search provider and asks for its key when
// none is available.
func (r *initRun) editSearch(s *setup) error {
	out := r.opts.Stdout
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Semantic search"))
	fmt.Fprintln(out, "  Rhizome can search notes and code by meaning.")
	current := searchProviderOf(s.cfg)
	if s.cfg.NoteEmbeddings == nil {
		current = ""
	}
	mark := func(provider string) string { return readyLabel(provider == current, ", current", "") }
	fmt.Fprintf(out, "  1  Voyage AI, recommended (%s%s)\n", readyLabel(searchReady(searchVoyage, r.session, nil), "key found", "needs a key"), mark(searchVoyage))
	fmt.Fprintf(out, "  2  OpenAI (%s%s)\n", readyLabel(searchReady(searchOpenAI, r.session, nil), "key found", "needs a key"), mark(searchOpenAI))
	fmt.Fprintf(out, "  3  Ollama, runs locally and free (%s%s)\n", readyLabel(searchReady(searchOllama, r.session, s.cfg.NoteEmbeddings), "installed", "not installed"), mark(searchOllama))
	fmt.Fprintf(out, "  4  Off%s\n", readyLabel(current == searchOff || current == "", " (current)", ""))
	provider, ok := map[string]string{"1": searchVoyage, "2": searchOpenAI, "3": searchOllama, "4": searchOff}[promptChoice(r.reader, out, "Choose a number, or press Enter to go back: ", "")]
	if !ok {
		return nil
	}
	ready := searchReady(provider, r.session, s.cfg.NoteEmbeddings)
	if !ready && (provider == searchVoyage || provider == searchOpenAI) {
		var saved string
		var err error
		if ready, saved, err = askKey(r.reader, out, provider, r.session); err != nil {
			return err
		}
		if saved != "" {
			fmt.Fprintf(out, "%s %s saved to ~/.config/rhizome/config.yml\n", styleOK(out, "✓"), saved)
		}
	}
	applySearchChoice(&s.cfg, provider, ready)
	noticeSearchNotReady(out, provider, ready)
	return nil
}

// editAgents toggles the agents Rhizome writes guidance for.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US1]]
func (r *initRun) editAgents(s *setup) {
	out := r.opts.Stdout
	for {
		h := r.harnessesFor(s.cfg)
		fmt.Fprintln(out)
		fmt.Fprintln(out, styleHeading(out, "Agents"))
		fmt.Fprintln(out, styleDim(out, "  AGENTS.md and .agents/skills are written for every agent; Codex, Cursor, and most agents read them."))
		// Codex reads only the shared files, so it has no row of its own.
		fmt.Fprintf(out, "  1  %s Claude Code   CLAUDE.md and skills in .claude/\n", checkbox(h.HasClaude))
		fmt.Fprintf(out, "  2  %s Cursor        a Rhizome rule in .cursor/rules\n", checkbox(h.HasCursor))
		fmt.Fprintln(out, "  3  Stop managing agent files")
		prefs := obsidian.AgentPreferences{}
		if s.cfg.Agents != nil {
			prefs = *s.cfg.Agents
		}
		switch promptChoice(r.reader, out, "Choose a number to turn it on or off, or press Enter when done: ", "") {
		case "1":
			prefs.Claude = toggledMode(h.HasClaude)
		case "2":
			prefs.Cursor = toggledMode(h.HasCursor)
		case "3":
			prefs = obsidian.AgentPreferences{Claude: agentModeOff, Codex: agentModeOff, Cursor: agentModeOff, AgentSkills: agentModeOff, AgentsMd: agentModeOff}
			s.cfg.Agents = &prefs
			return
		default:
			return
		}
		prefs.AgentSkills, prefs.AgentsMd = agentModeOn, agentModeOn
		s.cfg.Agents = &prefs
	}
}

func checkbox(on bool) string {
	return readyLabel(on, "[x]", "[ ]")
}

func toggledMode(on bool) string {
	return readyLabel(on, agentModeOff, agentModeOn)
}

// editWorkflow chooses the workflow. Moving away from an installed workflow
// asks whether to remove Rhizome's files for it or keep them for the team
// (eject).
//
// Docs: [[init-starter-workflow#^SPEC-0038-US7]]
func (r *initRun) editWorkflow(s *setup) error {
	out := r.opts.Stdout
	next := promptWorkflow(r.reader, out, s.workflows)
	if reflect.DeepEqual(normalizeMetadataIDs(next), normalizeMetadataIDs(s.workflows)) {
		return nil
	}
	installed, err := resolveEffectiveTemplatesWithOptions(s.workflows, &s.cfg, suppressDefaultAddonsForExistingConfig(r.layout, s.cfg))
	if err != nil {
		return err
	}
	kept := stringSet(r.starterClosure(*s, next))
	var dropped []string
	for _, id := range installed {
		if !kept[id] {
			dropped = append(dropped, id)
		}
	}
	ejected := stringSet(normalizeMetadataIDs(s.cfg.WorkflowTemplateManagement.Ejected))
	if len(dropped) > 0 && r.layout.HasExistingConfig {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "What should happen to the files %s installed?\n", workflowLabel(s.workflows, nil))
		fmt.Fprintln(out, "  1  Keep every file for your team to own; Rhizome stops updating them")
		fmt.Fprintln(out, "  2  Remove the skills, guidance, saved queries, and views Rhizome added (your docs stay)")
		if askChoice(r.reader, out, "Choose [1]: ", "1", "1", "2") == "1" {
			for _, id := range dropped {
				ejected[id] = true
			}
		}
	}
	// Choosing a workflow again resumes updates for it.
	for id := range kept {
		delete(ejected, id)
	}
	s.cfg.WorkflowTemplateManagement.Ejected = sortedStringSet(ejected)
	s.workflows = next
	return nil
}

// starterClosure lists every starter workflows installs, ignoring ejection.
func (r *initRun) starterClosure(s setup, workflows []string) []string {
	cfg := s.cfg
	cfg.WorkflowTemplateManagement.Ejected = nil
	effective, err := resolveEffectiveTemplatesWithOptions(workflows, &cfg, suppressDefaultAddonsForExistingConfig(r.layout, cfg))
	if err != nil {
		return workflows
	}
	return effective
}
