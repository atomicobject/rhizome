package hook

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// Shell commands are parsed, never executed. Only a bounded set of read
// forms yields paths: cat, sed -n, head, tail, nl, and rg/grep file
// operands, optionally after cd. Anything else yields nothing.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC5]]

const (
	maxCommandBytes = 64 << 10
	maxShellPaths   = 200
)

// shellWord is one word with whether any of it came from an unquoted
// expansion or glob, which makes it unusable as a path.
type shellWord struct {
	text    string
	dynamic bool
}

// shellSegments splits a command into simple commands at unquoted &&, ||, ;,
// |, &, and newlines, honoring single quotes, double quotes, backslashes, and
// comments. A segment with a subshell or here-document is dropped.
// A segment in a pipeline or run in the background runs in a subshell, so a cd
// there changes no later command's directory; isolated marks it.
type shellSegment struct {
	words    []shellWord
	isolated bool
}

func shellSegments(cmd string) []shellSegment {
	if len(cmd) > maxCommandBytes {
		return nil
	}
	var segments []shellSegment
	var words []shellWord
	var word strings.Builder
	inWord, dynamic, broken, isolated := false, false, false, false
	depth := 0 // open subshell parentheses
	endWord := func() {
		if inWord {
			words = append(words, shellWord{text: word.String(), dynamic: dynamic})
		}
		word.Reset()
		inWord, dynamic = false, false
	}
	endSegment := func(sepIsolates bool) {
		endWord()
		if len(words) > 0 && !broken {
			segments = append(segments, shellSegment{words: words, isolated: isolated || sepIsolates})
		}
		// Every segment inside a subshell is dropped through its closing ).
		words, broken = nil, depth > 0
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\'':
			end := strings.IndexByte(cmd[i+1:], '\'')
			if end < 0 {
				return segments
			}
			word.WriteString(cmd[i+1 : i+1+end])
			inWord = true
			i += end + 1
		case c == '"':
			inWord = true
			for i++; i < len(cmd) && cmd[i] != '"'; i++ {
				switch {
				case cmd[i] == '\\' && i+1 < len(cmd) && strings.IndexByte("\"\\$`\n", cmd[i+1]) >= 0:
					i++
					word.WriteByte(cmd[i])
				case cmd[i] == '$' || cmd[i] == '`':
					dynamic = true
					word.WriteByte(cmd[i])
				default:
					word.WriteByte(cmd[i])
				}
			}
			if i >= len(cmd) {
				return segments
			}
		case c == '\\' && i+1 < len(cmd):
			i++
			if cmd[i] != '\n' {
				word.WriteByte(cmd[i])
				inWord = true
			}
		case c == ' ' || c == '\t':
			endWord()
		case c == '#' && !inWord:
			// A comment runs to the end of the line.
			for i+1 < len(cmd) && cmd[i+1] != '\n' {
				i++
			}
		case c == '\n' || c == ';' || c == '&' || c == '|':
			double := i+1 < len(cmd) && (c == '&' || c == '|') && cmd[i+1] == c
			single := (c == '&' || c == '|') && !double
			endSegment(single)
			// The command after a single | is the next stage of the pipeline.
			isolated = single && c == '|'
			if double {
				i++
			}
		case c == '(' && !inWord:
			depth++
			broken = true
		case c == ')' && depth > 0:
			depth--
			broken = true
		case c == '(' || c == ')' || c == '<' && strings.HasPrefix(cmd[i:], "<<"):
			broken = true
		case c == '>' || c == '<':
			// A redirect target is not an operand; mark the next word so it
			// is skipped. A file descriptor number before it (2>) goes too.
			if inWord && !dynamic && strings.Trim(word.String(), "0123456789") == "" {
				word.Reset()
				inWord = false
			}
			endWord()
			for i+1 < len(cmd) && (cmd[i+1] == '>' || cmd[i+1] == '&') {
				i++
			}
			words = append(words, shellWord{text: string(c), dynamic: true})
		default:
			if c == '$' || c == '`' || c == '*' || c == '?' || c == '[' || c == '{' || c == '~' {
				dynamic = true
			}
			word.WriteByte(c)
			inWord = true
		}
	}
	endSegment(false)
	return segments
}

