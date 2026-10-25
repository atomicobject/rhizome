package hook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// Guard rules refuse a tool call before it runs. A starter declares where
// they live; each is the guard-rule map of a guard finding a person approved,
// a note tracked in Git whose approved hash still matches its rule, and
// which stays active after its action item is checked, since the guard is
// that check. Rules match the normalized event: a command
// rule against the command words of each simple command, a path rule against
// the contained paths the call reads or edits. Anything the dispatcher cannot
// read, and any shell command not built only of plain simple commands, lets
// the call proceed.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6]]

const (
	maxGuardNoteBytes = 256 << 10
	// maxGuardText caps a rule's reason and alternative, which the agent
	// sees verbatim.
	maxGuardText = 300
)

// findingName is the name shards.mjs gives a finding note:
// <response>-<slug>.md. Other files in the folder, such as an editor's
// backup, never hold a rule.
var findingName = regexp.MustCompile(`^(fix|guard|ask)-[a-z0-9]+(-[a-z0-9]+)*\.md$`)

// errEditedRule marks a rule changed since a person approved it.
var errEditedRule = errors.New("edited after approval; revise it through the session-memory review")

// GuardHash is the approved hash shards.mjs writes beside a guard rule: the
// first 16 hex digits of the SHA-256 of the finding's response and the
// rule's fields, joined by newlines, as written. It detects an edit made
// after approval; anyone who can write the repository can also forge it.
func GuardHash(response string, r GuardRule) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{response, r.Tool, r.Command, r.Path, r.Reason, r.Alternative}, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// GuardRule is one approved rule.
type GuardRule struct {
	// Source is the repository-relative note that holds the rule.
	Source      string `yaml:"-"`
	Tool        string `yaml:"tool"`
	Command     string `yaml:"command"`
	Path        string `yaml:"path"`
	Reason      string `yaml:"reason"`
	Alternative string `yaml:"alternative"`
}

// Match describes what the rule refuses, for status.
func (r GuardRule) Match() string {
	if r.Command != "" {
		return r.Tool + " command " + r.Command
	}
	return r.Tool + " path " + r.Path
}

// LoadGuardRules reads the approved rules in a repository-relative folder of
// finding notes, in file order. Only finding notes tracked in Git, staged
// or committed, count, so a backup or a note never added holds no rule. A
// note that carries a rule Rhizome cannot use is skipped and named in
// problems; a missing folder holds no rules.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6-AC1]]
func LoadGuardRules(ctx context.Context, root, dir string) (rules []GuardRule, problems []string) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	tracked, err := trackedNotes(ctx, root, dir)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: list the notes Git tracks: %v", dir, err)}
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !findingName.MatchString(e.Name()) {
			continue
		}
		source := path.Join(dir, e.Name())
		rule, ok, err := readGuardRule(filepath.Join(root, filepath.FromSlash(source)))
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: %v", source, err))
		case ok && !tracked[source]:
			problems = append(problems, source+": not tracked in Git; stage or commit it to activate its rule")
		case ok:
			rule.Source = source
			rules = append(rules, rule)
		}
	}
	return rules, problems
}

// trackedNotes lists the files Git tracks in a repository-relative folder.
func trackedNotes(ctx context.Context, root, dir string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "git", "--literal-pathspecs", "ls-files", "-z", "--", dir+"/")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	tracked := map[string]bool{}
	for _, f := range strings.Split(string(out), "\x00") {
		tracked[f] = f != ""
	}
	return tracked, nil
}

