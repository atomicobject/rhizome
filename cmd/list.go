package cmd

import (
	"fmt"
	"log"
	"os"
	"sync"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

// isTerminal returns true if stdout is a terminal
func isTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

func (commandOptions *vaultCommandOptions) makeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"l"},
		Short:   "List files in vault with various filtering options, such as tag:a-tag or find:a-filename-pattern",
		Long: `List files in your Obsidian vault with various filtering options:

Examples:
  rhizome list Notes                          # the Notes folder
  rhizome list find:joe                       # Filename containing "joe"
  rhizome list find:'n/s joe'                 # Notes in folder starting with "n" whose name contains a word starting with "s" and a word starting with "joe"
  rhizome list tag:career-pathing             # Notes tagged with "career-pathing"
  rhizome list tag:"career-pathing" -d 2      # Notes tagged with "career-pathing", notes they link to, and the notes those link to
  rhizome list find:project -d 1 --skip-anchors # Notes whose names match pattern plus directly linked notes, excluding anchors
  rhizome list find:notes -d 1 --skip-embeds    # Notes whose names match pattern plus directly linked notes, excluding embedded links`,
		Args: cobra.ArbitraryArgs,
		RunE: commandOptions.runList,
	}
}

func (commandOptions *vaultCommandOptions) registerListCommands(listCmd, rootCmd *cobra.Command) {
	listCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	listCmd.Flags().IntVarP(&commandOptions.maxDepth, "depth", "d", 0, "maximum depth for following wikilinks (0 means don't follow)")
	listCmd.Flags().BoolVar(&commandOptions.skipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	listCmd.Flags().BoolVar(&commandOptions.skipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")
	listCmd.Flags().BoolVarP(&commandOptions.absolutePaths, "absolute", "a", false, "print absolute paths")
	listCmd.Flags().BoolVar(&commandOptions.debug, "debug", false, "enable debug output")
	listCmd.Flags().BoolVar(&commandOptions.includeBacklinks, "backlinks", false, "include first-degree backlinks for each matched file")
	rootCmd.AddCommand(listCmd)
}

func (commandOptions *vaultCommandOptions) runList(cmd *cobra.Command, args []string) error {

	// If no vault name is provided, get the default vault name
	if commandOptions.vaultName == "" {
		vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
		defaultName, err := vault.DefaultName()
		if err != nil {
			return err
		}
		commandOptions.vaultName = defaultName
		if commandOptions.debug {
			log.Printf("Using default vault name: %s", commandOptions.vaultName)
		}
	}

	vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
	vaultDef, err := vault.Definition()
	if err != nil {
		return err
	}
	note, err := newProjectedNoteReader(cmd.Context(), vaultDef)
	if err != nil {
		return err
	}

	if commandOptions.debug {
		log.Printf("Number of input args: %d", len(args))
	}

	// Parse inputs using the helper function
	inputs, expr, err := actions.ParseInputsWithExpression(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return err
	}

	// If no inputs were provided, use a wildcard
	if len(inputs) == 0 {
		inputs = append(inputs, actions.ListInput{
			Type:  actions.InputTypeFile,
			Value: "*",
		})
	}

	if commandOptions.debug {
		log.Printf("Number of parsed inputs: %d", len(inputs))
	}

	// Mutex for printing when needed
	var printMu sync.Mutex

	// Get vault path for absolute paths
	vaultPath, err := vault.Path()
	if err != nil {
		return err
	}

	if commandOptions.debug {
		log.Printf("Vault path: %s", vaultPath)
	}

	params := actions.ListParams{
		Inputs:        inputs,
		MaxDepth:      commandOptions.maxDepth,
		SkipAnchors:   commandOptions.skipAnchors,
		SkipEmbeds:    commandOptions.skipEmbeds,
		AbsolutePaths: commandOptions.absolutePaths,
		Expression:    expr,
	}
	if commandOptions.debug {
		params.DebugOutput = cmd.ErrOrStderr()
	}

	var backlinks map[string][]obsidian.Backlink
	var primaryMatches []string

	if commandOptions.includeBacklinks {
		params.IncludeBacklinks = true
		params.Backlinks = &backlinks
		params.PrimaryMatches = &primaryMatches
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	matches, err := actions.ListFiles(&vault, note, params)
	if err != nil {
		return err
	}

	seen := make(map[string]bool)
	printPath := func(p string) {
		printMu.Lock()
		defer printMu.Unlock()
		if seen[p] {
			return
		}
		seen[p] = true
		path := p
		if commandOptions.absolutePaths {
			if absPath, err := vaultPaths.Abs(paths.RelPath(p)); err == nil && absPath.String() != "" {
				path = absPath.String()
			}
		}
		if isTerminal() {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", path)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%q\n", path)
		}
	}

	if commandOptions.includeBacklinks {
		primaries := primaryMatches
		if len(primaries) == 0 {
			primaries = matches
		}

		for _, file := range matches {
			printPath(file)
		}

		for _, file := range primaries {
			key := string(paths.NormalizeNotePath(file))
			for _, bl := range backlinks[key] {
				printPath(bl.Referrer)
			}
		}

		return nil
	}

	for _, file := range matches {
		printPath(file)
	}

	return nil
}
