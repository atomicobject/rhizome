package init

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// planLine is one entry in the change list.
type planLine struct {
	mark string // +, ↻, -, !, or ?
	text string
	// done reports the line once applied; empty means it is reported
	// through the file counts.
	done string
	// paths are shown under the line, all of them under --check.
	paths []string
}

// rerunPlan is what a rerun would change.
type rerunPlan struct {
	setup       setup
	res         resolved
	settings    []planLine // setting and detection changes
	files       []planLine // generated files
	suggestions []planLine // opt-in changes, applied only when accepted
	accepted    bool       // suggestions are applied with the changes
	// decisions are edited files only a person can settle. A run without a
	// terminal keeps them, so they do not count as pending there.
	decisions []planLine
	// proposed marks a setup that differs from the one on disk, after
	// settings edits or flags.
	proposed bool
}

// pending reports whether a run without a terminal would write anything.
func (p rerunPlan) pending() bool {
	return len(p.settings)+len(p.files) > 0 || p.accepted && len(p.suggestions) > 0
}

// proposal is a change detection suggests on a rerun.
type proposal struct {
	text string
	done string
	// optIn proposals are applied only after a person confirms them.
	optIn bool
	apply func(*setup)
}

// planRerun compares the existing setup with s, adds what detection
// proposes, and classifies every generated file. acceptOptIn applies opt-in
// proposals; otherwise they are returned as suggestions.
func (r *initRun) planRerun(before, s setup, acceptOptIn bool) (rerunPlan, error) {
	plan := rerunPlan{}
	for _, field := range r.summaryFields(before) {
		after := r.summaryValue(s, field.label)
		if after != field.value {
			plan.proposed = true
			plan.settings = append(plan.settings, planLine{mark: "↻", text: fmt.Sprintf("%s: %s → %s", field.label, field.value, after), done: field.label + ": " + after})
		}
	}
	if before.cfg.Rhizome.BinaryManager != s.cfg.Rhizome.BinaryManager && s.cfg.Rhizome.BinaryManager == obsidian.BinaryManagerExternal {
		plan.settings = append(plan.settings, planLine{mark: "↻", text: "Let an external tool such as mise own the Rhizome binary (--binary-manager external)", done: "An external tool now owns the Rhizome binary"})
	}
	for _, rel := range missingIncludedSubtrees(r.layout.ProjectRoot, s.includeIgnored) {
		plan.settings = append(plan.settings, planLine{mark: "+", text: "Index " + rel + "/ (Git ignores it)", done: "Indexing " + rel + "/ (Git ignores it)"})
	}
	plan.accepted = acceptOptIn
	for _, p := range r.proposals(s) {
		line := planLine{mark: "+", text: p.text, done: p.done}
		if !p.optIn {
			p.apply(&s)
			plan.settings = append(plan.settings, line)
			continue
		}
		plan.suggestions = append(plan.suggestions, line)
		if acceptOptIn {
			p.apply(&s)
		}
	}
	plan.settings = append(plan.settings, r.planSkips(&s)...)
	plan.setup = s

	res, err := r.resolve(s)
	if err != nil {
		return plan, err
	}
	plan.res = res
	if strings.TrimSpace(before.cfg.Rhizome.Version) == "" && res.cfg.Rhizome.Version != "" {
		plan.settings = append(plan.settings, planLine{mark: "+", text: "Pin this repository to Rhizome " + res.cfg.Rhizome.Version, done: "Pinned Rhizome " + res.cfg.Rhizome.Version})
	}

	for _, line := range fileLines(res.files.pending()) {
		if line.mark == "!" || line.mark == "?" {
			plan.decisions = append(plan.decisions, line)
		} else {
			plan.files = append(plan.files, line)
		}
	}

	var support []string
	if len(plan.settings) == 0 {
		// Workflow sections live in .rhizome/workflows.yml.
		files := map[string]bool{}
		for section := range r.configChanges(res.cfg) {
			switch section {
			case sectionWorkflowTmpls, sectionWorkflowAddons, sectionWorkflowMgmt:
				files[".rhizome/workflows.yml"] = true
			default:
				files[".rhizome/config.yml"] = true
			}
		}
		for _, file := range []string{".rhizome/config.yml", ".rhizome/workflows.yml"} {
			if files[file] {
				support = append(support, file)
			}
		}
	}
	root := r.layout.ProjectRoot
	var missing []string
	if data, err := os.ReadFile(filepath.Join(root, ".rhizome", ".gitignore")); errors.Is(err, os.ErrNotExist) {
		missing = append(missing, ".rhizome/.gitignore")
	} else if err != nil || string(data) != rhizomeGitIgnoreBody() {
		support = append(support, ".rhizome/.gitignore")
	}
	if _, err := os.Stat(filepath.Join(root, ".rhizome", "ignore")); errors.Is(err, os.ErrNotExist) {
		missing = append(missing, ".rhizome/ignore")
	}
	if len(missing) > 0 {
		plan.files = append(plan.files, planLine{mark: "+", text: "Create " + joinHumanPaths(missing), done: "Created " + joinHumanPaths(missing)})
	}
	if len(support) > 0 {
		plan.files = append(plan.files, planLine{mark: "↻", text: "Update " + joinHumanPaths(support), done: "Updated " + joinHumanPaths(support)})
	}
	if _, err := os.Stat(filepath.Join(root, obsidian.RhizomeDirName, legacyRejectionsName)); err == nil {
		retired := ".rhizome/" + legacyRejectionsName
		plan.files = append(plan.files, planLine{mark: "-", text: "Remove " + retired + " (generated-files.yml replaces it)", done: "Removed " + retired})
	}
	return plan, nil
}

