package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
)

// promptCompressIntent is the default intent for prompt compression.
// This should describe the agent's goal, not compression strategies (those are in the shared prompt).
const promptCompressIntent = "Understand the key concepts, relationships, and constraints in these notes. " +
	"Focus on what an agent needs to operate within documented boundaries."

func (commandOptions *promptInvocationOptions) makePromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "List files in vault with contents formatted for LLM consumption, such as tag:a-tag or find:a-filename-pattern",
		Long: `List files in your Obsidian vault with contents formatted for LLM consumption.
Similar to the list command, but outputs file contents in a format optimized for LLMs.

By default, files tagged with "no-prompt" are excluded from output. This can be controlled with --suppress-tags and --no-suppress flags.

Examples:
  rhizome prompt Notes  					 						# the Notes folder
  rhizome prompt find:joe  									# Filename containing "joe"
  rhizome prompt find:'n/s joe'  						# Notes in folder starting with "n" whose name contains a word starting with "s" and a word starting with "joe"
  rhizome prompt tag:career-pathing 					# Notes tagged with "career-pathing"
  rhizome prompt tag:"career-pathing" -d 2 	# Notes tagged with "career-pathing", notes they link to, and the notes those link to
  rhizome prompt find:project -d 1 --skip-anchors   # Notes whose names match pattern plus directly linked notes, excluding anchors
  rhizome prompt find:notes -d 1 --skip-embeds      # Notes whose names match pattern plus directly linked notes, excluding embedded links
  rhizome prompt find:docs -d 1 --skip-anchors --skip-embeds  # Skip both anchored and embedded links
  rhizome prompt tag:foo --suppress-tags private,draft     # Find tag:foo but exclude files with private or draft tags
  rhizome prompt Notes --no-suppress                        # Don't exclude any tags (including no-prompt)
	`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Enable debug output if debug flag is set

			// Check if any inputs were provided
			if len(args) == 0 {
				fmt.Fprintf(os.Stderr, "Error: at least one input is required\n")
				fmt.Fprintf(os.Stderr, "Run 'rhizome prompt --help' for usage.\n")
				return silentExitError{code: 1}
			}

			// If no vault name is provided, get the default vault name
			if commandOptions.vaultName == "" {
				vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
				defaultName, err := vault.DefaultName()
				if err != nil {
					return err
				}
				commandOptions.vaultName = defaultName
			}

			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			note := obsidian.Note{}

			// Parse inputs using the helper function
			inputs, expr, err := actions.ParseInputsWithExpression(args)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return silentExitError{code: 1}
			}

			// Configure suppressed tags
			var suppressedTags []string
			if !commandOptions.noSuppress {
				// Default suppression
				suppressedTags = append(suppressedTags, "no-prompt")
				// Add any additional suppressed tags
				suppressedTags = append(suppressedTags, commandOptions.suppressTags...)
			} else if len(commandOptions.suppressTags) > 0 {
				// Only use explicitly specified tags when --no-suppress is used
				suppressedTags = commandOptions.suppressTags
			}

			// Create a map to track unique files
			uniqueFiles := make(map[string]bool)
			// Create a mutex to safely print files
			var printMu sync.Mutex

			// Get vault path
			vaultPath, err := vault.Path()
			if err != nil {
				return err
			}

			// Buffer to store output
			var output strings.Builder

			// Print initial search message if in terminal mode
			isTerminalMode := isTerminal()
			if isTerminalMode {
				fmt.Fprintf(os.Stderr, "Searching with: %q\n", strings.Join(args, " "))
				if len(suppressedTags) > 0 {
					fmt.Fprintf(os.Stderr, "Suppressing files with tags: %v\n", suppressedTags)
				}
			}

			// Print the vault header
			fmt.Fprintf(&output, "<obsidian-vault name=\"%s\">\n\n", commandOptions.vaultName)

			type match struct {
				path    string
				content string
			}
			var matches []match

			onMatch := func(file string) {
				printMu.Lock()
				defer printMu.Unlock()
				if uniqueFiles[file] {
					return
				}
				uniqueFiles[file] = true

				filePath := filepath.Join(vaultPath, file)
				content, err := os.ReadFile(filePath)
				if err != nil {
					log.Printf("Error reading file %s: %v", file, err)
					return
				}

				if isTerminalMode {
					fmt.Fprintf(os.Stderr, "Found %s\n", file)
				}

				matches = append(matches, match{path: file, content: string(content)})
			}

			params := actions.ListParams{
				Inputs:         inputs,
				MaxDepth:       commandOptions.maxDepth,
				SkipAnchors:    commandOptions.skipAnchors,
				SkipEmbeds:     commandOptions.skipEmbeds,
				AbsolutePaths:  commandOptions.absolutePaths,
				Expression:     expr,
				SuppressedTags: suppressedTags,
				OnMatch:        onMatch,
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

			_, err = actions.ListFiles(&vault, &note, params)
			if err != nil {
				return err
			}

			if commandOptions.includeBacklinks {
				primaries := primaryMatches
				if len(primaries) == 0 {
					for _, m := range matches {
						primaries = append(primaries, m.path)
					}
				}

				seen := make(map[string]bool)
				for _, m := range matches {
					seen[string(paths.NormalizeNotePath(m.path))] = true
				}

				for _, p := range primaries {
					key := string(paths.NormalizeNotePath(p))
					for _, bl := range backlinks[key] {
						ref := string(paths.NormalizeNotePath(bl.Referrer))
						if seen[ref] {
							continue
						}
						content, err := os.ReadFile(filepath.Join(vaultPath, ref))
						if err != nil {
							log.Printf("Error reading backlink referrer %s: %v", ref, err)
							continue
						}
						matches = append(matches, match{path: ref, content: string(content)})
						seen[ref] = true
					}
				}
			}

			outputPath := func(path string) string {
				if commandOptions.absolutePaths && !filepath.IsAbs(path) {
					return filepath.Join(vaultPath, path)
				}
				return path
			}

			compressedText := ""
			if commandOptions.promptCompress && len(matches) > 0 {
				compressor := commandOptions.loadContextCompressor(cmd.Context(), vaultPath)
				if compressor == nil {
					fmt.Fprintf(os.Stderr, "Warning: no compressor available (check API key), falling back to normal output\n")
				} else {
					pieces := make([]contextpack.Piece, 0, len(matches))
					for i, m := range matches {
						pieces = append(pieces, contextpack.Piece{
							Key:      outputPath(m.path),
							Priority: 100,
							Score:    float64(len(matches) - i), // Preserve order
							Text:     m.content,
						})
					}
					intent := commandOptions.promptIntent
					if intent == "" {
						intent = promptCompressIntent
					}
					budget := commandOptions.promptBudget
					if budget <= 0 {
						budget = repoconfig.DefaultBudgetChars
					}
					if isTerminalMode {
						fmt.Fprintf(os.Stderr, "Compressing %d files...\n", len(pieces))
					}
					text, meta := contextpack.PackWithIntent(pieces, budget, contextpack.PackOptions{
						Intent:         intent,
						Compressor:     compressor,
						Ctx:            context.Background(),
						AlwaysCompress: true,
					})
					if meta.Compressed && text != "" {
						compressedText = text
					} else {
						fmt.Fprintf(os.Stderr, "Warning: compression failed, falling back to normal output\n")
					}
				}
			}
			if compressedText != "" {
				fmt.Fprintln(&output, compressedText)
			} else {
				for _, m := range matches {
					fmt.Fprintf(&output, "<file path=\"%s\">\n%s\n</file>\n\n", outputPath(m.path), m.content)
				}
			}
			fmt.Fprint(&output, "</obsidian-vault>")

			// If terminal, copy to clipboard and notify on stderr
			if isTerminalMode {
				err := clipboard.WriteAll(output.String())
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Copied %d files to clipboard in LLM-friendly format\n", len(uniqueFiles))
			} else {
				// If piped, print to stdout
				fmt.Print(output.String())
			}
			return nil
		},
	}
}

