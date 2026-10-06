package init

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

// includedSubtreesHeader labels the managed block of subtree negations in
// .rhizome/ignore so humans can find and prune them (SPEC-0064).
const includedSubtreesHeader = "# rhizome: included subtrees"

// withIncludedSubtrees returns content with a `!/<rel>/` negation for each
// of rels under the included subtrees block.
//
// CORRECTNESS TRAP: when .rhizome/ignore is missing or empty, the built-in
// default ignore layer only applies because the file has no rules. Writing a
// bare file with just negations would silently disable the defaults, so
// content without rules first becomes ignore.DefaultIgnoreFile() (see
// pkg/vault/ignore/load.go).
func withIncludedSubtrees(content string, rels []string) string {
	if len(rels) == 0 {
		return content
	}
	if len(strings.TrimSpace(content)) == 0 {
		content = ignore.DefaultIgnoreFile()
	}
	existing := map[string]bool{}
	hasHeader := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == includedSubtreesHeader {
			hasHeader = true
		}
		if line != "" {
			existing[line] = true
		}
	}

	var additions []string
	for _, rel := range rels {
		line := subtreeNegationLine(rel)
		if line == "" || existing[line] {
			continue
		}
		existing[line] = true
		additions = append(additions, line)
	}

	var b strings.Builder
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	if len(additions) > 0 {
		if !hasHeader {
			b.WriteString("\n")
			b.WriteString(includedSubtreesHeader)
			b.WriteString("\n")
		}
		for _, line := range additions {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func includeIgnoredInputs(projectRoot string, inputs []string) ([]string, error) {
	rels := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if isEmptyIncludeIgnoredInput(input) {
			continue
		}
		rel, err := includeIgnoredInput(projectRoot, input)
		if err != nil {
			return nil, err
		}
		rels = append(rels, rel)
	}
	return dedupePreserveOrder(rels), nil
}

func isEmptyIncludeIgnoredInput(input string) bool {
	input = strings.TrimSpace(input)
	return input == "" || input == "[]"
}

func includeIgnoredInput(projectRoot, input string) (string, error) {
	rel, err := validateProjectSubpath(projectRoot, input)
	if err != nil {
		return "", err
	}
	return rel, nil
}

// promptIgnoredSubtreeCandidates collects selected ignored subtrees without
// mutating the project. Init applies the selections only after it knows the
// final agent preferences and has completed collision preflight.
func promptIgnoredSubtreeCandidates(layout *DetectedLayout, reader *bufio.Reader, out io.Writer, defaultInclude bool) ([]string, error) {
	if layout == nil || len(layout.IgnoredRepoCandidates) == 0 {
		return nil, nil
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Ignored subtrees"))
	fmt.Fprintln(out, styleDim(out, "These directories look like real nested repos but are hidden by gitignore. Include any that Rhizome should index."))

	var selected []string
	for _, candidate := range layout.IgnoredRepoCandidates {
		fmt.Fprintf(out, "  %s: %s\n", candidate.Rel, ignoredCandidateSummary(candidate))
		if promptYesNo(reader, out, "Include "+candidate.Rel+" in indexing?", defaultInclude) {
			selected = append(selected, candidate.Rel)
		}
	}
	return dedupePreserveOrder(selected), nil
}

// noticeIgnoredSubtreeCandidates prints a one-line-per-candidate notice for
// non-interactive runs, which cannot prompt to include them. It points at the
// repeatable --include-ignored flag so a wrapper repo's gitignored submodule is
// never silently configured around.
func noticeIgnoredSubtreeCandidates(out io.Writer, candidates []IgnoredRepoCandidate) {
	if out == nil || len(candidates) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Ignored subtrees detected"))
	fmt.Fprintln(out, styleDim(out, "These look like real nested repos but are hidden by gitignore. Rhizome will not index them unless you include them."))
	for _, candidate := range candidates {
		fmt.Fprintf(out, "  %s: %s\n", candidate.Rel, ignoredCandidateSummary(candidate))
	}
	rels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		rels = append(rels, candidate.Rel)
	}
	fmt.Fprintf(out, "  %s --include-ignored %s\n", styleDim(out, "Include with:"), strings.Join(rels, " --include-ignored "))
}

// mergeDeferredIgnoredCodeSuggestions keeps code-indexing prompts useful after
// a user selects an ignored subtree, without writing .rhizome/ignore before
// the final agent-surface collision check. Each subtree is inspected as an
// explicit root, then its suggested roots are rebased to the project.
func mergeDeferredIgnoredCodeSuggestions(layout *DetectedLayout, rels []string) {
	if layout == nil {
		return
	}
	for _, rel := range dedupePreserveOrder(rels) {
		candidate := detectCode(filepath.Join(layout.ProjectRoot, filepath.FromSlash(rel)))
		layout.Code = mergeCodeSuggestions(layout.Code, rebaseCodeSuggestion(candidate, rel))
	}
}

func rebaseCodeSuggestion(suggestion CodeSuggestion, rel string) CodeSuggestion {
	rebaseRoots := func(roots []string) []string {
		out := make([]string, 0, len(roots))
		for _, root := range roots {
			if root == "." || root == "" {
				out = append(out, rel)
				continue
			}
			out = append(out, filepath.ToSlash(filepath.Join(rel, root)))
		}
		return rollupRoots(out)
	}
	suggestion.PythonRoots = rebaseRoots(suggestion.PythonRoots)
	suggestion.GoRoots = rebaseRoots(suggestion.GoRoots)
	suggestion.TSRoots = rebaseRoots(suggestion.TSRoots)
	suggestion.CSharpRoots = rebaseRoots(suggestion.CSharpRoots)
	suggestion.PHPRoots = rebaseRoots(suggestion.PHPRoots)
	files := make([]string, 0, len(suggestion.Files))
	for _, file := range suggestion.Files {
		files = append(files, path.Join(rel, file))
	}
	suggestion.Files = files
	return suggestion
}

func mergeCodeSuggestions(base, addition CodeSuggestion) CodeSuggestion {
	base.EnableCodeRefs = base.EnableCodeRefs || addition.EnableCodeRefs
	base.Languages = dedupePreserveOrder(append(base.Languages, addition.Languages...))
	base.Scan = dedupePreserveOrder(append(base.Scan, addition.Scan...))
	base.Ignore = dedupePreserveOrder(append(base.Ignore, addition.Ignore...))
	base.Files = dedupePreserveOrder(append(base.Files, addition.Files...))
	base.FileCount += addition.FileCount
	base.PythonRoots = rollupRoots(append(base.PythonRoots, addition.PythonRoots...))
	base.GoRoots = rollupRoots(append(base.GoRoots, addition.GoRoots...))
	base.TSRoots = rollupRoots(append(base.TSRoots, addition.TSRoots...))
	base.CSharpRoots = rollupRoots(append(base.CSharpRoots, addition.CSharpRoots...))
	base.PHPRoots = rollupRoots(append(base.PHPRoots, addition.PHPRoots...))
	if len(base.PythonScan) == 0 {
		base.PythonScan = addition.PythonScan
	}
	if len(base.GoScan) == 0 {
		base.GoScan = addition.GoScan
	}
	if len(base.TSScan) == 0 {
		base.TSScan = addition.TSScan
	}
	if len(base.CSharpScan) == 0 {
		base.CSharpScan = addition.CSharpScan
	}
	if len(base.PHPScan) == 0 {
		base.PHPScan = addition.PHPScan
	}
	return base
}

func ignoredCandidateSummary(candidate IgnoredRepoCandidate) string {
	var parts []string
	if candidate.HasGit {
		parts = append(parts, ".git")
	}
	if len(candidate.Markers) > 0 {
		parts = append(parts, strings.Join(candidate.Markers, ", "))
	}
	if candidate.Rule != nil {
		parts = append(parts, candidate.Rule.Source+":"+fmt.Sprint(candidate.Rule.Line))
	}
	if len(parts) == 0 {
		return "gitignored candidate"
	}
	return strings.Join(parts, "; ")
}

// subtreeNegationLine renders the durable negation line for a root-relative path.
func subtreeNegationLine(rel string) string {
	rel = strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
	if rel == "" {
		return ""
	}
	return "!/" + rel + "/"
}

// validateProjectSubpath resolves input (absolute or root-relative) to a
// root-relative slash path, requiring it to exist as a directory under the
// project root.
func validateProjectSubpath(projectRoot, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs := input
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(projectRoot, filepath.FromSlash(input))
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(projectRoot, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is not under the project root", input)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("path %q does not exist under the project root", input)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", input)
	}
	return filepath.ToSlash(rel), nil
}