// planSkips adds what detection proposes to skip to s and describes every
// pending change to .rhizome/ignore skips.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US10-AC3]]
func (r *initRun) planSkips(s *setup) []planLine {
	state := readIgnoreFileState(r.layout.ProjectRoot)
	keep := map[string]bool{}
	for p := range state.keep {
		keep[p] = true
	}
	for _, p := range s.keep {
		keep[cleanSkipPath(p)] = true
	}
	s.skips = mergeSkips(s.skips, detectSkips(r.layout.ProjectRoot, s.cfg.Notes, r.layout.Code.Files, keep))
	written := map[string]bool{}
	for _, sk := range state.skipped {
		written[cleanSkipPath(sk.path)] = true
	}
	var pending []skip
	for _, sk := range s.skips {
		if !written[cleanSkipPath(sk.path)] {
			pending = append(pending, sk)
		}
	}
	s.skips = pending
	lines := skipLines(pending)
	for _, p := range s.keep {
		switch p = cleanSkipPath(p); {
		case written[p]:
			lines = append(lines, planLine{mark: "+", text: "Index " + p + " again", done: "Indexing " + p + " again"})
		case !state.keep[p]:
			lines = append(lines, planLine{mark: "+", text: "Keep " + p + " indexed", done: p + " stays indexed"})
		}
	}
	return lines
}

// mergeSkips adds skips for paths not already listed.
func mergeSkips(current, more []skip) []skip {
	seen := map[string]bool{}
	out := append([]skip(nil), current...)
	for _, sk := range current {
		seen[cleanSkipPath(sk.path)] = true
	}
	for _, sk := range more {
		if p := cleanSkipPath(sk.path); !seen[p] {
			seen[p] = true
			out = append(out, sk)
		}
	}
	return out
}

// fileLines describes generated-file changes, one line per kind.
func fileLines(files pendingFiles) []planLine {
	var lines []planLine
	add := func(mark, text string, paths []string) {
		if len(paths) > 0 {
			lines = append(lines, planLine{mark: mark, text: text, paths: paths})
		}
	}
	add("+", countNoun(len(files.Create), "new Rhizome file"), files.Create)
	add("↻", "Update "+countNoun(len(files.Update), "Rhizome file"), files.Update)
	add("-", "Remove "+countNoun(len(files.Remove), "file")+" Rhizome no longer ships", files.Remove)
	add("!", countNoun(len(files.Edited), "file")+" you edited "+pluralVerb(len(files.Edited), "has", "have")+" a newer version", files.Edited)
	add("!", "Remove "+countNoun(len(files.RemoveWithUpdate), "file")+" Rhizome no longer ships, if you take the update to the skill that links "+pluralVerb(len(files.RemoveWithUpdate), "it", "them"), files.RemoveWithUpdate)
	add("?", "Rhizome can't tell whether "+countNoun(len(files.Unknown), "file")+" "+pluralVerb(len(files.Unknown), "was", "were")+" edited", files.Unknown)
	return lines
}

