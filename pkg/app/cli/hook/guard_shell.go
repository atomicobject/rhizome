package hook

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// How a guard rule reads a shell command: only one built of plain simple
// commands is evaluated, for command rules and shell path rules alike.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6-AC3]]

// plainCommands returns the simple commands of cmd, each without its leading
// assignments, when cmd is built only of plain simple commands: a guard
// evaluates nothing else. That is, cmd contains no expansion (a $ or
// backquote outside single quotes, even inside double quotes or an argument),
// its grammar is complete, and each segment is zero or more literal NAME=value
// assignments, then a command word that is a literal, unquoted name or path
// and no builtin but cd, true, false, echo, and pwd (see plainCommandWord),
// joined to the next by &&, ||, ;, |, &, or a newline. No word anywhere,
// argument or redirect target included, holds an unquoted glob (*, ?, [) or
// starts with an unquoted ~: zsh aborts the whole command when a glob
// matches nothing, and either may expand to words the guard cannot see.
// Anything else is not evaluated at all: rather than list the constructs that
// may change what a later word means or where it runs (eval, functions,
// aliases, if...fi, printf -v, and so on), the guard reads only commands
// that can do neither. That loses refusals such as
// `command -v go && go mod vendor`, deliberately: a false refusal is worse
// than a missed one.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6-AC3]]
func plainCommands(cmd string) (segments []shellSegment, ok bool) {
	segments, complete := shellSegments(cmd)
	if !complete {
		return nil, false
	}
	for i, seg := range segments {
		words := seg.words
		if slices.ContainsFunc(words, func(w shellWord) bool { return w.glob }) {
			return nil, false
		}
		for len(words) > 0 && plainAssignment.MatchString(words[0].text) && !words[0].dynamic &&
			len(plainAssignment.FindString(words[0].text)) <= words[0].plain {
			words = words[1:]
		}
		if len(words) == 0 || !plainCommandWord(words[0]) {
			return nil, false
		}
		segments[i].words = words
	}
	return segments, true
}

