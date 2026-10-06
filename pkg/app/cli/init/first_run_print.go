package init

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

// printFindings shows what Rhizome found and will set up, in plain words.
func printFindings(out io.Writer, cfg obsidian.LocalConfig, layout DetectedLayout, harnesses AgentHarnesses, skips []skip, provider string, ready bool, workflow string) {
	name := filepath.Base(layout.ProjectRoot)
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Set up Rhizome in "+name))
	fmt.Fprintln(out, "Rhizome indexes this repository's docs and code so agents can search them,")
	fmt.Fprintln(out, "and installs guidance that teaches your agents to use it.")
	fmt.Fprintln(out)
	printFinding(out, "Docs", docsFinding(cfg.Notes, layout))
	printFinding(out, "Code", codeFinding(layout))
	if len(skips) > 0 {
		printFinding(out, "Skip", skipSummary(skips))
	}
	printFinding(out, "Agents", agentsFinding(harnesses))
	printFinding(out, "Search", searchFinding(provider, ready))
	if workflow != "" {
		printFinding(out, "Workflow", workflow)
	}
}

func printFinding(out io.Writer, label, value string) {
	fmt.Fprintf(out, "  %s %s\n", styleLabel(out, fmt.Sprintf("%-9s", label)), value)
}

func docsFinding(notes obsidian.LocalVaultConfig, layout DetectedLayout) string {
	includes := notes.Includes
	if len(includes) == 0 {
		includes = allMarkdown
	}
	var files []string
	for _, path := range layout.Code.Files {
		if globsMatch(includes, path) && !globsMatch(notes.Excludes, path) {
			files = append(files, path)
		}
	}
	if !notesLimited(notes) {
		if len(files) == 0 {
			return "all Markdown (no notes yet)"
		}
		return fmt.Sprintf("all Markdown (%s in %s)", countNoun(len(files), "note"), notePlaces(files))
	}
	// Folder includes say where notes are limited to; other patterns, such
	// as the effort-page include, do not.
	var places []string
	for _, include := range includes {
		if include == "*.md" {
			places = append(places, "top-level Markdown files")
		} else if dir, ok := markdownDirFromInclude(include); ok {
			places = append(places, dir)
		}
	}
	where := "notes matching " + strings.Join(includes, ", ")
	if len(places) > 0 {
		where = joinHumanPaths(places) + " only"
	}
	if len(files) == 0 {
		return where + " (no notes yet)"
	}
	return fmt.Sprintf("%s (%s)", where, countNoun(len(files), "note"))
}

// noteCount gives the note count and any folder limit in a few words.
func noteCount(notes obsidian.LocalVaultConfig, layout DetectedLayout) string {
	full := docsFinding(notes, layout)
	if notesLimited(notes) {
		return full
	}
	count, _, _ := strings.Cut(strings.TrimPrefix(full, "all Markdown ("), " in ")
	return strings.TrimSuffix(count, ")")
}

func globsMatch(globs []string, path string) bool {
	for _, glob := range globs {
		if ok, err := doublestar.Match(glob, path); err == nil && ok {
			return true
		}
	}
	return false
}

func codeFinding(layout DetectedLayout) string {
	names := languageNames(layout.Code.Languages)
	if len(names) == 0 {
		return "none yet"
	}
	return fmt.Sprintf("%s (%s)", strings.Join(names, ", "), countNoun(layout.Code.FileCount, "file"))
}