func (commandOptions *promptInvocationOptions) registerPromptCommands(promptCmd, rootCmd *cobra.Command) {
	promptCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	promptCmd.Flags().IntVarP(&commandOptions.maxDepth, "depth", "d", 0, "maximum depth for following wikilinks (0 means don't follow)")
	promptCmd.Flags().BoolVar(&commandOptions.skipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	promptCmd.Flags().BoolVar(&commandOptions.skipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")
	promptCmd.Flags().BoolVarP(&commandOptions.absolutePaths, "absolute", "a", false, "print absolute paths")
	promptCmd.Flags().BoolVar(&commandOptions.debug, "debug", false, "enable debug output")
	promptCmd.Flags().StringSliceVar(&commandOptions.suppressTags, "suppress-tags", nil, "additional tags to suppress/exclude from output (comma-separated)")
	promptCmd.Flags().BoolVar(&commandOptions.noSuppress, "no-suppress", false, "disable all tag suppression, including default no-prompt tag")
	promptCmd.Flags().BoolVar(&commandOptions.includeBacklinks, "backlinks", false, "include first-degree backlinks for each matched file")
	promptCmd.Flags().BoolVar(&commandOptions.promptCompress, "compress", false, "compress output using LLM for token density")
	promptCmd.Flags().IntVar(&commandOptions.promptBudget, "budget", 0, "target output size in characters when compressing (default 70000)")
	promptCmd.Flags().StringVar(&commandOptions.promptIntent, "intent", "", "intent to guide compression (what you're trying to accomplish)")
	rootCmd.AddCommand(promptCmd)
}
