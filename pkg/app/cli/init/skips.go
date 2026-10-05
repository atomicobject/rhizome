// Skip suggestions: tracked content that would bloat the index without
// helping search, proposed for .rhizome/ignore with a short reason each.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US10]]
package init

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	suggestedSkipsHeader = "# rhizome: suggested skips"
	keepIndexedPrefix    = "# rhizome: keep indexed "
	largeFileBytes       = 1 << 20
)

const (
	reasonVendored = "vendored or generated folder"
	reasonFixtures = "test fixtures"
	reasonBundled  = "minified or bundled"
	reasonGenCode  = "generated code"
	reasonManual   = "skipped in settings"
)

// skip is one path init proposes, or was asked, to leave out of the index.
type skip struct {
	path   string // root-relative slash path; folders end with "/"
	reason string
	files  int // indexable files under a folder
}

// skipFolderNames name folders that usually hold checked-in third-party or
// generated code, or test fixtures, with the reason init gives.
var skipFolderNames = map[string]string{
	"third_party": reasonVendored, "third-party": reasonVendored, "external": reasonVendored,
	"extern": reasonVendored, "Pods": reasonVendored, "generated": reasonVendored,
	"__generated__": reasonVendored, ".next": reasonVendored, ".nuxt": reasonVendored,
	".svelte-kit": reasonVendored,
	"testdata":    reasonFixtures, "fixtures": reasonFixtures, "__fixtures__": reasonFixtures,
	"__snapshots__": reasonFixtures,
}

var (
	bundledSuffixes   = []string{".min.js", ".min.css", ".bundle.js", ".chunk.js"}
	generatedSuffixes = []string{".pb.go", "_pb2.py", ".g.dart", ".designer.cs"}
)

// trackedFiles lists the files Git tracks under projectRoot, relative to it.
// It returns nil when Git cannot list them, and callers then consider every
// detected file.
var trackedFiles = func(projectRoot string) map[string]bool {
	cmd := exec.Command("git", "ls-files", "-z", "--cached")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	tracked := map[string]bool{}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" {
			tracked[filepath.ToSlash(rel)] = true
		}
	}
	return tracked
}

