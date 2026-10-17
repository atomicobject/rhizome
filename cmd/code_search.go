package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *searchInvocationOptions) makeCodeSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Answer-oriented search across notes, code, graph, and refs",
		Long: `Answer-oriented search across notes, code, graph, and refs.

This command is designed to support "agent task context" style workflows:
- Provide one or more query strings (positional query and/or -q/--query)
- Attach file paths as seeds (-f/--file) so related docs/code are surfaced`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			timings := &search.Timings{}
			if commandOptions.unifiedTimings {
				cmd.SetContext(search.WithTimings(cmd.Context(), timings))
				defer func() {
					if out := strings.TrimSpace(timings.Render()); out != "" {
						writer := cmd.OutOrStdout()
						if commandOptions.unifiedJSON {
							writer = cmd.ErrOrStderr()
						}
						fmt.Fprintln(writer)
						fmt.Fprintln(writer, out)
					}
				}()
			}

			if commandOptions.unifiedFast {
				if commandOptions.unifiedMaxPerOwner <= 0 || commandOptions.unifiedMaxPerOwner > 1 {
					commandOptions.unifiedMaxPerOwner = 1
				}
			}

			if commandOptions.unifiedTimeout > 0 {
				ctx, cancel := context.WithTimeout(cmd.Context(), commandOptions.unifiedTimeout)
				defer cancel()
				cmd.SetContext(ctx)
			}

			vaultPath, vaultDef, resolved, err := commandOptions.loadVaultAndConfigContext(cmd.Context())
			if err != nil {
				return err
			}
			noteFormats, err := newNoteFormatRuntime()
			if err != nil {
				return err
			}
			result, err := unifiedsearch.Execute(cmd.Context(), unifiedsearch.ApplicationOptions{
				Profile:            unifiedsearch.ProfileInteractive,
				MaterializeDisplay: commandOptions.unifiedRaw,
				NoDefaultTimeout:   commandOptions.unifiedTimeout == 0,
				VisibleLimit:       commandOptions.unifiedLimit,
				BudgetChars:        commandOptions.unifiedBudgetChars,
				MaxPerOwner:        commandOptions.unifiedMaxPerOwner,
				Continuation:       commandOptions.unifiedContinuation,
				Runtime: unifiedsearch.Options{
					NoteFormats:    noteFormats,
					Config:         &resolved,
					Query:          strings.TrimSpace(strings.Join(args, " ")),
					Queries:        commandOptions.unifiedQueries,
					Seeds:          commandOptions.unifiedSeeds,
					Files:          commandOptions.unifiedFiles,
					IntentInput:    commandOptions.unifiedIntent,
					Limit:          commandOptions.unifiedLimit,
					Pack:           commandOptions.unifiedPack,
					BudgetChars:    commandOptions.unifiedBudgetChars,
					MaxPerOwner:    commandOptions.unifiedMaxPerOwner,
					SeedLimit:      commandOptions.unifiedSeedLimit,
					UseVector:      !commandOptions.unifiedFast,
					UseIntel:       true,
					UseGraph:       !commandOptions.unifiedFast,
					UseRefs:        !commandOptions.unifiedFast,
					UseFTSBody:     false,
					VaultPath:      vaultPath,
					VaultDef:       vaultDef,
					EmbCfg:         resolved.NoteEmbeddings,
					ProviderAPIKey: "",
				},
			})
			if err != nil {
				if errors.Is(err, unifiedsearch.ErrTimedOutBeforePlanning) {
					fmt.Fprintln(cmd.ErrOrStderr(), "Search timed out before planning completed.")
					return nil
				}
				return err
			}
			if len(result.Warnings) > 0 {
				for _, w := range result.Warnings {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning (%s): %s\n", w.Code, w.Message)
				}
			}
			if commandOptions.unifiedJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			if commandOptions.unifiedPack && strings.TrimSpace(result.PackedText) != "" {
				fmt.Fprintln(cmd.OutOrStdout(), result.PackedText)
				return nil
			}
			if len(result.Sources) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No matches.")
				return nil
			}
			// Docs: [[search-quality-evaluation-corpus#^spec-0041-us2-ac1]].
			// Keep answer-shaped output and raw ranked output comparable from the same run inputs.
			if !commandOptions.unifiedRaw {
				fmt.Fprintln(cmd.OutOrStdout(), answer.RenderText(result.Answer))
				return nil
			}

			useColor := colorEnabled()
			termWidth := getTerminalWidth()
			chunkCache := make(map[string][]embeddings.ChunkInput)
			for i, item := range result.Display {
				printUnifiedResult(cmd.OutOrStdout(), useColor, termWidth, vaultPath, i+1, item.Primary, item.NoteChunks, item.Anchor, item.ModuleExports, chunkCache)
			}
			return nil
		},
	}
}

