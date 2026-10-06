// Index scope: the rules that decide which paths Rhizome indexes, layer by
// layer, and the edits `rzm index scope` and the desktop app make to
// .rhizome/ignore. A first run plans .rhizome/ignore with the same
// transforms it writes with, so the planned rules are the written ones.
//
// Docs: [[desktop-repository-setup#^SPEC-0118-US2]]
package init

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Scope lists the rules that decide which paths Rhizome indexes, layer by
// layer in evaluation order. Hidden files and folders are never indexed,
// and the last matching rule wins.
type Scope struct {
	// BuiltInApplies is true while .rhizome/ignore has no rules, so the
	// built-in list is in effect.
	BuiltInApplies bool        `json:"builtInApplies"`
	Rules          []ScopeRule `json:"rules"`
	// KeepIndexed lists paths marked to stay indexed, which init never
	// proposes to skip.
	KeepIndexed []string `json:"keepIndexed"`
}

// ScopeRule is one rule and where it comes from.
type ScopeRule struct {
	// Layer is default (the built-in list), gitignore, rhizome, or config.
	Layer   string `json:"layer"`
	Source  string `json:"source,omitempty"`
	Line    int    `json:"line,omitempty"`
	Pattern string `json:"pattern"`
	// Folder is the folder a nested .gitignore applies to.
	Folder string `json:"folder,omitempty"`
	// Reason is why init suggested skipping the path.
	Reason string `json:"reason,omitempty"`
	// Included marks a negation that indexes what an earlier rule ignores.
	Included bool `json:"included,omitempty"`
	// Planned marks a rule a first run will write.
	Planned bool `json:"planned,omitempty"`
}

// ScopeEdits are changes to .rhizome/ignore, applied together.
type ScopeEdits struct {
	Skip           []string // folders or files to skip, relative to the root
	RemoveRules    []string // rules to remove, by their exact text
	IncludeIgnored []string // folders .gitignore excludes that Rhizome should index
}

func (e ScopeEdits) empty() bool {
	return len(e.Skip) == 0 && len(e.RemoveRules) == 0 && len(e.IncludeIgnored) == 0
}

// ReadScope reports the rules for a configured folder. excludes are the
// configuration's notes excludes.
func ReadScope(root string, excludes []string) Scope {
	data, _ := os.ReadFile(ignoreFilePath(root))
	return scopeFrom(ignore.LoadUnifiedMatcher(root, excludes), string(data), string(data))
}

// ChangeScope applies edits to .rhizome/ignore in one write and reports the
// resulting rules. When any edit fails, the file is unchanged.
func ChangeScope(root string, excludes []string, edits ScopeEdits) (Scope, error) {
	if edits.empty() {
		return ReadScope(root, excludes), nil
	}
	file := ignoreFilePath(root)
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Scope{}, err
	}
	content := string(data)
	if !hasIgnoreRules(content) {
		legacy, _ := os.ReadFile(filepath.Join(root, ".obsidianignore"))
		if hasIgnoreRules(string(legacy)) {
			return Scope{}, fmt.Errorf("this folder's rules are in the legacy .obsidianignore; move them to .rhizome/ignore first")
		}
	}
	if content, err = applyScopeEdits(root, withBuiltInList(content), edits); err != nil {
		return Scope{}, err
	}
	if content != string(data) {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return Scope{}, err
		}
		if err := obsidian.WriteFileAtomic(file, []byte(content), 0o644); err != nil {
			return Scope{}, err
		}
	}
	return ReadScope(root, excludes), nil
}

func applyScopeEdits(root, content string, edits ScopeEdits) (string, error) {
	for _, rule := range edits.RemoveRules {
		var err error
		if content, err = withoutRule(content, rule); err != nil {
			return "", err
		}
	}
	var skips []skip
	for _, input := range edits.Skip {
		rel, err := existingSkipPath(root, input)
		if err != nil {
			return "", err
		}
		skips = append(skips, skip{path: rel, reason: reasonManual})
	}
	content = withSkipChanges(content, skips, nil)
	rels, err := includeIgnoredInputs(root, edits.IncludeIgnored)
	if err != nil {
		return "", err
	}
	return withIncludedSubtrees(content, rels), nil
}

