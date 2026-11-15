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
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// exampleModule is the module the rhizome skill's references/automation.md
// declares as an example; no starter ships it.
const exampleModule = "pipeline"

var (
	inlineCodeSpan = regexp.MustCompile("`([^`\n]+)`")
	// A placeholder may hold spaces (`<Person title>`); a shell redirect is
	// followed by a space or a second `<`.
	commandPlaceholder = regexp.MustCompile(`<[A-Za-z][^<>]*>`)
	// A file name or flag inside `<...>` means the span is an attached
	// redirect running to a later `>`, as in `<script.js --flag > out.json`.
	redirectSpan    = regexp.MustCompile(`--|/|\.[A-Za-z]`)
	shellFences     = map[string]bool{"bash": true, "sh": true, "shell": true}
	shellAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

const placeholderMark = "\x00"

// markPlaceholders replaces each `<placeholder>` with placeholderMark and
// leaves attached redirects for the shell splitter.
func markPlaceholders(line string) string {
	return commandPlaceholder.ReplaceAllStringFunc(line, func(span string) string {
		if redirectSpan.MatchString(span) {
			return span
		}
		return placeholderMark
	})
}

type commandLiteral struct {
	at   string
	text string
}

// SPEC-0080 US8-AC7: an agent copies the commands shipped guidance names, so
// every `rzm` command and long flag in it must exist in the command tree, and
// every shipped module command must exist in its starter's module.
func TestShippedGuidanceNamesRealCommandsAndFlags(t *testing.T) {
	check := newCommandLiteralChecker(t)
	for _, tc := range []struct {
		literal string
		wrong   bool
	}{
		{literal: "rzm agent start --intent \"<task>\" --file <path>"},
		{literal: "rzm compounding item --ref proposal:<cluster>"},
		{literal: "rzm <module> <action>"},
		{literal: "rzm next-id", wrong: true},
		{literal: "rzm status", wrong: true},
		{literal: "rzm agent start --bogus", wrong: true},
		{literal: "rzm agent surface && rzm agent nonexistent", wrong: true},
		{literal: "rzm compounding itme --ref <ref>", wrong: true},
		{literal: "rzm compounding item --reference <ref>", wrong: true},
		{literal: "rzm agent code --help 2>&1 | head"},
		{literal: "rzm agent code execute < script.js --bogus", wrong: true},
		{literal: "rzm agent code execute <script.js --bogus", wrong: true},
		{literal: "rzm agent code execute <script.js --bogus > out.json", wrong: true},
		{literal: "rzm agent code execute <script.js > out.json"},
		{literal: "rzm agent code execute <<'JS' 2>&1 >>log --bogus", wrong: true},
	} {
		require.Equal(t, tc.wrong, check(tc.literal) != "", "check(%q) = %q", tc.literal, check(tc.literal))
	}

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
		if problem := check(literal.text); problem != "" {
			failures = append(failures, literal.at+": `"+literal.text+"`: "+problem)
		}
	}
	require.Empty(t, failures, "shipped guidance names commands or flags rzm does not have:\n%s", strings.Join(failures, "\n"))
}

// newCommandLiteralChecker returns a check of a command line's `rzm`
// invocations against the command tree and, for `rzm <module>`, against the
// tree `rzm module` builds from each starter's shipped module.
func newCommandLiteralChecker(t *testing.T) func(literal string) string {
	t.Helper()
	root := newRootCommand(commandEnv{Config: testkit.Env(t)}).Command
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	root.InitDefaultVersionFlag()

	vault := t.TempDir()
	moduleDirs, err := filepath.Glob(filepath.Join("..", "pkg", "app", "cli", "init", "templates", "starters", "*", "module"))
	require.NoError(t, err)
	require.NotEmpty(t, moduleDirs)
	modules := map[string]*cobra.Command{}
	for _, dir := range moduleDirs {
		name := filepath.Base(filepath.Dir(dir))
		require.NoError(t, os.CopyFS(filepath.Join(vault, ".rhizome", "modules", name), os.DirFS(dir)))
	}
	for _, dir := range moduleDirs {
		name := filepath.Base(filepath.Dir(dir))
		tree, err := (&vaultCommandOptions{}).moduleCommandTree(obsidian.VaultDefinition{Name: "starters", Path: vault}, name)
		require.NoError(t, err)
		modules[name] = tree
	}

	return func(literal string) string {
		for _, words := range shellCommands(markPlaceholders(literal)) {
			if len(words) == 0 || words[0] != "rzm" {
				continue
			}
			args := words[1:]
			if len(args) > 0 && args[0] == "module" {
				args = args[1:]
			}
			tree := root
			if len(args) > 0 && !isBuiltinCommand(root, args[0]) {
				if strings.Contains(args[0], placeholderMark) || args[0] == exampleModule {
					continue
				}
				if modules[args[0]] != nil {
					tree = modules[args[0]]
				}
			}
			if problem := checkCommandArgs(tree, args); problem != "" {
				return problem
			}
		}
		return ""
	}
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

// rzmCommandLiterals returns inline code spans and shell-fence lines that run
// `rzm`, joining continued fence lines.
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
			if invokesRzm(text) {
				out = append(out, commandLiteral{at: path + ":" + strconv.Itoa(pendingLine), text: text})
			}
			continue
		}
		for _, match := range inlineCodeSpan.FindAllStringSubmatch(line, -1) {
			if invokesRzm(match[1]) {
				out = append(out, commandLiteral{at: path + ":" + strconv.Itoa(i+1), text: match[1]})
			}
		}
	}
	return out
}

// checkCommandArgs resolves an invocation's subcommand path and long flags in
// a command tree and returns what is wrong, or "".
func checkCommandArgs(root *cobra.Command, args []string) string {
	command, rest, err := root.Find(args)
	if err != nil {
		return err.Error()
	}
	command.InitDefaultHelpFlag()
	// A command that parses its own flags declares none.
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
			if !placeholder && command.HasSubCommands() && (!command.Runnable() || command.ValidateArgs([]string{word}) != nil) {
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

// invokesRzm reports whether a command line runs `rzm` with arguments.
func invokesRzm(text string) bool {
	for _, words := range shellCommands(markPlaceholders(text)) {
		if len(words) > 1 && words[0] == "rzm" {
			return true
		}
	}
	return false
}

// shellCommands splits a command line into the words of each command,
// honoring quotes. Pipes and lists (`|`, `||`, `&&`, `;`, `&`) separate
// commands, a redirect's operator and target word are dropped, and a comment
// ends the line. Leading variable assignments are dropped.
func shellCommands(line string) [][]string {
	var commands [][]string
	var words []string
	var word strings.Builder
	inWord, target := false, false
	var quote rune
	flush := func() {
		if inWord {
			switch {
			case target:
				target = false
			case len(words) > 0 || !shellAssignment.MatchString(word.String()):
				words = append(words, word.String())
			}
		}
		word.Reset()
		inWord = false
	}
	end := func() {
		flush()
		if len(words) > 0 {
			commands = append(commands, words)
		}
		words, target = nil, false
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
		case r == '<' || r == '>' || r == '&' && i+1 < len(runes) && runes[i+1] == '>':
			// A file descriptor written against the operator (`2>`) is part of it.
			if inWord && word.Len() > 0 && strings.Trim(word.String(), "0123456789") == "" {
				word.Reset()
				inWord = false
			}
			flush()
			for i+1 < len(runes) && strings.ContainsRune("<>&|", runes[i+1]) {
				i++
			}
			target = true
		case strings.ContainsRune("|&;", r):
			end()
		case r == '#' && !inWord:
			end()
			return commands
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	end()
	return commands
}
