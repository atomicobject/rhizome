package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/spf13/cobra"
)

var (
	codeRelatednessJSON             bool
	codeRelatednessMaxRelated       int
	codeRelatednessClusterThreshold int
	codeRelatednessIncludeTests     bool
	codeRelatednessCriticalLimit    int
	codeRelatednessHotFilesLimit    int
	codeRelatednessCriticalVerbose  bool
)

var codeRelatednessCmd = &cobra.Command{
	Use:   "relatedness [roots...]",
	Short: "Report code relatedness and refactor hotspots under one or more roots",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}

		var roots []string
		for _, a := range args {
			if strings.TrimSpace(a) == "" {
				continue
			}
			absRoot, err := resolveCodeRoot(vaultPath, a)
			if err != nil {
				return err
			}
			if absRoot == "" {
				continue
			}
			roots = append(roots, absRoot)
		}
		if len(roots) == 0 {
			return fmt.Errorf("at least one root is required")
		}

		var intel actions.CodeRelatednessIntel
		store, cleanup, warning, err := codeintel.OpenOptionalIntelStore(vaultPath, codeCfg)
		if err != nil {
			return err
		}
		if cleanup != nil {
			defer cleanup()
		}
		if store != nil {
			intel = store
		}
		if strings.TrimSpace(warning) != "" {
			warning = strings.Replace(warning, "without intel.", "without call/anchor intel.", 1)
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}

		vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
		if err != nil {
			return err
		}
		report, err := actions.CodeRelatedness(cmd.Context(), intel, actions.CodeRelatednessOptions{
			VaultPath:        vaultPath,
			Roots:            roots,
			Excludes:         vaultDef.Excludes,
			MaxRelated:       codeRelatednessMaxRelated,
			ClusterThreshold: codeRelatednessClusterThreshold,
			IncludeTests:     codeRelatednessIncludeTests,
			CriticalLimit:    codeRelatednessCriticalLimit,
			HotFilesLimit:    codeRelatednessHotFilesLimit,
		})
		if err != nil {
			return err
		}

		if codeRelatednessJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(report)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Code relatedness (roots: %s)\n", strings.Join(args, ", "))
		if report.Module != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Module: %s\n", report.Module)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Packages: %d\n", len(report.Packages))
		if len(report.Clusters) > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "Clusters (score >= %d):\n", codeRelatednessClusterThreshold)
			for i, c := range report.Clusters {
				fmt.Fprintf(cmd.OutOrStdout(), "%2d. %s\n", i+1, strings.Join(shortenPkgs(report.Module, c.Packages), ", "))
			}
		}

		if len(report.CouplingEdges) > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Strongest coupling edges (directed; cross-package calls):")
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "Calls\tImpFiles\tImports\tBothWays\tFrom\tTo")
			for _, e := range report.CouplingEdges {
				fmt.Fprintf(w, "%d\t%d\t%t\t%t\t%s\t%s\n",
					e.CallsTo, e.ImportFiles, e.Imports, e.BothWays, displayPkg(shortenPkg(report.Module, e.FromPkg)), displayPkg(shortenPkg(report.Module, e.ToPkg)),
				)
			}
			_ = w.Flush()

			// Edge explanations: what symbols/files drive the coupling.
			fmt.Fprintln(cmd.OutOrStdout())
			maxExplain := 10
			if len(report.CouplingEdges) < maxExplain {
				maxExplain = len(report.CouplingEdges)
			}
			for i := 0; i < maxExplain; i++ {
				e := report.CouplingEdges[i]
				if len(e.TopCallees) == 0 && len(e.TopCallerFiles) == 0 {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s → %s (calls=%d)\n",
					shortenPkg(report.Module, e.FromPkg),
					shortenPkg(report.Module, e.ToPkg),
					e.CallsTo,
				)
				if len(e.TopCallees) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "- callees: %s\n", formatCouplingCallees(report.Module, e.TopCallees))
				}
				if len(e.TopImportFiles) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "- imports: %s\n", formatCouplingCallerFiles(e.TopImportFiles))
				}
				if len(e.TopCallerFiles) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "- callers: %s\n", formatCouplingCallerFiles(e.TopCallerFiles))
				}
			}
		}

		if len(report.GodCandidates) > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "God candidates (highest combined hotspot score):")
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "Score\tImpIn\tImpOut\tCallIn\tCallOut\tAnchors\tNotes\tFiles\tPackage\tDir")
			for _, g := range report.GodCandidates {
				fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n",
					g.CombinedGodScore, g.ImportFanIn, g.ImportFanOut, g.CallFanIn, g.CallFanOut, g.AnchorLabels, g.LinkedNotes, g.Files,
					displayPkg(g.Package), g.Dir,
				)
			}
			_ = w.Flush()

			// Print top related packages for each god candidate (if present).
			pkgByImport := make(map[string]actions.CodeRelatednessPackage, len(report.Packages))
			for _, p := range report.Packages {
				pkgByImport[p.ImportPath] = p
			}
			fmt.Fprintln(cmd.OutOrStdout())
			for _, g := range report.GodCandidates {
				p, ok := pkgByImport[g.Package]
				if !ok || len(p.TopRelated) == 0 {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s top related:\n", displayPkg(p.ImportPath))
				for _, e := range p.TopRelated {
					fmt.Fprintf(cmd.OutOrStdout(), "- %s (score=%d, imports=%d, calls=%d, callsTo=%d, shared_anchors=%d)\n",
						displayPkg(e.To), e.Score, e.ImportsBothWays, e.CallsBothWays, e.CallsTo, e.SharedAnchorLabels,
					)
				}
				if len(p.HotFiles) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "%s hot files (cross-package call fan-out):\n", displayPkg(p.ImportPath))
					for _, f := range p.HotFiles {
						pkgsStr := ""
						if len(f.CalleePkgs) > 0 {
							pkgsStr = fmt.Sprintf(" pkgs=%s", strings.Join(shortenPkgs(report.Module, f.CalleePkgs), ","))
						}
						fmt.Fprintf(cmd.OutOrStdout(), "- %s (deps=%d, calls=%d)%s\n", f.File, f.DistinctCallees, f.CallsOutOfPkg, pkgsStr)
					}
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
		}

		if len(report.CriticalSymbols) > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "Critical symbols (out-of-package calls; top %d):\n", min(len(report.CriticalSymbols), codeRelatednessCriticalLimit))
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "Calls\tCallerPkgs\tTopCaller%\tMentions\tLinks\tAnchors\tNotes\tFQN\tPath")
			for _, s := range report.CriticalSymbols {
				topPct := int(s.TopCallerShare * 100)
				fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n",
					s.Calls, s.CallerPackages, topPct, s.DocMentions, s.DocLinks, s.MatchingAnchors, s.MatchingNotes,
					s.FQN, relToVault(vaultPath, s.DocPath),
				)
			}
			_ = w.Flush()

			fmt.Fprintln(cmd.OutOrStdout())
			for _, s := range report.CriticalSymbols {
				if len(s.TopCallerPackages) == 0 && len(s.AnchorLabels) == 0 && len(s.NotePaths) == 0 {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", s.FQN)
				if len(s.TopCallerPackages) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "- callers: %s\n", formatCallerPkgs(report.Module, s.TopCallerPackages))
				}
				if len(s.TopCallerFiles) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "- files: %s\n", formatCallerFiles(s.TopCallerFiles))
				}
				if len(s.AnchorLabels) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "- anchors: %s\n", strings.Join(s.AnchorLabels, ", "))
				}
				if codeRelatednessCriticalVerbose && len(s.NotePaths) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "- notes: %s\n", strings.Join(s.NotePaths, ", "))
				}
			}
		}

		if len(report.Packages) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(No indexed code files found under the provided roots.)")
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Notes:")
		fmt.Fprintln(cmd.OutOrStdout(), "- `ImpIn/ImpOut` are internal import fan-in/out across discovered packages (from Go imports when available).")
		fmt.Fprintln(cmd.OutOrStdout(), "- `CallIn/CallOut`, `Anchors`, graph signals, and `Notes` require a built code index (`rzm index`).")
		return nil
	},
}

