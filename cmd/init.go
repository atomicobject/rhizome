package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/spf13/cobra"
)

var (
	initPath           string
	initWorkflow       string
	initAgents         string
	initSearch         string
	initRefreshDocs    bool
	initIncludeIgnored []string
	initBinaryManager  string
	initEject          string
	initRestore        string
	initCheck          bool
	initAccept         bool
	initJSON           bool
	initAddons         string
	initSkip           []string
	initKeepIndexed    []string
	initSearchKeyStdin bool
)

// removedInitFlags maps flags init no longer accepts to what replaces them.
var removedInitFlags = map[string]string{
	"yes":                         "rzm init no longer needs --yes: without a terminal it uses the recommendations and keeps edited files",
	"template":                    "use --workflow agentic-engineering|domain|none",
	"cursor":                      "use --agents with the agents you use, for example --agents claude,cursor",
	"claude":                      "use --agents with the agents you use, for example --agents claude,codex",
	"codex":                       "use --agents with the agents you use, for example --agents claude,codex",
	"agent-skills":                "use --agents; AGENTS.md and .agents/skills follow the agents you choose, and --agents none turns them off",
	"agentsmd":                    "use --agents; AGENTS.md and .agents/skills follow the agents you choose, and --agents none turns them off",
	"no-config":                   "run rzm init; a run that changes no setting leaves config alone",
	"refresh-template-docs":       "use --refresh-docs",
	"eject-template":              "use --eject",
	"eject-template-cascade":      "name every workflow to eject, for example --eject agentic-engineering,complex-domain",
	"restore-template-management": "use --restore",
	"skip-rejected":               "rzm init now asks only about files you edited and remembers what you keep",
	"reject-all":                  "rzm init now asks only about files you edited and remembers what you keep",
	"clear-rejections":            "rzm init now asks only about files you edited and remembers what you keep",
	"skill-overlay-manifest":      "this debugging flag was removed",
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up or update Rhizome in this repository",
	Long: `Set up Rhizome in this repository, or update it on later runs.

The first run shows what Rhizome found (docs, code, agents, search), asks which
workflow to install and for a semantic search key if none is available, and
writes everything after one confirmation.

Later runs show the setup and what init would change: settings that detection
suggests and Rhizome's generated files. In a terminal, init asks once before
applying and offers the settings menu; it asks separately only about files
someone edited. Without a terminal, init applies the changes, keeps edited
files, and lists them.

rzm init --check prints the same report, writes nothing, and exits with status 1
when init would change something.

For a first run, --json writes one JSON document instead of text: with --check,
what setup would do with the given choices and every choice it offers; without
it, what setup wrote. Scripts and the desktop app use it with --workflow,
--addons, --agents, --search, --skip, --keep-indexed, --include-ignored, and
--search-key-stdin, which reads a key from a pipe so it never appears in
arguments.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := initactions.RunOptions{
			Dir:               initPath,
			Interactive:       stdinIsTerminal(cmd),
			Workflow:          initWorkflow,
			Agents:            initAgents,
			Search:            initSearch,
			RefreshDocs:       initRefreshDocs,
			IncludeIgnored:    initIncludeIgnored,
			BinaryManager:     initBinaryManager,
			Eject:             initEject,
			Restore:           initRestore,
			Check:             initCheck,
			AcceptSuggestions: initAccept,
			IndexNow: func(projectRoot string) error {
				def, err := localVaultDefFromCWD(projectRoot)
				if err != nil {
					return err
				}
				return runResolvedIndexCommand(cmd, def, newNoteMetadataIndexer)
			},
			Addons:      initAddons,
			Skip:        initSkip,
			KeepIndexed: initKeepIndexed,
			Stdin:       cmd.InOrStdin(),
			Stdout:      cmd.OutOrStdout(),
			Stderr:      cmd.ErrOrStderr(),
		}
		var keyErr error
		if initSearchKeyStdin {
			opts.Interactive = false
			opts.SearchKey, keyErr = readSearchKey(cmd)
		}
		if initJSON {
			return runInitJSON(cmd.OutOrStdout(), opts, keyErr)
		}
		if keyErr != nil {
			return keyErr
		}
		err := initactions.Run(opts)
		if errors.Is(err, initactions.ErrChangesPending) {
			return silentExitError{code: 1}
		}
		return err
	},
}

func stdinIsTerminal(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && initactions.IsTTY(f)
}

// readSearchKey reads --search-key-stdin's key: the first line of a pipe.
// A terminal would echo the key, so it is refused.
func readSearchKey(cmd *cobra.Command) (string, error) {
	if stdinIsTerminal(cmd) {
		return "", errors.New("--search-key-stdin reads the key from a pipe, so it is never shown; pipe it in instead of typing it")
	}
	line, err := bufio.NewReader(io.LimitReader(cmd.InOrStdin(), 64<<10)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if key := strings.TrimSpace(line); key != "" {
		return key, nil
	}
	return "", errors.New("--search-key-stdin found no key on standard input")
}

// initJSONFailure is the --json document for a failed run.
type initJSONFailure struct {
	Schema int `json:"schema"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	SavedKey string `json:"savedKey,omitempty"`
}