// detectSkips proposes tracked, indexable files that match a conservative
// rule: a vendored or generated folder name, a minified or bundled file, a
// generated-code file, or a file larger than 1 MB. files are the paths
// detection walked, which already excludes ignored paths. Paths in keep are
// never proposed.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US10-AC1]]
func detectSkips(projectRoot string, notes obsidian.LocalVaultConfig, files []string, keep map[string]bool) []skip {
	tracked := trackedFiles(projectRoot)
	includes := notes.Includes
	if len(includes) == 0 {
		includes = []string{"**/*.md"}
	}
	indexable := func(rel string) bool {
		return sourceFileExtensions[strings.ToLower(path.Ext(rel))] ||
			globsMatch(includes, rel) && !globsMatch(notes.Excludes, rel)
	}

	folders := map[string]*skip{}
	var out []skip
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	for _, rel := range sorted {
		if tracked != nil && !tracked[rel] || !indexable(rel) {
			continue
		}
		if dir, reason := skipFolder(rel); dir != "" {
			if keep[strings.TrimSuffix(dir, "/")] {
				continue
			}
			if folders[dir] == nil {
				folders[dir] = &skip{path: dir, reason: reason}
			}
			folders[dir].files++
			continue
		}
		if keep[rel] {
			continue
		}
		if reason := fileSkipReason(projectRoot, rel); reason != "" {
			out = append(out, skip{path: rel, reason: reason})
		}
	}
	dirs := make([]string, 0, len(folders))
	for dir := range folders {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	result := make([]skip, 0, len(dirs)+len(out))
	for _, dir := range dirs {
		result = append(result, *folders[dir])
	}
	return append(result, out...)
}

// skipFolder returns the shallowest folder of rel that init proposes to
// skip, with a trailing slash, and the reason.
func skipFolder(rel string) (string, string) {
	parts := strings.Split(rel, "/")
	for i, part := range parts[:len(parts)-1] {
		if reason, ok := skipFolderNames[part]; ok {
			return strings.Join(parts[:i+1], "/") + "/", reason
		}
	}
	return "", ""
}

func fileSkipReason(projectRoot, rel string) string {
	switch {
	case hasAnySuffix(rel, bundledSuffixes):
		return reasonBundled
	case hasAnySuffix(rel, generatedSuffixes) || hasGeneratedHeader(filepath.Join(projectRoot, filepath.FromSlash(rel))):
		return reasonGenCode
	}
	info, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	if err == nil && info.Size() > largeFileBytes {
		return "large file, " + humanSize(info.Size())
	}
	return ""
}

func hasAnySuffix(rel string, suffixes []string) bool {
	lower := strings.ToLower(rel)
	for _, suffix := range suffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// hasGeneratedHeader reports whether a source file's first line marks it as
// generated, following the Go convention other generators also use.
func hasGeneratedHeader(file string) bool {
	if !sourceFileExtensions[strings.ToLower(filepath.Ext(file))] {
		return false
	}
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	line, _, _ := bytes.Cut(head[:n], []byte("\n"))
	return bytes.Contains(line, []byte("Code generated")) && bytes.Contains(line, []byte("DO NOT EDIT"))
}

func humanSize(n int64) string {
	return fmt.Sprintf("%.0f MB", float64(n)/float64(1<<20))
}

// skipSummary describes skips in one line: folders with file counts, files
// grouped by reason.
func skipSummary(skips []skip) string {
	var parts []string
	byReason := map[string][]skip{}
	var reasons []string
	for _, s := range skips {
		if strings.HasSuffix(s.path, "/") {
			if s.files > 0 {
				parts = append(parts, fmt.Sprintf("%s (%s)", s.path, countNoun(s.files, "file")))
			} else {
				parts = append(parts, s.path)
			}
			continue
		}
		if byReason[s.reason] == nil {
			reasons = append(reasons, s.reason)
		}
		byReason[s.reason] = append(byReason[s.reason], s)
	}
	for _, reason := range reasons {
		group := byReason[reason]
		if len(group) == 1 {
			parts = append(parts, fmt.Sprintf("%s (%s)", group[0].path, reason))
			continue
		}
		switch reason {
		case reasonBundled:
			parts = append(parts, fmt.Sprintf("%d minified or bundled files", len(group)))
		case reasonGenCode:
			parts = append(parts, fmt.Sprintf("%d generated files", len(group)))
		case reasonManual:
			parts = append(parts, fmt.Sprintf("%d files you chose", len(group)))
		default:
			parts = append(parts, fmt.Sprintf("%d files over 1 MB", len(group)))
		}
	}
	return joinHumanPaths(parts)
}

// skipLines describes skips for the change list: one line per folder and one
// per kind of file.
func skipLines(skips []skip) []planLine {
	var lines []planLine
	byReason := map[string][]string{}
	var reasons []string
	for _, s := range skips {
		if strings.HasSuffix(s.path, "/") {
			text := "Skip " + s.path + " (" + s.reason
			if s.files > 0 {
				text += ", " + countNoun(s.files, "file")
			}
			lines = append(lines, planLine{mark: "-", text: text + ")", done: "Skipping " + s.path})
			continue
		}
		if byReason[s.reason] == nil {
			reasons = append(reasons, s.reason)
		}
		byReason[s.reason] = append(byReason[s.reason], s.path)
	}
	for _, reason := range reasons {
		paths := byReason[reason]
		if len(paths) == 1 {
			lines = append(lines, planLine{mark: "-", text: fmt.Sprintf("Skip %s (%s)", paths[0], reason), done: "Skipping " + paths[0]})
			continue
		}
		what := countNoun(len(paths), "file")
		lines = append(lines, planLine{mark: "-", text: fmt.Sprintf("Skip %s (%s)", what, reason), done: fmt.Sprintf("Skipping %s (%s)", what, reason), paths: paths})
	}
	return lines
}

// ignoreFileState is what .rhizome/ignore says about suggested skips.
type ignoreFileState struct {
	skipped []skip          // entries in the suggested skips section, with their reasons
	keep    map[string]bool // paths marked keep indexed
}

func readIgnoreFileState(projectRoot string) ignoreFileState {
	state := ignoreFileState{keep: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(projectRoot, ".rhizome", "ignore"))
	if err != nil {
		return state
	}
	inSection := false
	reason := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, keepIndexedPrefix):
			state.keep[cleanSkipPath(strings.TrimPrefix(trimmed, keepIndexedPrefix))] = true
		case trimmed == suggestedSkipsHeader:
			inSection = true
		case trimmed == "":
			inSection = false
		case !inSection:
		case strings.HasPrefix(trimmed, "# "):
			reason = strings.TrimPrefix(trimmed, "# ")
		case !strings.HasPrefix(trimmed, "#"):
			state.skipped = append(state.skipped, skip{path: unescapeSkipPattern(trimmed), reason: firstNonEmpty(reason, reasonManual)})
			reason = ""
		}
	}
	return state
}