// commandSegments splits command text for prefix matching the way the
// session-memory handler does: at &&, ||, ;, |, and newlines, trimmed, with
// whitespace collapsed. It is deliberately not quote-aware so the
// dispatcher's gate and the handler agree.
func commandSegments(cmd string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	var out []string
	for _, seg := range strings.Split(replacer.Replace(cmd), "\n") {
		if seg = strings.Join(strings.Fields(seg), " "); seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// matchesPrefix reports whether any segment of cmd starts with a prefix.
func matchesPrefix(cmd string, prefixes []string) bool {
	for _, seg := range commandSegments(cmd) {
		for _, p := range prefixes {
			if p = strings.Join(strings.Fields(p), " "); p != "" && strings.HasPrefix(seg, p) {
				return true
			}
		}
	}
	return false
}

// shellReads returns the files a command reads in the recognized forms, each
// resolved against the working directory in effect for it.
func shellReads(cmd, cwd string) []string {
	var out []string
	for _, seg := range shellSegments(cmd) {
		words := seg.words
		for len(words) > 0 && isAssignment(words[0]) {
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		head := words[0].text
		head = strings.TrimSuffix(strings.ToLower(head[strings.LastIndexAny(head, `/\`)+1:]), ".exe")
		args := operands(words[1:])
		if head == "cd" {
			// Bash's cd is logical: ".." drops the previous component of the
			// path text, not of the link's target. File operands after it
			// still resolve physically (see paths.RelPhysicalStrict).
			if len(args) == 1 && !args[0].dynamic && !seg.isolated {
				cwd = filepath.Clean(paths.ResolveAgainst(cwd, args[0].text))
			}
			continue
		}
		for _, file := range readOperands(head, args) {
			if len(out) == maxShellPaths {
				return out
			}
			out = append(out, paths.ResolveAgainst(cwd, file))
		}
	}
	return out
}

func isAssignment(w shellWord) bool {
	eq := strings.IndexByte(w.text, '=')
	return eq > 0 && !strings.ContainsAny(w.text[:eq], "/-.")
}

// operands drops redirects and their targets.
func operands(words []shellWord) []shellWord {
	var out []shellWord
	for i := 0; i < len(words); i++ {
		if words[i].dynamic && (words[i].text == ">" || words[i].text == "<") {
			i++
			continue
		}
		out = append(out, words[i])
	}
	return out
}

// Options that take the next word as their value, per command.
var valueOptions = map[string]map[string]bool{
	"head": set("-n", "-c"),
	"tail": set("-n", "-c"),
	"nl":   set("-b", "-d", "-f", "-h", "-i", "-l", "-n", "-s", "-v", "-w"),
	"sed":  set("-e", "-f", "-l"),
	"rg": set("-e", "-f", "-g", "--glob", "--iglob", "-t", "--type", "-T", "--type-not", "-m", "--max-count",
		"-A", "--after-context", "-B", "--before-context", "-C", "--context", "-M", "--max-columns", "-j", "--threads",
		"-d", "--max-depth", "-r", "--replace", "-E", "--encoding", "--sort", "--sortr", "--color", "--colors",
		"--pre", "--pre-glob", "--type-add", "--ignore-file", "--path-separator", "--regexp", "--file"),
	"grep": set("-e", "-f", "-m", "--max-count", "-A", "--after-context", "-B", "--before-context", "-C", "--context",
		"-d", "--directories", "-D", "--devices", "--include", "--exclude", "--exclude-dir", "--label", "--regexp", "--file"),
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

// readOperands returns the file operands of one recognized read form.
func readOperands(head string, args []shellWord) []string {
	switch head {
	case "cat", "nl", "head", "tail", "sed", "rg", "grep":
	default:
		return nil
	}
	takesValue := valueOptions[head]
	var positional []string
	patternGiven, quietSed := false, false
	endOfOptions := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !endOfOptions && a.text == "--" {
			endOfOptions = true
			continue
		}
		if !endOfOptions && strings.HasPrefix(a.text, "--") {
			name, _, attached := strings.Cut(a.text, "=")
			switch {
			case head == "sed" && name == "--in-place":
				// An in-place edit is not a read form.
				return nil
			case head == "sed" && (name == "--quiet" || name == "--silent"):
				quietSed = true
			case head == "rg" && name == "--files":
				patternGiven = true
			}
			if takesValue[name] {
				patternGiven = patternGiven || name == "--regexp" || name == "--file"
				if !attached {
					i++
				}
			}
			continue
		}
		if !endOfOptions && strings.HasPrefix(a.text, "-") && a.text != "-" {
			// A cluster of short flags (-rn, -n5, -e1p): flags up to the
			// first that takes a value, whose value is the rest of the word,
			// or else the next word.
			for j := 1; j < len(a.text); j++ {
				flag := "-" + a.text[j:j+1]
				switch {
				case head == "sed" && flag == "-i":
					return nil
				case head == "sed" && flag == "-n":
					quietSed = true
				}
				if takesValue[flag] {
					patternGiven = patternGiven || flag == "-e" || flag == "-f"
					if j == len(a.text)-1 {
						i++
					}
					break
				}
			}
			continue
		}
		if a.dynamic {
			positional = append(positional, "")
			continue
		}
		positional = append(positional, a.text)
	}
	if head == "sed" && !quietSed {
		return nil
	}
	if (head == "sed" || head == "rg" || head == "grep") && !patternGiven && len(positional) > 0 {
		positional = positional[1:]
	}
	var files []string
	for _, p := range positional {
		if p != "" && p != "-" {
			files = append(files, p)
		}
	}
	return files
}

// patchPaths returns the files an apply_patch body adds, updates, deletes,
// or moves to.
func patchPaths(patch, cwd string) []string {
	var out []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, marker := range []string{"*** Update File:", "*** Add File:", "*** Delete File:", "*** Move to:"} {
			if rest, ok := strings.CutPrefix(line, marker); ok && strings.TrimSpace(rest) != "" && len(out) < maxShellPaths {
				out = append(out, paths.ResolveAgainst(cwd, strings.TrimSpace(rest)))
			}
		}
	}
	return out
}