func init() {
	codeRelatednessCmd.Flags().BoolVar(&codeRelatednessJSON, "json", false, "Output results as JSON")
	codeRelatednessCmd.Flags().IntVar(&codeRelatednessMaxRelated, "max-related", 8, "Max related packages to show per package")
	codeRelatednessCmd.Flags().IntVar(&codeRelatednessClusterThreshold, "cluster-threshold", 3, "Minimum relatedness score to connect packages into clusters")
	codeRelatednessCmd.Flags().BoolVar(&codeRelatednessIncludeTests, "include-tests", false, "Include _test.go files when discovering packages")
	codeRelatednessCmd.Flags().IntVar(&codeRelatednessCriticalLimit, "critical-limit", 20, "Max critical out-of-package symbols to include")
	codeRelatednessCmd.Flags().IntVar(&codeRelatednessHotFilesLimit, "hot-files-limit", 5, "Max hot files per package to include")
	codeRelatednessCmd.Flags().BoolVar(&codeRelatednessCriticalVerbose, "critical-verbose", false, "Include anchor label + note-path details for critical symbols")

	codeCmd.AddCommand(codeRelatednessCmd)
}

func shortenPkg(module, pkg string) string {
	if module == "" {
		return pkg
	}
	return strings.TrimPrefix(pkg, module+"/")
}

