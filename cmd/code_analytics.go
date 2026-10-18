package cmd

// Docs:
// - [Code Intel - Query patterns + graph analysis](docs/reference/analysis/Code Intel - Query patterns + graph analysis.md)
// - [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

import (
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/atomicobject/rhizome/pkg/app/presentation"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *codeInvocationOptions) makeCodeDocCoverageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doc-coverage [paths...]",
		Short: "Show widely-used symbols with weak documentation coverage",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultPath, codeCfg, err := commandOptions.loadVaultAndCodeConfig(cmd)
			if err != nil {
				return err
			}
			store, cleanup, err := requireIntelStore(vaultPath, codeCfg, indexstore.ReadOnly)
			if err != nil {
				return err
			}
			defer cleanup()

			vaultDef, err := commandOptions.vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return err
			}

			pathFilters := normalizeDocCoveragePaths(vaultPath, args)

			options := semdb.DocCoverageOptions{
				Limit:        commandOptions.codeDocCoverageLimit,
				PathPrefixes: pathFilters,
				Langs:        commandOptions.codeDocCoverageLangs,
			}
			if commandOptions.codeDocCoverageFallbackThreshold > 0 {
				t := commandOptions.codeDocCoverageFallbackThreshold
				options.FallbackThreshold = &t
			}

			var warnings []string
			if len(commandOptions.codeDocCoverageQueries) > 0 {
				searchFQNs, searchPaths, warns, err := runDocCoverageSearch(cmd, vaultPath, vaultDef, store, commandOptions.codeDocCoverageQueries, args, commandOptions.codeDocCoverageSearchLimit)
				if err != nil {
					return err
				}
				warnings = append(warnings, warns...)
				options.PathPrefixes = dedupeStrings(append(options.PathPrefixes, searchPaths...))
				options.FQNs = dedupeStrings(append(options.FQNs, searchFQNs...))
			}

			rows, err := store.DocCoverage(cmd.Context(), options)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No call data available (index code first with `rzm index`).")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Doc coverage (top %d by usage; docs=mentions):\n", len(rows))
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "Calls\tCallers\tDocs\tLinks\tLang\tKind\tFQN\tPath")
			for _, r := range rows {
				kind := r.Kind
				path := r.Path
				docs := r.Mentions
				if !r.Resolved {
					kind = "unresolved"
					path = "-"
					docs = 0
				}
				if path != "-" {
					path = relToVault(vaultPath, path)
				}
				fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%s\t%s\t%s\t%s\n", r.Calls, r.Callers, docs, r.Links, r.Lang, kind, r.FQN, path)
			}
			_ = w.Flush()

			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Notes:")
			fmt.Fprintln(cmd.OutOrStdout(), "- `Docs` counts doc-section mentions resolved to an `anchor_id` (via `intel_edges(kind=mentions)`).")
			fmt.Fprintln(cmd.OutOrStdout(), "- `unresolved` rows should be rare; call edges are only stored when a callee resolves to an indexed anchor.")
			if len(warnings) > 0 {
				for _, w := range warnings {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", w)
				}
			}
			return nil
		},
	}
}

func (commandOptions *codeInvocationOptions) makeCodeHotspotsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hotspots",
		Short: "Rank dependency hotspots (fan-in packages and fan-out files)",
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultPath, codeCfg, err := commandOptions.loadVaultAndCodeConfig(cmd)
			if err != nil {
				return err
			}
			store, cleanup, err := requireIntelStore(vaultPath, codeCfg, indexstore.ReadOnly)
			if err != nil {
				return err
			}
			defer cleanup()

			hopts := semdb.HotspotPackagesOptions{
				Limit: commandOptions.codeHotspotsLimit,
				Langs: commandOptions.codeHotspotsLangs,
			}
			if commandOptions.codeHotspotsFallbackThreshold > 0 {
				t := commandOptions.codeHotspotsFallbackThreshold
				hopts.FallbackThreshold = &t
			}
			pkgs, err := store.HotspotPackages(cmd.Context(), hopts)
			if err != nil {
				return err
			}
			fopts := semdb.HotspotFilesOptions{
				Limit: commandOptions.codeHotspotsLimit,
				Langs: commandOptions.codeHotspotsLangs,
			}
			if commandOptions.codeHotspotsFallbackThreshold > 0 {
				t := commandOptions.codeHotspotsFallbackThreshold
				fopts.FallbackThreshold = &t
			}
			files, err := store.HotspotFiles(cmd.Context(), fopts)
			if err != nil {
				return err
			}

			if len(pkgs) == 0 && len(files) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No call data available (index code first with `rzm index`).")
				return nil
			}

			if len(pkgs) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Dependency hotspots (packages by fan-in; top %d):\n", len(pkgs))
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "Callers\tCalls\tDocs\tUsed\tLang\tPackage")
				for _, p := range pkgs {
					fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%s\t%s\n", p.Callers, p.Calls, p.DocumentedSymbols, p.UsedSymbols, p.Lang, displayPkg(p.Pkg))
				}
				_ = w.Flush()
				fmt.Fprintln(cmd.OutOrStdout())
			}

			if len(files) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Dependency hotspots (files by fan-out; top %d):\n", len(files))
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "Deps\tCalls\tUsed\tFile")
				for _, f := range files {
					fmt.Fprintf(w, "%d\t%d\t%d\t%s\n", f.Deps, f.Calls, f.UsedSymbols, relToVault(vaultPath, f.File))
				}
				_ = w.Flush()
			}

			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Notes:")
			fmt.Fprintln(cmd.OutOrStdout(), "- Package `Docs` counts how many *used symbols* have at least one doc-section mention (`intel_edges(kind=mentions)`), not total docs.")
			fmt.Fprintln(cmd.OutOrStdout(), "- Fan-in/fan-out are derived from intel_edges(kind='calls').")
			return nil
		},
	}
}