// runInitJSON writes the first-run plan (with --check) or result as one
// JSON document. Failures are JSON too, and exit with status 1, as does a
// setup whose pinned executable could not be installed.
func runInitJSON(out io.Writer, opts initactions.RunOptions, err error) error {
	var doc any
	failed, savedKey := false, ""
	switch {
	case err != nil:
	case opts.Check:
		doc, err = initactions.Plan(opts)
	default:
		var result initactions.SetupResult
		result, err = initactions.Apply(opts)
		doc, failed, savedKey = result, result.Pin.Error != "", result.SavedKey
	}
	if err != nil {
		failure := initJSONFailure{Schema: initactions.SetupSchema, SavedKey: savedKey}
		failure.Error.Code, failure.Error.Message = "setup_failed", err.Error()
		if errors.Is(err, initactions.ErrNotFirstRun) {
			failure.Error.Code = "not_first_run"
		}
		doc, failed = failure, true
	}
	if encodeErr := json.NewEncoder(out).Encode(doc); encodeErr != nil {
		return encodeErr
	}
	if failed {
		return silentExitError{code: 1}
	}
	return nil
}

// initFlagError explains a removed flag instead of printing cobra's generic
// unknown-flag error.
func initFlagError(cmd *cobra.Command, err error) error {
	name, ok := strings.CutPrefix(err.Error(), "unknown flag: --")
	if !ok {
		return err
	}
	name, _, _ = strings.Cut(name, "=")
	if replacement, removed := removedInitFlags[name]; removed {
		return fmt.Errorf("--%s was removed: %s", name, replacement)
	}
	return err
}

func init() {
	initCmd.Flags().BoolVar(&initAccept, "accept-suggestions", false, "also apply the suggestions a rerun lists, such as removing folder limits (for agents and scripts after the person agrees)")
	initCmd.Flags().BoolVar(&initCheck, "check", false, "show what init would change without writing; exit 1 when something would change")
	initCmd.Flags().StringVar(&initPath, "path", "", "project directory to set up (default: current directory or Git root)")
	initCmd.Flags().StringVar(&initWorkflow, "workflow", "", "workflow to install: agentic-engineering, domain, or none")
	initCmd.Flags().StringVar(&initAgents, "agents", "", "agents to set up: claude, codex, cursor (comma-separated), or none")
	initCmd.Flags().StringVar(&initSearch, "search", "", "semantic search provider: voyage (default), openai, ollama, or off")
	initCmd.Flags().BoolVar(&initRefreshDocs, "refresh-docs", false, "offer Rhizome's latest versions of the starter docs your team owns")
	initCmd.Flags().StringArrayVar(&initIncludeIgnored, "include-ignored", nil, "index a folder Git ignores (repeatable)")
	initCmd.Flags().StringVar(&initBinaryManager, "binary-manager", "", "let an external tool such as mise own the Rhizome binary: external")
	initCmd.Flags().StringVar(&initEject, "eject", "", "keep a workflow's files for your team to own and stop updating them (comma-separated)")
	initCmd.Flags().StringVar(&initRestore, "restore", "", "resume updates for an ejected workflow")
	initCmd.Flags().BoolVar(&initJSON, "json", false, "write one JSON document instead of text; first-run setup only (with --check: the plan and every choice)")
	initCmd.Flags().StringVar(&initAddons, "addons", "", "add-on starters to install with the workflow, such as action-items (comma-separated), or none; first run only")
	initCmd.Flags().StringArrayVar(&initSkip, "skip", nil, "leave a folder or file out of the index (repeatable; first run only)")
	initCmd.Flags().StringArrayVar(&initKeepIndexed, "keep-indexed", nil, "keep indexing a path init suggests skipping (repeatable; first run only)")
	initCmd.Flags().BoolVar(&initSearchKeyStdin, "search-key-stdin", false, "read the semantic search key from a pipe and save it, so it never appears in arguments (first run only)")
	initCmd.SetFlagErrorFunc(initFlagError)

	rootCmd.AddCommand(initCmd)
}
