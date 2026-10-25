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
// expansion or glob, which makes it unusable as a path, whether it holds an
// unquoted glob character (*, ?, [) or starts with an unquoted ~, and how
// many of its leading bytes were neither quoted nor escaped.
type shellWord struct {
	text    string
	dynamic bool
	glob    bool
	plain   int
}

// A segment in a pipeline or run in the background runs in a subshell, so a cd
// there changes no later command's directory; isolated marks it.
type shellSegment struct {
	words    []shellWord
	isolated bool
	// next is the operator after the segment: "&&", "||", ";", "|", "&",
	// "\n", or "" at the end.
	next string
}

// shellSegments splits a command into simple commands at unquoted &&, ||, ;,
// |, &, and newlines, honoring single quotes, double quotes, backslashes, and
// comments. Segments inside a subshell are dropped, and parsing stops at a
// here-document. complete is false when either happened, a quote never
// closed, a $ or backquote outside single quotes may expand, or the grammar is
// broken: an empty command before an operator or at the end after &&, ||, or
// |, a redirect without a target, or a malformed redirect operator such as
// >>> or >&&. Only the guard reads complete.
func shellSegments(cmd string) (segments []shellSegment, complete bool) {
	if len(cmd) > maxCommandBytes {
		return nil, false
	}
	complete = true
	var words []shellWord
	var word strings.Builder
	inWord, dynamic, glob, broken, isolated := false, false, false, false, false
	operand := false // the last operator needs a command after it
	depth := 0       // open subshell parentheses
	plain := -1      // where the word's first quote or escape began
	quote := func() {
		if plain < 0 {
			plain = word.Len()
		}
	}
	endWord := func() {
		if inWord {
			if plain < 0 {
				plain = word.Len()
			}
			words = append(words, shellWord{text: word.String(), dynamic: dynamic, glob: glob, plain: plain})
		}
		word.Reset()
		inWord, dynamic, glob, plain = false, false, false, -1
	}
	endSegment := func(op string) {
		endWord()
		switch {
		case broken:
			complete = false
		case len(words) == 0:
			// Blank lines and comments are fine, and a line may end after
			// &&, ||, or |; bash rejects any other empty command. zsh
			// runs some, which the guard then gives up on.
			if op != "\n" && (op != "" || operand) {
				complete = false
			}
		default:
			for j, w := range words {
				if redirect(w) && (j+1 == len(words) || redirect(words[j+1])) {
					complete = false
				}
			}
			segments = append(segments, shellSegment{words: words, isolated: isolated || op == "&" || op == "|", next: op})
			operand = op == "&&" || op == "||" || op == "|"
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
				return segments, false
			}
			quote()
			word.WriteString(cmd[i+1 : i+1+end])
			inWord = true
			i += end + 1
		case c == '"':
			quote()
			inWord = true
			for i++; i < len(cmd) && cmd[i] != '"'; i++ {
				switch {
				case cmd[i] == '\\' && i+1 < len(cmd) && strings.IndexByte("\"\\$`\n", cmd[i+1]) >= 0:
					i++
					word.WriteByte(cmd[i])
				case cmd[i] == '$' || cmd[i] == '`':
					dynamic, complete = true, false
					word.WriteByte(cmd[i])
				default:
					word.WriteByte(cmd[i])
				}
			}
			if i >= len(cmd) {
				return segments, false
			}
		case c == '\\' && i+1 < len(cmd):
			i++
			if cmd[i] != '\n' {
				quote()
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
		case c == '\n' || c == ';' || c == '|' || c == '&' && !strings.HasPrefix(cmd[i:], "&>"):
			op := string(c)
			if i+1 < len(cmd) && (c == '&' || c == '|') && cmd[i+1] == c {
				i++
				op += op
			}
			endSegment(op)
			// The command after a single | is the next stage of the pipeline.
			isolated = op == "|"
		case c == '(' && !inWord:
			depth++
			broken = true
		case c == ')' && depth > 0:
			depth--
			broken = true
		case c == '<' && strings.HasPrefix(cmd[i:], "<<"):
			// A here-document's body is literal text, not commands, and its
			// end is not worth parsing for: stop at the first one.
			return segments, false
		case c == '(' || c == ')':
			broken = true
		case c == '>' || c == '<' || c == '&':
			// A redirect target is not an operand; mark the next word so it
			// is skipped. A file descriptor number before it (2>) goes too;
			// &> redirects both outputs.
			if c != '&' && inWord && !dynamic && strings.Trim(word.String(), "0123456789") == "" {
				word.Reset()
				inWord = false
			}
			endWord()
			// The whole run of operator characters is one token, so >>> or
			// >&& is not read as a redirect followed by something else.
			end := i + 1
			for end < len(cmd) && strings.IndexByte("<>&|", cmd[end]) >= 0 {
				end++
			}
			if !redirectOps[cmd[i:end]] {
				complete = false
			}
			i = end - 1
			words = append(words, shellWord{text: strings.ReplaceAll(string(c), "&", ">"), dynamic: true})
		default:
			if c == '$' || c == '`' {
				complete = false
			}
			if c == '$' || c == '`' || c == '*' || c == '?' || c == '[' || c == '{' || c == '~' {
				dynamic = true
			}
			if c == '*' || c == '?' || c == '[' || c == '~' && !inWord {
				glob = true
			}
			word.WriteByte(c)
			inWord = true
		}
	}
	endSegment("")
	return segments, complete
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
	known := true
	segments, _ := shellSegments(cmd)
	for _, seg := range segments {
		words := seg.words
		for len(words) > 0 && isAssignment(words[0]) {
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		args := operands(words[1:])
		// Only the bare word cd is the builtin; ./cd is a program, which
		// cannot change the shell's directory.
		if words[0].text == "cd" {
			if seg.isolated {
				continue
			}
			// Bash's cd is logical: ".." drops the previous component of the
			// path text, not of the link's target. File operands after it
			// still resolve physically (see paths.RelPhysicalStrict). A cd
			// this cannot follow leaves the directory unknown, and later
			// relative reads are dropped rather than guessed.
			// After && the next command runs only if the cd succeeded; after ;
			// or a newline it runs anyway, but a cd that failed leaves its
			// reads failing too, and agents write `cd <dir>; cat ...` all the
			// time. Delivery guesses there; the guard does not (guardShellReads).
			// After || the next command runs only if the cd failed.
			if dir, ok := cdTarget(args); ok && (seg.next == "&&" || seg.next == ";" || seg.next == "\n") && (known || rooted(dir)) {
				cwd, known = filepath.Clean(paths.ResolveAgainst(cwd, dir)), true
			} else {
				known = false
			}
			continue
		}
		for _, file := range readOperands(commandName(words[0].text), args) {
			if len(out) == maxShellPaths {
				return out
			}
			if !known && !rooted(file) {
				continue
			}
			out = append(out, paths.ResolveAgainst(cwd, file))
		}
	}
	return out
}

// rooted reports whether a path names its directory without the working
// directory: absolute, or on Windows rooted at the current drive.
func rooted(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)
}