// plainAssignment is an assignment's unquoted NAME= or NAME+=.
var plainAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\+?=`)

// plainName is a literal command name or path; a leading - is zsh's
// precommand modifier.
var plainName = regexp.MustCompile(`^[A-Za-z0-9._/+][A-Za-z0-9._/+-]*$`)

// plainCommandWord reports whether a command word is literal, unquoted, and
// neither a reserved word nor a builtin other than cd, true, false, echo,
// and pwd.
func plainCommandWord(w shellWord) bool {
	return !w.dynamic && w.plain == len(w.text) && plainName.MatchString(w.text) && !notPlain[commandName(w.text)]
}

// The reserved words and builtins of bash (enable -a, compgen -k) and zsh
// (builtins, reswords, and those of its common modules), then wrappers that
// run other words; cd, true, false, echo, and pwd are left out. test and [
// are listed: in zsh and bash, `test -v 'path[functions[go]=1]'` assigns as
// it tests.
// lib/verify.mjs in the compounding starter holds the same list.
var notPlain = set(strings.Fields(`
	! [[ ]] { } case coproc do done elif else end esac fi for foreach function if in nocorrect repeat select then time until while
	. : alias bg bind break builtin caller command compgen complete compopt continue declare dirs disown enable eval exec exit
	export fc fg getopts hash help history jobs kill let local logout mapfile popd printf pushd read readarray readonly return
	set shift shopt source suspend times trap type typeset ulimit umask unalias unset wait test [
	- autoload bindkey bye chdir compadd comparguments compcall compctl compdescribe compfiles compgroups compquote compset
	comptags comptry compvalues disable echotc echoti emulate float functions getln integer limit log nameref noglob print
	private pushln r rehash sched setopt ttyctl unfunction unhash unlimit unsetopt vared whence where which zcompile zformat
	zle zmodload zparseopts zregexparse zstyle
	cap clone getcap setcap pcre_compile pcre_match pcre_study strftime sysopen sysread sysseek syswrite zsystem zselect zpty
	zsocket ztcp zprof zcurses zstat zgetattr zsetattr zdelattr zlistattr ztie zuntie zgdbmpath zftp
	env sudo doas nohup nice timeout xargs sh bash zsh dash ksh fish
`)...)

// guardShellReads splits the files a shell command reads, resolved against
// cwd, into those whose directory is certain and those whose directory is
// not. Only a command built of plain simple commands (see plainCommands) has
// certain reads. The directory is certain from the start only when the host
// reports where the command runs (cwdKnown); an absolute path is always
// certain. A plain `cd` to one literal directory, optionally after -- or
// -L, moves it, and keeps it certain along the unbroken && chain that
// follows, since those commands run only if it succeeded; an absolute one
// makes it certain. Only an absolute directory or one starting . or .. is
// certain: CDPATH, which the hook cannot see, may send `cd sub` anywhere.
// Only the bare word cd is the builtin; ./cd is a program in a child
// process. Any other cd makes the directory uncertain from there: one that
// is conditional (after ||), in a pipeline, backgrounded, or followed by ;
// or a newline. pushd, popd, and chdir are builtins, so not plain.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6-AC3]]
func guardShellReads(cmd, cwd string, cwdKnown bool) (known, unknown []string) {
	segments, ok := plainCommands(cmd)
	if !ok {
		return nil, shellReads(cmd, cwd)
	}
	certain, moved := cwdKnown, false
	for i, seg := range segments {
		before := ""
		if i > 0 {
			before = segments[i-1].next
		}
		// After a cd, only its && chain is sure the cd succeeded.
		if moved && before != "&&" {
			certain = false
		}
		args := operands(seg.words[1:])
		if seg.words[0].text == "cd" {
			moved = true
			dir, ok := cdTarget(args)
			// A cd after || or | runs only sometimes, or in a subshell.
			if !ok || before == "||" || before == "|" {
				certain = false
				continue
			}
			// CDPATH, which the hook cannot see, may send a cd to a bare
			// relative name (sub, pkg/..) anywhere; it never applies to an
			// absolute path or one starting . or ..
			if !filepath.IsAbs(dir) && dir != "." && dir != ".." && !strings.HasPrefix(dir, "./") && !strings.HasPrefix(dir, "../") {
				certain = false
			}
			certain = certain || filepath.IsAbs(dir)
			cwd = filepath.Clean(paths.ResolveAgainst(cwd, dir))
			continue
		}
		for _, file := range readOperands(commandName(seg.words[0].text), args) {
			if p := paths.ResolveAgainst(cwd, file); certain || filepath.IsAbs(file) {
				known = append(known, p)
			} else {
				unknown = append(unknown, p)
			}
		}
	}
	return known, unknown
}

// Words a command rule may start with that run the command after them; the
// rule matches that command. A shell command starting with one is not plain,
// so it is never matched.
var commandWrappers = set("env", "command", "builtin", "exec", "time", "nohup", "sudo")

// commandRuleMatches reports whether a simple command in cmd starts with the
// rule's words, so `go mod vendor` matches `cd x && go mod vendor -v` but not
// `echo "go mod vendor"`. Only a command built of plain simple commands (see
// plainCommands) is matched; unsure says whether the rule's words appear in
// another anyway.
func commandRuleMatches(cmd string, want []string) (matched, unsure bool) {
	if len(want) == 0 {
		return false, false
	}
	mentioned := strings.Contains(strings.Join(strings.Fields(cmd), " "), strings.Join(want, " "))
	segments, ok := plainCommands(cmd)
	if !ok {
		return false, mentioned
	}
	for _, seg := range segments {
		// A redirect may sit between arguments: `go mod >log vendor` runs
		// `go mod vendor`.
		words := append(seg.words[:1:1], operands(seg.words[1:])...)
		if len(words) < len(want) {
			continue
		}
		head, name := words[0].text, want[0]
		if !strings.ContainsAny(name, `/\`) {
			head, name = commandName(head), commandName(name)
		}
		ok := head == name
		for i := 1; ok && i < len(want); i++ {
			ok = words[i].text == want[i]
		}
		if ok {
			return true, false
		}
	}
	return false, false
}