func formatCallerPkgs(module string, pkgs []actions.CodeRelatednessCallerPkg) string {
	if len(pkgs) == 0 {
		return ""
	}
	var parts []string
	for _, p := range pkgs {
		parts = append(parts, fmt.Sprintf("%s:%d(%df)", shortenPkg(module, p.Pkg), p.Calls, p.Files))
	}
	return strings.Join(parts, ", ")
}

func formatCallerFiles(files []actions.CodeRelatednessCallerFile) string {
	if len(files) == 0 {
		return ""
	}
	max := 4
	if len(files) < max {
		max = len(files)
	}
	var parts []string
	for i := 0; i < max; i++ {
		parts = append(parts, fmt.Sprintf("%s:%d", files[i].File, files[i].Calls))
	}
	return strings.Join(parts, ", ")
}

func formatCouplingCallees(module string, callees []actions.CodeRelatednessCouplingCallee) string {
	if len(callees) == 0 {
		return ""
	}
	max := 5
	if len(callees) < max {
		max = len(callees)
	}
	var parts []string
	for i := 0; i < max; i++ {
		fqn := callees[i].FQN
		fqn = strings.TrimPrefix(fqn, module+"/")
		// Keep it compact: show anchors if present.
		prefix := ""
		if callees[i].Kind != "" {
			prefix = callees[i].Kind + " "
		}
		fileStr := ""
		if callees[i].File != "" && callees[i].File != "." {
			fileStr = "@" + callees[i].File
		}
		if len(callees[i].Anchors) > 0 {
			parts = append(parts, fmt.Sprintf("%s%s%s:%d[%s]", prefix, fqn, fileStr, callees[i].Calls, strings.Join(callees[i].Anchors, ",")))
		} else {
			parts = append(parts, fmt.Sprintf("%s%s%s:%d", prefix, fqn, fileStr, callees[i].Calls))
		}
	}
	return strings.Join(parts, ", ")
}

func formatCouplingCallerFiles(files []actions.CodeRelatednessCouplingCallerFile) string {
	if len(files) == 0 {
		return ""
	}
	max := 5
	if len(files) < max {
		max = len(files)
	}
	var parts []string
	for i := 0; i < max; i++ {
		parts = append(parts, fmt.Sprintf("%s:%d", files[i].File, files[i].Calls))
	}
	return strings.Join(parts, ", ")
}

func shortenPkgs(module string, pkgs []string) []string {
	if module == "" {
		out := make([]string, len(pkgs))
		copy(out, pkgs)
		return out
	}
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		if strings.HasPrefix(p, module+"/") {
			out = append(out, strings.TrimPrefix(p, module+"/"))
			continue
		}
		out = append(out, p)
	}
	return out
}