// readGuardRule reads one finding note's rule; ok is false for a note that
// carries none.
func readGuardRule(file string) (GuardRule, bool, error) {
	f, err := os.Open(file)
	if err != nil {
		return GuardRule{}, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxGuardNoteBytes+1))
	if err != nil {
		return GuardRule{}, false, err
	}
	text := string(data)
	if !strings.Contains(text, "guard-rule:") {
		return GuardRule{}, false, nil
	}
	if len(data) > maxGuardNoteBytes {
		return GuardRule{}, false, errors.New("note exceeds 256 KiB")
	}
	front, _, ok := markdownsyntax.Frontmatter(text)
	if !ok {
		return GuardRule{}, false, nil
	}
	var fm struct {
		Response     string     `yaml:"response"`
		ReviewedBy   string     `yaml:"reviewed-by"`
		ApprovedHash string     `yaml:"approved-hash"`
		GuardRule    *GuardRule `yaml:"guard-rule"`
	}
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return GuardRule{}, false, fmt.Errorf("unreadable frontmatter: %w", err)
	}
	if fm.GuardRule == nil {
		return GuardRule{}, false, nil
	}
	r := *fm.GuardRule
	approved := GuardHash(fm.Response, r)
	written := canonicalGuardRule(r) == ruleLine(front)
	r.Command, r.Path = strings.Join(strings.Fields(r.Command), " "), strings.TrimSpace(r.Path)
	switch {
	case fm.Response != "guard":
		return r, false, errors.New("only a guard finding may carry guard-rule")
	case strings.TrimSpace(fm.ReviewedBy) == "":
		return r, false, errors.New("no reviewed-by: a person has not approved it")
	case !written:
		return r, false, errors.New("guard-rule is not the JSON the session-memory scripts write; revise it through the session-memory review")
	case strings.TrimSpace(fm.ApprovedHash) != approved:
		return r, false, errEditedRule
	case r.Tool != ToolRead && r.Tool != ToolEdit && r.Tool != ToolShell && r.Tool != ToolPatch:
		return r, false, fmt.Errorf("guard-rule tool %q must be read, edit, shell, or patch", r.Tool)
	case (r.Command == "") == (r.Path == ""):
		return r, false, errors.New("guard-rule needs exactly one of command or path")
	case r.Command != "" && r.Tool != ToolShell:
		// Only a shell call has command words to match.
		return r, false, fmt.Errorf("guard-rule command %q needs tool shell; a %s rule needs a path", r.Command, r.Tool)
	case strings.ContainsAny(r.Path, `{}[]\`) || strings.HasPrefix(r.Path, "!"):
		// The shard pattern dialect, which approval's breadth check counts
		// in: doublestar would expand what that check read literally.
		return r, false, fmt.Errorf("guard-rule path %q may use only literal names, *, **, and ?", r.Path)
	case r.Path != "" && (!doublestar.ValidatePattern(r.Path) || path.IsAbs(r.Path) || r.Path != path.Clean(r.Path) || strings.HasPrefix(r.Path, "../")):
		return r, false, fmt.Errorf("guard-rule path %q is not a repository-relative glob", r.Path)
	case strings.ContainsAny(r.Command, "'\"\\$`"):
		return r, false, fmt.Errorf("guard-rule command %q must be plain words, without quotes or expansions", r.Command)
	case r.Command != "" && !namesCommand(r.Command):
		return r, false, fmt.Errorf("guard-rule command %q must name a literal command word", r.Command)
	case r.Command != "" && globbed(r.Command):
		return r, false, fmt.Errorf("guard-rule command %q may not use a glob or a leading ~, since the guard never evaluates a command with one", r.Command)
	case strings.TrimSpace(r.Reason) == "" || strings.TrimSpace(r.Alternative) == "":
		return r, false, errors.New("guard-rule needs a reason and an alternative")
	case utf8.RuneCountInString(r.Reason) > maxGuardText || utf8.RuneCountInString(r.Alternative) > maxGuardText:
		return r, false, fmt.Errorf("guard-rule reason and alternative must each be at most %d characters", maxGuardText)
	}
	return r, true, nil
}