func (commandOptions *codeInvocationOptions) makeCodeComplexityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "complexity [paths...]",
		Short: "Rank symbol-level complexity hotspots (size + fan-in/out)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultPath, codeCfg, err := commandOptions.loadVaultAndCodeConfig(cmd)
			if err != nil {
				return err
			}
			store, cleanup, err := requireIntelStore(vaultPath, codeCfg, indexstore.ReadOnly)
			if err != nil {
				return err
			}
			defer cleanup()

			pathFilters := normalizeDocCoveragePaths(vaultPath, args)
			rows, err := store.ComplexityHotspots(cmd.Context(), semdb.ComplexityOptions{
				Limit:        commandOptions.codeComplexityLimit,
				PathPrefixes: pathFilters,
				MinLines:     commandOptions.codeComplexityMinLines,
			})
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No complexity data available (index code first with `rzm index`).")
				return nil
			}

			useColor := colorEnabled()
			title := fmt.Sprintf("Complexity hotspots (top %d by score; functions + methods):", len(rows))
			if useColor {
				title = colorGreen + title + colorReset
			}
			fmt.Fprintln(cmd.OutOrStdout(), title)
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			header := "Score\tSpan\tCallers\tCallees\tCallsIn\tCallsOut\tLang\tKind\tFQN\tPath"
			if useColor {
				header = colorYellow + "Score" + colorReset + "\t" +
					colorGray + "Span" + colorReset + "\t" +
					colorGray + "Callers" + colorReset + "\t" +
					colorGray + "Callees" + colorReset + "\t" +
					colorGray + "CallsIn" + colorReset + "\t" +
					colorGray + "CallsOut" + colorReset + "\t" +
					colorGray + "Lang" + colorReset + "\t" +
					colorGray + "Kind" + colorReset + "\t" +
					colorGray + "FQN" + colorReset + "\t" +
					colorGray + "Path" + colorReset
			}
			fmt.Fprintln(w, header)
			for _, r := range rows {
				path := r.Path
				if path != "" {
					path = relToVault(vaultPath, path)
				} else {
					path = "-"
				}
				scoreStr := fmt.Sprintf("%.1f", r.Score)
				if useColor {
					scoreStr = colorYellow + scoreStr + colorReset
				}
				fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%s\t%s\t%s\t%s\n",
					scoreStr, r.SpanLines, r.Callers, r.Callees, r.CallsIn, r.CallsOut, r.Lang, r.Kind, r.FQN, path)
			}
			_ = w.Flush()

			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Notes:")
			fmt.Fprintln(cmd.OutOrStdout(), "- `Span` is end_line - start_line + 1 from indexed anchors.")
			fmt.Fprintln(cmd.OutOrStdout(), "- `Callers` counts distinct caller files; `Callees` counts distinct symbols called by the function.")
			fmt.Fprintln(cmd.OutOrStdout(), "- `CallsIn/CallsOut` count total call rows; `Score` is a heuristic (size × fan-in × fan-out).")
			return nil
		},
	}
}

func relToVault(vaultPath, p string) string {
	if vaultPath == "" || p == "" {
		return p
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(vaultPath, p)
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return filepath.ToSlash(p)
	}
	rel, err := vaultPaths.RelStrict(abs)
	if err != nil {
		return filepath.ToSlash(p)
	}
	if rel.String() == "" {
		return "."
	}
	return rel.String()
}

func displayPkg(pkg string) string {
	if pkg == "" {
		return "(unknown)"
	}
	return pkg
}

