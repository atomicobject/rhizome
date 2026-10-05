package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/spf13/cobra"
)

var (
	unifiedIntent       string
	unifiedSeeds        []string
	unifiedQueries      []string
	unifiedFiles        []string
	unifiedLimit        int
	unifiedPack         bool
	unifiedBudgetChars  int
	unifiedMaxPerOwner  int
	unifiedSeedLimit    int
	unifiedFast         bool
	unifiedTimeout      time.Duration
	unifiedTimings      bool
	unifiedRaw          bool
	unifiedJSON         bool
	unifiedContinuation string
)

var codeSearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Answer-oriented search across notes, code, graph, and refs",
	Long: `Answer-oriented search across notes, code, graph, and refs.

This command is designed to support "agent task context" style workflows:
- Provide one or more query strings (positional query and/or -q/--query)
- Attach file paths as seeds (-f/--file) so related docs/code are surfaced`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		timings := &search.Timings{}
		if unifiedTimings {
			cmd.SetContext(search.WithTimings(cmd.Context(), timings))
			defer func() {
				if out := strings.TrimSpace(timings.Render()); out != "" {
					writer := cmd.OutOrStdout()
					if unifiedJSON {
						writer = cmd.ErrOrStderr()
					}
					fmt.Fprintln(writer)
					fmt.Fprintln(writer, out)
				}
			}()
		}

		if unifiedFast {
			if unifiedMaxPerOwner <= 0 || unifiedMaxPerOwner > 1 {
				unifiedMaxPerOwner = 1
			}
		}

		if unifiedTimeout > 0 {
			ctx, cancel := context.WithTimeout(cmd.Context(), unifiedTimeout)
			defer cancel()
			cmd.SetContext(ctx)
		}

		vaultPath, vaultDef, embCfg, err := loadVaultAndConfigContext(cmd.Context())
		if err != nil {
			return err
		}
		result, err := unifiedsearch.Execute(cmd.Context(), unifiedsearch.ApplicationOptions{
			Profile:            unifiedsearch.ProfileInteractive,
			MaterializeDisplay: unifiedRaw,
			NoDefaultTimeout:   unifiedTimeout == 0,
			VisibleLimit:       unifiedLimit,
			BudgetChars:        unifiedBudgetChars,
			MaxPerOwner:        unifiedMaxPerOwner,
			Continuation:       unifiedContinuation,
			Runtime: unifiedsearch.Options{
				Query:          strings.TrimSpace(strings.Join(args, " ")),
				Queries:        unifiedQueries,
				Seeds:          unifiedSeeds,
				Files:          unifiedFiles,
				IntentInput:    unifiedIntent,
				Limit:          unifiedLimit,
				Pack:           unifiedPack,
				BudgetChars:    unifiedBudgetChars,
				MaxPerOwner:    unifiedMaxPerOwner,
				SeedLimit:      unifiedSeedLimit,
				UseVector:      !unifiedFast,
				UseIntel:       true,
				UseGraph:       !unifiedFast,
				UseRefs:        !unifiedFast,
				UseFTSBody:     false,
				VaultPath:      vaultPath,
				VaultDef:       vaultDef,
				EmbCfg:         embCfg,
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
		if unifiedJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		if unifiedPack && strings.TrimSpace(result.PackedText) != "" {
			fmt.Fprintln(cmd.OutOrStdout(), result.PackedText)
			return nil
		}
		if len(result.Sources) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No matches.")
			return nil
		}
		// Docs: [[search-quality-evaluation-corpus#^spec-0041-us2-ac1]].
		// Keep answer-shaped output and raw ranked output comparable from the same run inputs.
		if !unifiedRaw {
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

func resolveSeedHandles(ctx context.Context, raw []string, vaultPath string, intelStore *semdb.Store, seedLimit int) ([]knowledge.Handle, error) {
	return unifiedsearch.ResolveSeedHandles(ctx, raw, vaultPath, intelStore, seedLimit)
}

func addUnifiedSearchFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&unifiedIntent, "mode", "", "Search mode: search|docs_for_code|related_to_seed|overview|subsystem_overview|code_for_docs|find_usages|go_to_def|explain_symbol|tests_for_code|refactor_impact|callers|callees|implementers|overrides|imports|data_flow|security_audit, or free-form text for inference")
	cmd.Flags().StringArrayVar(&unifiedSeeds, "seed", nil, "Seed(s): note path, file path, canonical handle (note:/file:/anchor:), or fqn:/symbol: lookups (directory seeds are sampled)")
	cmd.Flags().StringArrayVarP(&unifiedQueries, "query", "q", nil, "Additional focused query (repeatable); multiple queries run independently and are fused")
	cmd.Flags().StringArrayVarP(&unifiedFiles, "file", "f", nil, "File path seed(s) (repeatable); equivalent to passing file paths via --seed (directory seeds are sampled)")
	cmd.Flags().IntVarP(&unifiedLimit, "limit", "n", 25, "Max results to return")
	cmd.Flags().IntVar(&unifiedMaxPerOwner, "max-per-owner", 3, "Max results per owning note/anchor/file")
	cmd.Flags().IntVar(&unifiedSeedLimit, "seed-limit", 5, "Max anchors to expand per fqn:/symbol: seed")
	cmd.Flags().BoolVar(&unifiedPack, "pack", false, "Print a single budgeted context pack instead of per-result output")
	cmd.Flags().IntVar(&unifiedBudgetChars, "budget-chars", contextpack.DefaultBudgetChars, "Max output size in characters when --pack is set")
	cmd.Flags().BoolVar(&unifiedFast, "fast", false, "Prefer speed over recall (disables vector/graph/refs)")
	cmd.Flags().DurationVar(&unifiedTimeout, "timeout", search.DefaultQueryTimeout, "Maximum time to spend (0 for no timeout)")
	cmd.Flags().BoolVar(&unifiedTimings, "timings", false, "Print per-stage timings after results")
	cmd.Flags().BoolVar(&unifiedRaw, "raw", false, "Print ranked search results instead of the answer packet")
	cmd.Flags().BoolVar(&unifiedJSON, "json", false, "Print the canonical application result as JSON")
	cmd.Flags().StringVar(&unifiedContinuation, "continuation", "", "Continue a previous search from its opaque token")

}

func init() {
	addUnifiedSearchFlags(codeSearchCmd)
	rootCmd.AddCommand(codeSearchCmd)
}

func joinQueries(primary string, extras []string) string {
	return unifiedsearch.JoinQueries(primary, extras)
}

func mergeSeedTokens(seeds, files []string) []string {
	return unifiedsearch.MergeSeedTokens(seeds, files)
}