func resolveSeedHandles(ctx context.Context, raw []string, vaultPath string, intelStore *semdb.Store, seedLimit int) ([]knowledge.Handle, error) {
	return unifiedsearch.ResolveSeedHandles(ctx, raw, vaultPath, intelStore, seedLimit)
}

func (commandOptions *searchInvocationOptions) addUnifiedSearchFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&commandOptions.unifiedIntent, "mode", "", "Search mode: search|docs_for_code|related_to_seed|overview|subsystem_overview|code_for_docs|find_usages|go_to_def|explain_symbol|tests_for_code|refactor_impact|callers|callees|implementers|overrides|imports|data_flow|security_audit, or free-form text for inference")
	cmd.Flags().StringArrayVar(&commandOptions.unifiedSeeds, "seed", nil, "Seed(s): note path, file path, canonical handle (note:/file:/anchor:), or fqn:/symbol: lookups (directory seeds are sampled)")
	cmd.Flags().StringArrayVarP(&commandOptions.unifiedQueries, "query", "q", nil, "Additional focused query (repeatable); multiple queries run independently and are fused")
	cmd.Flags().StringArrayVarP(&commandOptions.unifiedFiles, "file", "f", nil, "File path seed(s) (repeatable); equivalent to passing file paths via --seed (directory seeds are sampled)")
	cmd.Flags().IntVarP(&commandOptions.unifiedLimit, "limit", "n", 25, "Max results to return")
	cmd.Flags().IntVar(&commandOptions.unifiedMaxPerOwner, "max-per-owner", 3, "Max results per owning note/anchor/file")
	cmd.Flags().IntVar(&commandOptions.unifiedSeedLimit, "seed-limit", 5, "Max anchors to expand per fqn:/symbol: seed")
	cmd.Flags().BoolVar(&commandOptions.unifiedPack, "pack", false, "Print a single budgeted context pack instead of per-result output")
	cmd.Flags().IntVar(&commandOptions.unifiedBudgetChars, "budget-chars", repoconfig.DefaultBudgetChars, "Max output size in characters when --pack is set")
	cmd.Flags().BoolVar(&commandOptions.unifiedFast, "fast", false, "Prefer speed over recall (disables vector/graph/refs)")
	cmd.Flags().DurationVar(&commandOptions.unifiedTimeout, "timeout", search.DefaultQueryTimeout, "Maximum time to spend (0 for no timeout)")
	cmd.Flags().BoolVar(&commandOptions.unifiedTimings, "timings", false, "Print per-stage timings after results")
	cmd.Flags().BoolVar(&commandOptions.unifiedRaw, "raw", false, "Print ranked search results instead of the answer packet")
	cmd.Flags().BoolVar(&commandOptions.unifiedJSON, "json", false, "Print the canonical application result as JSON")
	cmd.Flags().StringVar(&commandOptions.unifiedContinuation, "continuation", "", "Continue a previous search from its opaque token")

}

func (commandOptions *searchInvocationOptions) registerCodeSearchCommands(codeSearchCmd, rootCmd *cobra.Command) {
	commandOptions.addUnifiedSearchFlags(codeSearchCmd)
	rootCmd.AddCommand(codeSearchCmd)
}

func joinQueries(primary string, extras []string) string {
	return unifiedsearch.JoinQueries(primary, extras)
}

func mergeSeedTokens(seeds, files []string) []string {
	return unifiedsearch.MergeSeedTokens(seeds, files)
}