// ruleLine is the guard-rule value as written on its frontmatter line.
func ruleLine(front string) string {
	for _, line := range strings.Split(front, "\n") {
		if rest, ok := strings.CutPrefix(line, "guard-rule:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// canonicalGuardRule is the rule as shards.mjs writes it, JSON.stringify of
// guardFields: keys in this order, an empty command or path left out, no
// spaces. Only a rule written so is used, since JSON.parse, which the
// scripts check a rule with, and YAML read other forms differently: a
// duplicate key, which JSON.parse takes the last of, is a YAML error.
func canonicalGuardRule(r GuardRule) string {
	var b strings.Builder
	field := func(key, value string) {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + key + `":`)
		jsString(&b, value)
	}
	field("tool", r.Tool)
	if r.Command != "" {
		field("command", r.Command)
	}
	if r.Path != "" {
		field("path", r.Path)
	}
	field("reason", r.Reason)
	field("alternative", r.Alternative)
	return "{" + b.String() + "}"
}

// jsString writes s quoted as JSON.stringify does: only ", \, and control
// characters are escaped, and those without a short form as lowercase \u00xx.
func jsString(b *strings.Builder, s string) {
	short := map[rune]string{'"': `\"`, '\\': `\\`, '\b': `\b`, '\f': `\f`, '\n': `\n`, '\r': `\r`, '\t': `\t`}
	b.WriteByte('"')
	for _, c := range s {
		switch {
		case short[c] != "":
			b.WriteString(short[c])
		case c < 0x20:
			fmt.Fprintf(b, `\u%04x`, c)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
}

// namesCommand reports whether a command rule's first word after its
// wrappers and assignments is one the guard evaluates (see plainCommands),
// so the rule can ever match: a literal name, not a glob such as * or a
// builtin such as eval.
func namesCommand(command string) bool {
	words := strings.Fields(command)
	for len(words) > 0 && (commandWrappers[words[0]] || isAssignment(shellWord{text: words[0]})) {
		words = words[1:]
	}
	return len(words) > 0 && plainCommandWord(shellWord{text: words[0], plain: len(words[0])})
}

// globbed reports whether a command rule's word holds a glob character or
// starts with ~. Rules are unquoted, so such a rule could match only a
// command that quotes the word, which means something else.
func globbed(command string) bool {
	return slices.ContainsFunc(strings.Fields(command), func(w string) bool {
		return strings.ContainsAny(w, "*?[") || strings.HasPrefix(w, "~")
	})
}

// refuses reports whether the rule forbids the call. unsure is true when a
// command the rule may name was not evaluated, since it is not built only of
// plain simple commands (see plainCommands).
func (r GuardRule) refuses(ev Event) (refused, unsure bool) {
	// An edit rule covers Codex's patch tool, which is how Codex edits.
	if r.Tool != ev.Tool && !(r.Tool == ToolEdit && ev.Tool == ToolPatch) {
		return false, false
	}
	if r.Path != "" {
		match := func(p string) bool {
			ok, _ := doublestar.Match(r.Path, p)
			return ok
		}
		if ev.Tool != ToolShell {
			return slices.ContainsFunc(ev.Paths, match), false
		}
		// A shell read counts only where its directory is certain.
		known, unknown := guardShellReads(ev.Command, ev.cwd, ev.cwdKnown)
		if slices.ContainsFunc(containedPaths(ev.Root, ev.cwd, known), match) {
			return true, false
		}
		return false, slices.ContainsFunc(containedPaths(ev.Root, ev.cwd, unknown), match)
	}
	if ev.Tool != ToolShell {
		return false, false
	}
	want := strings.Fields(r.Command)
	for len(want) > 1 && (commandWrappers[want[0]] || isAssignment(shellWord{text: want[0]})) {
		want = want[1:]
	}
	return commandRuleMatches(ev.Command, want)
}

// refusalMessage is the one message a refused call gets.
func refusalMessage(r GuardRule) string {
	return fmt.Sprintf("Refused by the guard rule in %s. Reason: %s Instead: %s",
		r.Source, strings.TrimSpace(r.Reason), strings.TrimSpace(r.Alternative))
}

// guard applies the approved rules of every guard declaration to a call
// before it runs and refuses it on the first match. It never refuses on its
// own failure: an unreadable rule is skipped and a command it cannot parse
// confidently proceeds, each with a diagnostic.
//
// Docs:
// - [[agent-surface-integration-modes#^SPEC-0045-US6-AC2]]
// - [[agent-surface-integration-modes#^SPEC-0045-US6-AC3]]
func (d *dispatch) guard(handlers []Handler) error {
	var errs []error
	for _, h := range handlers {
		if h.GuardRules == "" || !has(h.Events, PreToolUse) || !has(h.Tools, d.ev.Tool) {
			continue
		}
		if err := d.ctx.Err(); err != nil {
			return errors.Join(append(errs, guardExpired(err))...)
		}
		rules, problems := LoadGuardRules(d.ctx, d.ev.Root, h.GuardRules)
		for _, p := range problems {
			errs = append(errs, fmt.Errorf("guard rule skipped: %s", p))
		}
		for _, r := range rules {
			if err := d.ctx.Err(); err != nil {
				return errors.Join(append(errs, guardExpired(err))...)
			}
			refused, unsure := r.refuses(d.ev)
			if unsure {
				errs = append(errs, fmt.Errorf("guard rule %s not applied: the command is not built only of plain simple commands", r.Source))
			}
			if !refused {
				continue
			}
			// A refusal that arrives after the deadline would come from a
			// guard the host may already have given up on.
			if err := d.ctx.Err(); err != nil {
				return errors.Join(append(errs, guardExpired(err))...)
			}
			if err := writeRefusal(d.opts.Stdout, refusalMessage(r)); err != nil {
				return errors.Join(append(errs, err)...)
			}
			if err := updateStatus(d.ctx, d.ev.Root, func(st *Status) {
				if st.Refused[d.ev.Harness] == nil {
					st.Refused[d.ev.Harness] = map[string]int{}
				}
				st.Refused[d.ev.Harness][r.Source]++
			}); err != nil {
				errs = append(errs, fmt.Errorf("record refusal: %w", err))
			}
			return errors.Join(errs...)
		}
	}
	return errors.Join(errs...)
}

func guardExpired(err error) error {
	return fmt.Errorf("guard stopped at the deadline, so the call proceeds: %w", err)
}

// writeRefusal emits the deny decision Claude Code and Codex both read from
// a PreToolUse hook; Codex ignores a deny without a reason.
func writeRefusal(w io.Writer, reason string) error {
	return writeJSONLine(w, map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            HostEvent(PreToolUse),
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}})
}
