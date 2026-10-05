package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
)

var (
	suppressTags   []string
	noSuppress     bool
	promptCompress bool
	promptBudget   int
	promptIntent   string
)

// promptCompressIntent is the default intent for prompt compression.
// This should describe the agent's goal, not compression strategies (those are in the shared prompt).
const promptCompressIntent = "Understand the key concepts, relationships, and constraints in these notes. " +
	"Focus on what an agent needs to operate within documented boundaries."

var promptCmd = &cobra.Command{
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
		actions.Debug = debug

		// Check if any inputs were provided
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "Error: at least one input is required\n")
			fmt.Fprintf(os.Stderr, "Run 'rhizome prompt --help' for usage.\n")
			return silentExitError{code: 1}
		}

		// If no vault name is provided, get the default vault name
		if vaultName == "" {
			vault := &obsidian.Vault{}
			defaultName, err := vault.DefaultName()
			if err != nil {
				return err
			}
			vaultName = defaultName
		}

		vault := obsidian.Vault{Name: vaultName}
		note := obsidian.Note{}

		// Parse inputs using the helper function
		inputs, expr, err := actions.ParseInputsWithExpression(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return silentExitError{code: 1}
		}

		// Configure suppressed tags
		var suppressedTags []string
		if !noSuppress {
			// Default suppression
			suppressedTags = append(suppressedTags, "no-prompt")
			// Add any additional suppressed tags
			suppressedTags = append(suppressedTags, suppressTags...)
		} else if len(suppressTags) > 0 {
			// Only use explicitly specified tags when --no-suppress is used
			suppressedTags = suppressTags
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
		fmt.Fprintf(&output, "<obsidian-vault name=\"%s\">\n\n", vaultName)

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
			MaxDepth:       maxDepth,
			SkipAnchors:    skipAnchors,
			SkipEmbeds:     skipEmbeds,
			AbsolutePaths:  absolutePaths,
			Expression:     expr,
			SuppressedTags: suppressedTags,
			OnMatch:        onMatch,
		}

		var backlinks map[string][]obsidian.Backlink
		var primaryMatches []string
		if includeBacklinks {
			params.IncludeBacklinks = true
			params.Backlinks = &backlinks
			params.PrimaryMatches = &primaryMatches
		}

		_, err = actions.ListFiles(&vault, &note, params)
		if err != nil {
			return err
		}

		if includeBacklinks {
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
			if absolutePaths && !filepath.IsAbs(path) {
				return filepath.Join(vaultPath, path)
			}
			return path
		}

		compressedText := ""
		if promptCompress && len(matches) > 0 {
			compressor := loadContextCompressor(vaultPath)
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
				intent := promptIntent
				if intent == "" {
					intent = promptCompressIntent
				}
				budget := promptBudget
				if budget <= 0 {
					budget = contextpack.DefaultBudgetChars
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

func init() {
	promptCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	promptCmd.Flags().IntVarP(&maxDepth, "depth", "d", 0, "maximum depth for following wikilinks (0 means don't follow)")
	promptCmd.Flags().BoolVar(&skipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	promptCmd.Flags().BoolVar(&skipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")
	promptCmd.Flags().BoolVarP(&absolutePaths, "absolute", "a", false, "print absolute paths")
	promptCmd.Flags().BoolVar(&debug, "debug", false, "enable debug output")
	promptCmd.Flags().StringSliceVar(&suppressTags, "suppress-tags", nil, "additional tags to suppress/exclude from output (comma-separated)")
	promptCmd.Flags().BoolVar(&noSuppress, "no-suppress", false, "disable all tag suppression, including default no-prompt tag")
	promptCmd.Flags().BoolVar(&includeBacklinks, "backlinks", false, "include first-degree backlinks for each matched file")
	promptCmd.Flags().BoolVar(&promptCompress, "compress", false, "compress output using LLM for token density")
	promptCmd.Flags().IntVar(&promptBudget, "budget", 0, "target output size in characters when compressing (default 70000)")
	promptCmd.Flags().StringVar(&promptIntent, "intent", "", "intent to guide compression (what you're trying to accomplish)")
	rootCmd.AddCommand(promptCmd)
}