// proposals lists what detection found that the setup does not cover yet.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US3-AC3]]
func (r *initRun) proposals(s setup) []proposal {
	var out []proposal
	// Folder limits may be deliberate, so widening them waits for a person.
	for _, text := range notesLimitDrift(&s.cfg, r.layout) {
		out = append(out, proposal{text: text, done: "Notes cover all Markdown", optIn: true, apply: func(s *setup) { notesLimitDrift(&s.cfg, r.layout) }})
	}
	codeOn := s.cfg.Code.IndexesCode()
	for _, text := range codeDrift(&s.cfg, r.layout) {
		done := "Code indexing covers the whole repository"
		if !codeOn {
			done = "Code indexing on"
		}
		out = append(out, proposal{text: text, done: done, optIn: true, apply: func(s *setup) { codeDrift(&s.cfg, r.layout) }})
	}
	if embeddingsNeedProviderConfig(s.cfg) {
		provider := inferEmbeddingsProvider(s.cfg.NoteEmbeddings)
		if provider == "" {
			provider = inferEmbeddingsProvider(s.cfg.CodeEmbeddings)
		}
		out = append(out, proposal{
			text:  "Record " + providerDisplayName(provider) + " as the semantic search provider (config had none)",
			done:  "Recorded " + providerDisplayName(provider) + " as the semantic search provider",
			apply: func(s *setup) { recordEmbeddingsProvider(&s.cfg, provider) },
		})
	}
	if s.cfg.NoteEmbeddings == nil && s.cfg.CodeEmbeddings == nil {
		for _, provider := range []string{searchVoyage, searchOpenAI} {
			if searchReady(provider, r.session, nil) {
				out = append(out, proposal{
					text:  "Turn on semantic search with " + providerDisplayName(provider) + " (key found)",
					done:  "Semantic search on (" + providerDisplayName(provider) + ")",
					optIn: true,
					apply: func(s *setup) { applySearchChoice(&s.cfg, provider, true) },
				})
				break
			}
		}
	}
	if addons, names := r.newDefaultAddons(s); len(addons) > 0 {
		out = append(out, proposal{
			text:  "Add " + joinHumanPaths(names) + " (now part of " + workflowLabel(s.workflows, nil) + ")",
			done:  "Added " + joinHumanPaths(names),
			optIn: true,
			apply: func(s *setup) {
				s.cfg.WorkflowTemplateAddons.Enabled = sortedStringSet(stringSet(append(cloneTemplates(s.cfg.WorkflowTemplateAddons.Enabled), addons...)))
			},
		})
	}
	return out
}

// newDefaultAddons lists addons, and their names, that a workflow now turns
// on by default but this repository adopted the workflow before.
func (r *initRun) newDefaultAddons(s setup) ([]string, []string) {
	if !suppressDefaultAddonsForExistingConfig(r.layout, s.cfg) {
		return nil, nil
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil, nil
	}
	ids := defaultAddonsForTemplates(s.workflows, registry)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, firstNonEmpty(registry[id].Name, id))
	}
	return ids, names
}

func recordEmbeddingsProvider(cfg *obsidian.LocalConfig, provider string) {
	if cfg.NoteEmbeddings != nil && cfg.NoteEmbeddings.Enabled && strings.TrimSpace(cfg.NoteEmbeddings.Provider) == "" {
		next := *cfg.NoteEmbeddings
		next.Provider = provider
		cfg.NoteEmbeddings = &next
	}
	if cfg.CodeEmbeddings != nil && cfg.CodeEmbeddings.Enabled && strings.TrimSpace(cfg.CodeEmbeddings.Provider) == "" {
		next := *cfg.CodeEmbeddings
		next.Provider = provider
		cfg.CodeEmbeddings = &next
	}
}

// allMarkdown is the notes default: every Markdown file not ignored.
var allMarkdown = []string{"**/*.md"}