// withoutRule removes every line whose text is rule. A suggested skip also
// loses its reason line and is marked to stay indexed, so init does not
// propose it again.
func withoutRule(content, rule string) (string, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" || strings.HasPrefix(rule, "#") {
		return "", fmt.Errorf("%q is not a rule", rule)
	}
	var out []string
	found, suggested, inSection := false, false, false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == suggestedSkipsHeader:
			inSection = true
		case trimmed == "":
			inSection = false
		case trimmed == rule:
			found = true
			if inSection {
				// withSkipChanges removes it together with its reason line.
				suggested = true
				out = append(out, line)
			}
			continue
		}
		out = append(out, line)
	}
	if !found {
		return "", fmt.Errorf("%s is not a rule in .rhizome/ignore", rule)
	}
	content = strings.Join(out, "\n")
	if suggested {
		content = withSkipChanges(content, nil, []string{unescapeSkipPattern(rule)})
	}
	return content, nil
}

// plannedIgnore returns .rhizome/ignore as apply writes it for s, and the
// file's current content ("" when it is missing).
func plannedIgnore(root string, s setup) (planned, current string, exists bool, err error) {
	data, err := os.ReadFile(ignoreFilePath(root))
	switch {
	case errors.Is(err, os.ErrNotExist):
		planned = ignore.DefaultIgnoreFile()
	case err != nil:
		return "", "", false, err
	default:
		current, exists, planned = string(data), true, string(data)
		if len(s.includeIgnored) > 0 || len(s.skips) > 0 || len(s.keep) > 0 {
			planned = withBuiltInList(planned)
		}
	}
	planned = withSkipChanges(withIncludedSubtrees(planned, s.includeIgnored), s.skips, s.keep)
	return planned, current, exists, nil
}

// withBuiltInList returns content with the built-in list first when content
// has no rules. The built-in list applies only while the file has no rules,
// so it must be written before the first rule; comments are kept.
func withBuiltInList(content string) string {
	switch {
	case hasIgnoreRules(content):
		return content
	case strings.TrimSpace(content) == "":
		return ignore.DefaultIgnoreFile()
	}
	return ignore.DefaultIgnoreFile() + "\n" + content
}

// writeIgnore writes .rhizome/ignore for s: the built-in list when the file
// is missing, folders to include, and skip changes.
func writeIgnore(root string, s setup) error {
	planned, current, exists, err := plannedIgnore(root, s)
	if err != nil || (exists && planned == current) {
		return err
	}
	return obsidian.WriteFileAtomic(ignoreFilePath(root), []byte(planned), 0o644)
}

// plannedScope reports the rules after a first run writes s.
func plannedScope(root string, s setup) (Scope, error) {
	planned, current, _, err := plannedIgnore(root, s)
	if err != nil {
		return Scope{}, err
	}
	m := ignore.LoadMatcherWithRhizomeLines(root, strings.Split(planned, "\n"), s.cfg.Notes.Excludes)
	return scopeFrom(m, planned, current), nil
}

// scopeFrom describes m's rules. content is the .rhizome/ignore the matcher
// holds, and rules missing from onDisk are planned.
func scopeFrom(m *ignore.Matcher, content, onDisk string) Scope {
	state := parseIgnoreFileState(content)
	existing := map[string]bool{}
	for _, line := range strings.Split(onDisk, "\n") {
		existing[strings.TrimSpace(line)] = true
	}
	scope := Scope{Rules: []ScopeRule{}, KeepIndexed: []string{}}
	for _, r := range m.Rules() {
		rule := ScopeRule{Layer: r.Layer, Source: r.Source, Line: r.Line, Pattern: r.Pattern}
		switch r.Layer {
		case ignore.LayerDefault:
			scope.BuiltInApplies = true
			rule.Line = 0
		case ignore.LayerGitignore:
			if dir := path.Dir(r.Source); dir != "." {
				rule.Folder = dir
			}
		case ignore.LayerRhizome:
			rule.Included = strings.HasPrefix(r.Pattern, "!")
			if r.Source == ".rhizome/ignore" {
				rule.Reason = state.reasons[r.Line]
				rule.Planned = !existing[strings.TrimSpace(r.Pattern)]
			}
		case ignore.LayerConfig:
			rule.Source, rule.Line = ".rhizome/config.yml", 0
		}
		scope.Rules = append(scope.Rules, rule)
	}
	for p := range state.keep {
		scope.KeepIndexed = append(scope.KeepIndexed, p)
	}
	sort.Strings(scope.KeepIndexed)
	return scope
}

func ignoreFilePath(root string) string {
	return filepath.Join(root, ".rhizome", "ignore")
}

func hasIgnoreRules(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return true
		}
	}
	return false
}