// cleanSkipPath turns a typed or written path into the root-relative form
// used for comparisons, without a trailing slash.
func cleanSkipPath(p string) string {
	rel, err := paths.CleanRelPath(strings.Trim(filepath.ToSlash(strings.TrimSpace(p)), "/"))
	if err != nil {
		return ""
	}
	return string(rel)
}

// writeSkipChanges adds skips to the suggested skips section of
// .rhizome/ignore, removes entries for paths in keep, and records keep lines
// so init does not propose those paths again. The file already exists.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US10-AC2]]
func writeSkipChanges(projectRoot string, add []skip, keep []string) error {
	if len(add) == 0 && len(keep) == 0 {
		return nil
	}
	file := filepath.Join(projectRoot, ".rhizome", "ignore")
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	keepSet := map[string]bool{}
	for _, p := range keep {
		keepSet[cleanSkipPath(p)] = true
	}

	// Find the section, dropping kept entries and the reason line above each.
	start, end := -1, len(lines)
	var section []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if trimmed == suggestedSkipsHeader {
				start = i
			}
			continue
		}
		if trimmed == "" {
			end = i
			break
		}
		if !strings.HasPrefix(trimmed, "#") && keepSet[cleanSkipPath(unescapeSkipPattern(trimmed))] {
			if n := len(section); n > 0 && strings.HasPrefix(section[n-1], "# ") && !strings.HasPrefix(section[n-1], "# rhizome:") {
				section = section[:n-1]
			}
			continue
		}
		section = append(section, line)
	}

	present := map[string]bool{}
	for _, line := range section {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			present[trimmed] = true
		}
	}
	for _, s := range add {
		pattern := skipPattern(s.path)
		if present[pattern] || keepSet[cleanSkipPath(s.path)] {
			continue
		}
		present[pattern] = true
		section = append(section, "# "+s.reason, pattern)
	}
	for _, p := range keep {
		line := keepIndexedPrefix + cleanSkipPath(p)
		if !present[line] {
			present[line] = true
			section = append(section, line)
		}
	}

	var out []string
	if start < 0 {
		out = append(out, lines...)
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, suggestedSkipsHeader)
		out = append(out, section...)
	} else {
		out = append(out, lines[:start+1]...)
		out = append(out, section...)
		out = append(out, lines[end:]...)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

// skipPattern anchors a root-relative path for .rhizome/ignore, escaping
// gitignore wildcards and a trailing space.
func skipPattern(rel string) string {
	var b strings.Builder
	b.WriteString("/")
	for _, r := range rel {
		if strings.ContainsRune(`*?[\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	p := b.String()
	if strings.HasSuffix(p, " ") {
		p = strings.TrimSuffix(p, " ") + `\ `
	}
	return p
}

func unescapeSkipPattern(pattern string) string {
	var b strings.Builder
	escaped := false
	for _, r := range strings.TrimPrefix(pattern, "/") {
		if r == '\\' && !escaped {
			escaped = true
			continue
		}
		escaped = false
		b.WriteRune(r)
	}
	return b.String()
}