// languageNames turns detected language ids into display names.
func languageNames(ids []string) []string {
	display := map[string]string{
		"go": "Go", "python": "Python", "typescript": "TypeScript", "javascript": "JavaScript",
		"csharp": "C#", "php": "PHP", "java": "Java", "rust": "Rust", "ruby": "Ruby", "cpp": "C/C++", "shell": "Shell",
	}
	set := stringSet(ids)
	var names []string
	for _, id := range ids {
		if id == "javascript" && set["typescript"] {
			continue
		}
		if name, ok := display[id]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func agentsFinding(h AgentHarnesses) string {
	var names []string
	if h.HasClaude {
		names = append(names, "Claude Code")
	}
	if h.HasCodex {
		names = append(names, "Codex")
	}
	if h.HasCursor {
		names = append(names, "Cursor")
	}
	switch {
	case len(names) > 0:
		return strings.Join(names, ", ")
	case h.HasAgents || h.HasAgentSkills:
		return "AGENTS.md and shared skills (read by Codex, Cursor, and most agents)"
	default:
		return "none"
	}
}

func searchFinding(provider string, ready bool) string {
	switch {
	case provider == searchOff:
		return "off"
	case ready:
		return providerDisplayName(provider)
	default:
		return providerDisplayName(provider) + " (needs a key)"
	}
}

func readyLabel(ready bool, yes, no string) string {
	if ready {
		return yes
	}
	return no
}

// printSetupSummary reports a first run grouped by purpose, then the next
// steps.
func printSetupSummary(out io.Writer, report syncReport, templates []string, skips []skip, savedKey string) {
	fmt.Fprintln(out)
	check := styleOK(out, "✓")
	for _, line := range setupSummaryLines(report, templates, skips, savedKey) {
		fmt.Fprintf(out, "  %s %s\n", check, line)
	}
	printKeptAndWarnings(out, report)
}

// setupSummaryLines groups a first run's writes by purpose.
func setupSummaryLines(report syncReport, templates []string, skips []skip, savedKey string) []string {
	paths := append(append([]string{}, report.Created...), report.Updated...)
	var guidance, skillNames, harnesses, docDirs []string
	schema := false
	for _, path := range paths {
		switch {
		case path == "AGENTS.md" || path == "CLAUDE.md":
			guidance = appendUnique(guidance, path)
		case strings.HasPrefix(path, ".agents/skills/") || strings.HasPrefix(path, ".claude/skills/"):
			parts := strings.Split(path, "/")
			skillNames = appendUnique(skillNames, parts[2])
			harnesses = appendUnique(harnesses, parts[0])
		case strings.HasPrefix(path, ".rhizome/"):
			schema = true
		case strings.HasPrefix(path, "docs/"):
			parts := strings.Split(path, "/")
			if len(parts) > 2 {
				docDirs = appendUnique(docDirs, parts[0]+"/"+parts[1])
			}
		}
	}
	lines := []string{".rhizome/config.yml"}
	if len(guidance) > 0 {
		lines = append(lines, "Rhizome guidance in "+joinHumanPaths(guidance))
	}
	if len(skillNames) > 0 {
		lines = append(lines, fmt.Sprintf("%s in %s", countNoun(len(skillNames), "skill"), joinHumanPaths(harnessDirs(harnesses))))
	}
	if len(docDirs) > 0 {
		if len(docDirs) > 3 {
			docDirs = append(docDirs[:3], "more")
		}
		lines = append(lines, fmt.Sprintf("%s docs in %s", workflowName(templates), joinHumanPaths(docDirs)))
	}
	if schema {
		lines = append(lines, "Schema, saved queries, and views in .rhizome/")
	}
	if len(skips) > 0 {
		lines = append(lines, fmt.Sprintf("Skipped %s in .rhizome/ignore", skipSummary(skips)))
	}
	if savedKey != "" {
		lines = append(lines, savedKey+" saved to ~/.config/rhizome/config.yml")
	}
	return lines
}

// printNextSteps closes a first run with what to do now.
func printNextSteps(out io.Writer, report syncReport, hint string, needIndex bool) {
	notes := []string{"Commit " + joinHumanPaths(commitPaths(report)) + " so your team shares this setup."}
	if hint != "" {
		notes = append([]string{hint}, notes...)
	}
	var steps [][2]string
	if needIndex {
		steps = append(steps, [2]string{"rzm index", "build the search index"})
	}
	printNext(out, steps, notes...)
}

// commitPaths lists the top-level paths a first run wrote, for the commit
// reminder.
func commitPaths(report syncReport) []string {
	commit := []string{".rhizome/"}
	seen := map[string]bool{}
	for _, path := range append(append([]string{}, report.Created...), report.Updated...) {
		top, _, nested := strings.Cut(path, "/")
		if nested {
			top += "/"
		}
		if top == ".rhizome/" || seen[top] {
			continue
		}
		seen[top] = true
		commit = append(commit, top)
	}
	return commit
}

func harnessDirs(harnesses []string) []string {
	out := make([]string, 0, len(harnesses))
	for _, h := range harnesses {
		out = append(out, h+"/")
	}
	sort.Strings(out)
	return out
}

func appendUnique(values []string, value string) []string {
	if contains(values, value) {
		return values
	}
	return append(values, value)
}

func workflowName(templates []string) string {
	switch {
	case contains(templates, templateComplexDomain):
		return "Agentic Engineering and domain modeling"
	case contains(templates, templateAgenticEngineering):
		return "Agentic Engineering"
	default:
		return "Workflow"
	}
}

// printKeptAndWarnings lists files left as they are and paths skipped.
func printKeptAndWarnings(out io.Writer, report syncReport) {
	if len(report.Kept) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Left files that may have local edits as they are (run rzm init in a terminal to review Rhizome's changes):")
		for _, path := range report.Kept {
			fmt.Fprintf(out, "  - %s\n", path)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(out, "%s: %s\n", styleWarn(out, "Warning"), warning)
	}
}
