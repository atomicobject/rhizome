package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// moduleShortcuts are first words of `rzm <module> <action>` commands. Modules
// resolve at run time from .rhizome/modules/<name>/, not from the command tree,
// so each name here is checked by its own module, never by this test.
var moduleShortcuts = map[string]bool{
	"<module>":    true, // the generic form
	"ao-kb":       true, // the ao-kb starter's module actions
	"compounding": true, // the compounding starter's module recipes and actions
	"pipeline":    true, // the example module in the rhizome skill's references/automation.md
}

var (
	inlineCodeSpan = regexp.MustCompile("`([^`\n]+)`")
	// A placeholder may hold spaces (`<Person title>`); a shell redirect is
	// followed by a space or a second `<`.
	commandPlaceholder = regexp.MustCompile(`<[A-Za-z][^<>]*>`)
	shellFences        = map[string]bool{"bash": true, "sh": true, "shell": true}
)

const placeholderMark = "\x00"

type commandLiteral struct {
	at   string
	text string
}

// SPEC-0080 US8-AC7: an agent copies the commands shipped guidance names, so
// every `rzm` command and long flag in it must exist in the command tree.
func TestShippedGuidanceNamesRealCommandsAndFlags(t *testing.T) {
	root := newRootCommand(commandEnv{Config: testkit.Env(t)}).Command
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	root.InitDefaultVersionFlag()

	var literals []commandLiteral
	for _, dir := range shippedGuidanceDirs(t) {
		require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			literals = append(literals, rzmCommandLiterals(filepath.ToSlash(path), string(content))...)
			return nil
		}))
	}
	require.Greater(t, len(literals), 100, "the scan should find the guidance's rzm commands")

	var failures []string
	for _, literal := range literals {
		if problem := checkCommandLiteral(root, literal.text); problem != "" {
			failures = append(failures, literal.at+": `"+literal.text+"`: "+problem)
		}
	}
	require.Empty(t, failures, "shipped guidance names commands or flags rzm does not have:\n%s", strings.Join(failures, "\n"))
}

func shippedGuidanceDirs(t *testing.T) []string {
	t.Helper()
	starters, err := filepath.Glob(filepath.Join("..", "pkg", "app", "cli", "init", "templates", "starters", "*", "agents"))
	require.NoError(t, err)
	require.NotEmpty(t, starters)
	return append([]string{
		filepath.Join("..", "pkg", "app", "cli", "init", "templates", "skills", "markdown"),
		filepath.Join("..", "docs", "rhizome-md-templates"),
	}, starters...)
}

// rzmCommandLiterals returns inline code spans and shell-fence lines that start
// with `rzm `, joining continued fence lines.
func rzmCommandLiterals(path, content string) []commandLiteral {
	var out []commandLiteral
	fence, shell, pending, pendingLine := "", false, "", 0
	for i, line := range strings.Split(content, "\n") {
		if next, ok := markdownsyntax.FenceDelimiter(line, fence); ok {
			if fence == "" {
				lang, _, _ := strings.Cut(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "`~")), " ")
				shell = shellFences[strings.ToLower(lang)]
			}
			fence, pending = next, ""
			continue
		}
		if fence != "" {
			if !shell {
				continue
			}
			text := strings.TrimPrefix(strings.TrimSpace(line), "$ ")
			if pending != "" {
				text = pending + " " + text
			} else {
				pendingLine = i + 1
			}
			if strings.HasSuffix(text, `\`) {
				pending = strings.TrimSuffix(text, `\`)
				continue
			}
			pending = ""
			if strings.HasPrefix(text, "rzm ") {
				out = append(out, commandLiteral{at: path + ":" + strconv.Itoa(pendingLine), text: text})
			}
			continue
		}
		for _, match := range inlineCodeSpan.FindAllStringSubmatch(line, -1) {
			if strings.HasPrefix(match[1], "rzm ") {
				out = append(out, commandLiteral{at: path + ":" + strconv.Itoa(i+1), text: match[1]})
			}
		}
	}
	return out
}

// checkCommandLiteral resolves a literal's subcommand path and long flags in
// the command tree and returns what is wrong, or "".
func checkCommandLiteral(root *cobra.Command, literal string) string {
	if fields := strings.Fields(literal); len(fields) > 1 && moduleShortcuts[fields[1]] {
		return ""
	}
	args := shellWords(commandPlaceholder.ReplaceAllString(literal, placeholderMark))[1:]
	command, rest, err := root.Find(args)
	if err != nil {
		return err.Error()
	}
	command.InitDefaultHelpFlag()
	// A command that parses its own flags, such as `rzm module`, declares none.
	if command.DisableFlagParsing {
		return ""
	}
	for i := 0; i < len(rest); i++ {
		word := rest[i]
		if word == "--" {
			break
		}
		if !strings.HasPrefix(word, "-") {
			// A placeholder stands for any value, so it is never an unknown command.
			placeholder := strings.Contains(word, placeholderMark) || word == "..." || word == "…"
			if !placeholder && command.HasSubCommands() && !command.Runnable() {
				return "unknown command \"" + word + "\" for \"" + command.CommandPath() + "\""
			}
			continue
		}
		if !strings.HasPrefix(word, "--") {
			continue
		}
		name, _, hasValue := strings.Cut(strings.TrimPrefix(word, "--"), "=")
		flag := command.Flag(name)
		if flag == nil {
			return "unknown flag --" + name + " for \"" + command.CommandPath() + "\""
		}
		// Prose often lists flags without their values (`--intent` and `--file`).
		if !hasValue && flag.NoOptDefVal == "" && i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "--") {
			i++
		}
	}
	return ""
}

// shellWords splits a command line into words, honoring quotes and stopping at
// the first unquoted shell operator (pipe, list, redirect, or comment).
func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	flush := func() {
		if inWord {
			words = append(words, word.String())
		}
		word.Reset()
		inWord = false
	}
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\' && i+1 < len(runes):
			i++
			word.WriteRune(runes[i])
			inWord = true
		case r == ' ' || r == '\t':
			flush()
		case strings.ContainsRune("|&;<>", r) || r == '#' && !inWord:
			flush()
			return words
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return words
}
