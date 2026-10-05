package cmd

import (
	"errors"
	"fmt"
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
when init would change something.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		err := initactions.Run(initactions.RunOptions{
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
			Stdin:  cmd.InOrStdin(),
			Stdout: cmd.OutOrStdout(),
			Stderr: cmd.ErrOrStderr(),
		})
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
	initCmd.SetFlagErrorFunc(initFlagError)

	rootCmd.AddCommand(initCmd)
}
