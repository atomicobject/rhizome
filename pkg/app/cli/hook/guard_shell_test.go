package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A guard evaluates a command only when it contains no expansions, every
// segment is a plain non-builtin command (or cd, true, false, echo, pwd),
// and its grammar is complete; anything else proceeds.
func TestGuardsEvaluateOnlyCommandsTheyFullyUnderstand(t *testing.T) {
	for _, tc := range []struct {
		cmd             string
		matched, unsure bool
	}{
		// An expansion anywhere, even inside double quotes or an argument,
		// may run or redefine anything: in bash the first is one echo, and
		// in zsh the second makes go a function.
		{`echo "$(printf ";go mod vendor; true ")"`, false, true},
		{`echo "${functions[go]::=print harmless}"; go mod vendor`, false, true},
		{"echo `true`; go mod vendor", false, true},
		{"go mod vendor > $LOG", false, true},
		{`go mod vendor "$X"`, false, true},
		{`echo $'x'; go mod vendor`, false, true},
		// Escaped, quoted, or commented, a $ expands nothing.
		{`echo \$HOME '$X'; go mod vendor # costs $5`, true, false},
		{`echo "\$HOME"; go mod vendor`, true, false},
		// A tilde, brace, or glob in the command word may name anything. A
		// glob anywhere aborts the whole command in zsh when it matches
		// nothing, and a leading tilde may expand to anything.
		{"~/go mod vendor", false, true},
		{"{true,go} mod vendor; go mod vendor", false, true},
		{"g? mod vendor; go mod vendor", false, true},
		{"g* mod vendor; go mod vendor", false, true},
		{"go mod vendor *.go", false, true},
		{"go mod vendor x?", false, true},
		{"go mod vendor [ab]", false, true},
		{"ls nothing* ; go mod vendor", false, true},
		{"go mod vendor > *.log", false, true},
		{"go mod vendor ~/x", false, true},
		{"go mod vendor ~", false, true},
		// Quoted or escaped, or a tilde inside a word, none of these expand.
		{"go mod vendor ./...", true, false},
		{`go mod vendor '*.go' "x?" \[ab]`, true, false},
		{"git diff HEAD~1; go mod vendor", true, false},
		{`go mod vendor "~/x" \~`, true, false},
		// Every builtin may change what a later word means, except these.
		{`printf -v 'functions[go]' '%s' 'print harmless'; go mod vendor`, false, true},
		{`print -v 'functions[go]' 'print harmless'; go mod vendor`, false, true},
		{`read 'functions[go]' <<< 'print harmless'; go mod vendor`, false, true},
		{"vared x; go mod vendor", false, true},
		{"zle -N go; go mod vendor", false, true},
		{"let x=1; go mod vendor", false, true},
		{"pushd sub && go mod vendor", false, true},
		{"/usr/bin/printf x; go mod vendor", false, true},
		{"true && go mod vendor", true, false},
		{"false || go mod vendor", true, false},
		{"echo hi; pwd; go mod vendor", true, false},
		// test and [ evaluate subscripts, which may assign.
		{"test -v 'path[functions[go]=1]' && go mod vendor", false, true},
		{"[ -v 'path[functions[go]=1]' ] && go mod vendor", false, true},
		{"test -f go.mod && go mod vendor", false, true},
		{"/bin/test -f go.mod && go mod vendor", false, true},
		{`"[" -f go.mod ] && go mod vendor`, false, true},
		{"cd sub && go mod vendor", true, false},
		// A command the shell rejects runs nothing.
		{"go mod vendor &&", false, true},
		{"&& go mod vendor", false, true},
		{"go mod vendor || || true", false, true},
		{"; go mod vendor", false, true},
		{"go mod vendor;;", false, true},
		{"go mod vendor & ; true", false, true},
		{"go mod vendor |", false, true},
		{"go mod vendor >", false, true},
		{"go mod vendor 2>", false, true},
		{"go mod vendor > ; true", false, true},
		{"go mod vendor > > log", false, true},
		{"go mod vendor >&", false, true},
		{"go mod vendor >>> log", false, true},
		{"go mod vendor >&& true", false, true},
		{"true >&&go mod vendor", false, true},
		{"go mod vendor <<< x", false, true},
		{"go mod vendor >|| true", false, true},
		{"go mod vendor &>& log", false, true},
		// These are complete.
		{"go mod vendor;", true, false},
		{"go mod vendor &", true, false},
		{"true &&\ngo mod vendor", true, false},
		{"true && # why\ngo mod vendor", true, false},
		{"\n\ngo mod vendor\n", true, false},
		{"go mod vendor >log 2>&1", true, false},
		{"go mod vendor >>log 2>&1|cat", true, false},
		{"go mod vendor &>>log <in", true, false},
		{"go mod vendor >|log 2>&-", true, false},
	} {
		matched, unsure := commandRuleMatches(tc.cmd, strings.Fields("go mod vendor"))
		require.Equal(t, [2]bool{tc.matched, tc.unsure}, [2]bool{matched, unsure}, tc.cmd)
	}
}

// Shell path rules read commands the same way.
func TestShellPathRulesReadOnlyCommandsTheyFullyUnderstand(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "docs", "x.md")
	for _, cmd := range []string{
		`echo "$(printf ";cat docs/x.md; true ")"`,
		`echo "${functions[cat]::=print harmless}"; cat docs/x.md`,
		`printf -v 'functions[cat]' '%s' 'print harmless'; cat docs/x.md`,
		`print -v 'functions[cat]' 'print harmless'; cat docs/x.md`,
		"cat docs/x.md &&",
		"cat docs/x.md >",
		"cat docs/x.md > $OUT",
		"cat docs/x.md >>> log",
		"cat docs/x.md docs/*.md",
		"cat docs/x.md ~/notes.md",
		"[ -f docs/x.md ] && cat docs/x.md",
		"test -d docs && cat docs/x.md",
	} {
		known, _ := guardShellReads(cmd, root, true)
		require.Empty(t, known, cmd)
	}
	for _, cmd := range []string{"true && cat docs/x.md", "cat docs/x.md;"} {
		known, _ := guardShellReads(cmd, root, true)
		require.Equal(t, []string{want}, known, cmd)
	}
}