// notesLimitDrift removes notes folder limits when Markdown sits outside
// them, and describes what that indexes. Content init proposes to skip does
// not count.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US3-AC3]]
func notesLimitDrift(cfg *obsidian.LocalConfig, layout DetectedLayout) []string {
	includes := cfg.Notes.Includes
	if !notesLimited(cfg.Notes) {
		return nil
	}
	var outside []string
	for _, rel := range layout.Code.Files {
		if globsMatch(allMarkdown, rel) && !globsMatch(includes, rel) && !globsMatch(cfg.Notes.Excludes, rel) && !proposedSkip(rel) {
			outside = append(outside, rel)
		}
	}
	if len(outside) == 0 {
		return nil
	}
	cfg.Notes.Includes = withoutNotesLimits(includes)
	return []string{"Index Markdown in " + notePlaces(outside) + " (remove the notes folder limits)"}
}

// notesLimited reports whether notes includes leave some Markdown out.
func notesLimited(notes obsidian.LocalVaultConfig) bool {
	return len(notes.Includes) > 0 && !(globsMatch(notes.Includes, "probe/probe.md") && globsMatch(notes.Includes, "probe.md"))
}

// withoutNotesLimits returns includes that cover all Markdown, keeping
// includes for other formats such as effort pages.
func withoutNotesLimits(includes []string) []string {
	next := cloneTemplates(allMarkdown)
	for _, include := range includes {
		if _, folder := markdownDirFromInclude(include); !folder && include != "*.md" {
			next = append(next, include)
		}
	}
	return next
}

// notePlaces names where notes are: up to three top-level folders by note
// count, then top-level files, named when there are one or two.
func notePlaces(files []string) string {
	counts := map[string]int{}
	var folders, top []string
	for _, rel := range files {
		dir, _, nested := strings.Cut(rel, "/")
		if !nested {
			top = append(top, rel)
			continue
		}
		if counts[dir+"/"] == 0 {
			folders = append(folders, dir+"/")
		}
		counts[dir+"/"]++
	}
	sort.SliceStable(folders, func(i, j int) bool { return counts[folders[i]] > counts[folders[j]] })
	places := folders
	if len(folders) > 3 {
		places = append(folders[:3:3], countNoun(len(folders)-3, "more folder"))
	}
	switch {
	case len(top) > 2:
		places = append(places, countNoun(len(top), "top-level file"))
	default:
		sort.Strings(top)
		places = append(places, top...)
	}
	return joinHumanPaths(places)
}

// codeDrift turns code indexing on when config leaves it off, and removes
// code folder limits when detection finds code outside them. It returns a
// line describing each change.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US9-AC2]]
func codeDrift(cfg *obsidian.LocalConfig, layout DetectedLayout) []string {
	if !cfg.Code.IndexesCode() {
		next := cfg.Code
		next.Enabled = true
		cfg.Code = next
		if names := languageNames(layout.Code.Languages); len(names) > 0 {
			return []string{"Turn on code indexing (" + joinHumanPaths(names) + " found)"}
		}
		return []string{"Turn on code indexing, so code added later is indexed"}
	}
	limits := codeFolderLimits(cfg.Code)
	if len(limits) == 0 {
		return nil
	}
	outside := codeOutsideLimits(layout.Code.Files, limits, cfg.Code.DisabledLanguages)
	if len(outside) == 0 {
		return nil
	}
	cfg.Code = removeCodeFolderLimits(cfg.Code)
	where := make([]string, 0, len(outside))
	for _, dir := range outside {
		if dir == "." {
			where = append(where, "top-level files")
		} else {
			where = append(where, dir+"/")
		}
	}
	return []string{"Index code in " + joinHumanPaths(where) + " (remove the code folder limits)"}
}

func rootCovered(configured []string, root string) bool {
	for _, c := range configured {
		c = strings.Trim(path.Clean(filepath.ToSlash(strings.TrimSpace(c))), "/")
		if c == "." || c == "" || c == root || strings.HasPrefix(root, c+"/") {
			return true
		}
	}
	return false
}

func folderNames(rels []string) []string {
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		out = append(out, strings.TrimSuffix(rel, "/")+"/")
	}
	return out
}

// missingIncludedSubtrees lists the Git-ignored folders .rhizome/ignore does
// not include yet.
func missingIncludedSubtrees(projectRoot string, rels []string) []string {
	data, _ := os.ReadFile(filepath.Join(projectRoot, ".rhizome", "ignore"))
	present := stringSet(strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"))
	var out []string
	for _, rel := range dedupePreserveOrder(rels) {
		if line := subtreeNegationLine(rel); line != "" && !present[line] {
			out = append(out, rel)
		}
	}
	return out
}