// cdTarget returns the directory a cd changes to when it can be followed:
// one literal operand, after "--" or the logical -L option. -P, -e, "-"
// (the previous directory), and anything expanded cannot be.
func cdTarget(args []shellWord) (string, bool) {
	for len(args) > 0 && !args[0].dynamic && (args[0].text == "-L" || args[0].text == "--") {
		end := args[0].text == "--"
		args = args[1:]
		if end {
			break
		}
	}
	if len(args) != 1 || args[0].dynamic || args[0].text == "-" || strings.HasPrefix(args[0].text, "-") && args[0].text != "" {
		return "", false
	}
	return args[0].text, true
}

// commandName is a command word without its directory or .exe suffix.
func commandName(word string) string {
	return strings.TrimSuffix(strings.ToLower(word[strings.LastIndexAny(word, `/\`)+1:]), ".exe")
}

func isAssignment(w shellWord) bool {
	eq := strings.IndexByte(w.text, '=')
	return eq > 0 && !strings.ContainsAny(w.text[:eq], "/-.")
}

// The redirect operators bash and zsh share; any other run of <, >, &, and |
// leaves the command incomplete.
var redirectOps = set("<", ">", ">>", ">|", "<>", "<&", ">&", "&>", "&>>")

// redirect reports whether a word is the mark shellSegments leaves for a
// redirect operator.
func redirect(w shellWord) bool {
	return w.dynamic && (w.text == ">" || w.text == "<")
}

// operands drops redirects and their targets.
func operands(words []shellWord) []shellWord {
	var out []shellWord
	for i := 0; i < len(words); i++ {
		if redirect(words[i]) {
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
// or moves to. Codex resolves them against the turn's directory, which is
// the hook's cwd, unless an "*** Environment ID:" line sends the patch to
// another environment, whose files are not this worktree's.
func patchPaths(patch, cwd string) []string {
	var out []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "*** Environment ID:") {
			return nil
		}
		for _, marker := range []string{"*** Update File:", "*** Add File:", "*** Delete File:", "*** Move to:"} {
			if rest, ok := strings.CutPrefix(line, marker); ok && strings.TrimSpace(rest) != "" && len(out) < maxShellPaths {
				out = append(out, paths.ResolveAgainst(cwd, strings.TrimSpace(rest)))
			}
		}
	}
	return out
}