func normalizeDocCoveragePaths(vaultPath string, raw []string) []string {
	seen := map[string]struct{}{}
	var out []string
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if vaultPaths.Root() == "" {
			candidate := string(paths.NormalizeCode(p))
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
			continue
		}

		rel, _, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, p)
		if err != nil || rel == "" {
			candidate := string(paths.NormalizeCode(p))
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
			continue
		}
		if _, ok := seen[rel.String()]; ok {
			continue
		}
		seen[rel.String()] = struct{}{}
		out = append(out, rel.String())
	}
	return out
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func runDocCoverageSearch(cmd *cobra.Command, vaultPath string, vaultDef obsidian.VaultDefinition, intelStore *semdb.Store, queries, pathArgs []string, searchLimit int) ([]string, []string, []string, error) {
	query := strings.TrimSpace(strings.Join(queries, " "))
	if query == "" {
		return nil, nil, nil, nil
	}

	if searchLimit <= 0 {
		searchLimit = 50
	}

	var warnings []string
	resolvedConfig1, configErr1 := repoconfig.Load(vaultPath)
	embCfg, err := resolvedConfig1.NoteEmbeddings, configErr1
	if err != nil {
		return nil, nil, nil, err
	}
	embCfg.IndexPath = repoconfig.UnifiedIndexPath(vaultPath, embCfg.IndexPath)
	codeEmbCfg, _ := resolvedConfig1.CodeEmbeddingsFor(embCfg)

	var (
		noteProvider embeddings.Provider
		codeStore    *embsqlite.CodeStore
		codeProvider embeddings.Provider
	)

	if embCfg.Enabled {
		provider, providerCfg, err := embeddings.NewProviderForConfig(vaultconfig.FromContext(cmd.Context()), embCfg, "")
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("semantic note search disabled: %v", err))
		} else if _, statErr := os.Stat(embCfg.IndexPath); statErr == nil {
			err := embsqlite.ValidateExisting(cmd.Context(), embsqlite.File(embCfg.IndexPath), embsqlite.Notes, embeddings.MetadataForProvider(provider, providerCfg))
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					warnings = append(warnings, fmt.Sprintf("semantic note index metadata mismatch: %v", metaErr))
				} else {
					warnings = append(warnings, fmt.Sprintf("semantic note index unavailable at %s: %v", embCfg.IndexPath, err))
				}
			} else {
				noteProvider = provider
			}
		}
	}

	if codeEmbCfg.Enabled {
		provider, providerCfg, err := embeddings.NewProviderForConfig(vaultconfig.FromContext(cmd.Context()), codeEmbCfg, "")
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("semantic code search disabled: %v", err))
		} else if _, statErr := os.Stat(codeEmbCfg.IndexPath); statErr == nil {
			codeMetadata := embeddings.MetadataForProvider(provider, providerCfg)
			store, err := embsqlite.OpenCode(cmd.Context(), embsqlite.File(codeEmbCfg.IndexPath), embsqlite.Options{Access: embsqlite.ReadExisting, Dimensions: codeMetadata.Dimensions, Metadata: &codeMetadata})
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					warnings = append(warnings, fmt.Sprintf("semantic code index metadata mismatch: %v", metaErr))
				} else {
					warnings = append(warnings, fmt.Sprintf("semantic code index unavailable at %s: %v", codeEmbCfg.IndexPath, err))
				}
			} else {
				codeStore = store
				codeProvider = provider
			}
		}
	}
	defer func() {
		if codeStore != nil {
			_ = codeStore.Close()
		}
	}()

	seedHandles, err := resolveSeedHandles(cmd.Context(), pathArgs, vaultPath, intelStore, searchLimit)
	if err != nil {
		return nil, nil, warnings, err
	}

	spec := search.QuerySpec{
		Text:   query,
		Seeds:  seedHandles,
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: searchLimit},
	}

	enableVector := intelStore != nil && (codeProvider != nil || noteProvider != nil)
	enableIntel := intelStore != nil

	searcher := semantic.Searcher{
		CodeProvider: codeProvider,
		NoteProvider: noteProvider,
		IntelStore:   intelStore,
	}

	planner := searchplanner.Planner{
		Deps: searchplanner.Deps{
			Semantic:   &searcher,
			IntelStore: intelStore,
			VaultPath:  vaultPath,
			VaultDef:   vaultDef,
			NoteReader: func() obsidian.NoteReader {
				return &obsidian.Note{}
			}(),
		},
		Options: searchplanner.Options{
			EnableVector: enableVector,
			EnableIntel:  enableIntel,
			EnableGraph:  false,
			EnableRefs:   enableIntel,
			MaxPerOwner:  3,
		},
	}
	plan, err := planner.Plan(cmd.Context(), spec)
	if err != nil {
		return nil, nil, warnings, err
	}
	plan.Packer = &presentation.DefaultPacker{VaultPath: vaultPath, CodeRoot: vaultPath, Intel: intelStore}

	resp, err := plan.Search(cmd.Context(), spec)
	if err != nil {
		return nil, nil, warnings, err
	}

	var (
		fqns       []string
		pathPrefix []string
	)
	for _, r := range resp.Results {
		if r.Handle.Kind == knowledge.KindAnchor && strings.TrimSpace(r.FQN) != "" {
			fqns = append(fqns, strings.TrimSpace(r.FQN))
		}
		if r.Path != "" && (r.Handle.Kind == knowledge.KindAnchor || r.Handle.Kind == knowledge.KindFile) {
			pathPrefix = append(pathPrefix, strings.TrimSpace(r.Path))
		}
	}

	return dedupeStrings(fqns), normalizeDocCoveragePaths(vaultPath, pathPrefix), warnings, nil
}
