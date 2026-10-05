package cmd

import (
	"fmt"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	codeOverviewBudgetChars  int
	codeOverviewLimit        int
	codeOverviewIncludeTests bool
)

var codeOverviewCmd = &cobra.Command{
	Use:   "overview [roots...]",
	Short: "Summarize key code modules and attached docs as hierarchical markdown",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}
		vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
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

		var intel actions.CodeOverviewIntel
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
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}

		textResult, err := buildCodeOverviewText(cmd, intel, vaultDef, vaultPath, roots)
		if err != nil {
			return err
		}
		useColor := colorEnabled()
		fmt.Fprintln(cmd.OutOrStdout(), formatCodeOverviewOutput(textResult, useColor))
		return nil
	},
}

func buildCodeOverviewText(cmd *cobra.Command, intel actions.CodeOverviewIntel, vaultDef obsidian.VaultDefinition, vaultPath string, roots []string) (string, error) {
	fileCfg, _ := obsidian.FileContextConfigForVault(vaultPath)
	docPatterns := fileCfg.DocPatterns
	note, err := newProjectedNoteReader(cmd.Context(), vaultDef)
	if err != nil {
		return "", err
	}
	res, text, err := actions.CodeOverview(cmd.Context(), actions.CodeOverviewOptions{
		Vault:          &obsidian.Vault{Name: vaultName},
		Notes:          note,
		VaultDef:       vaultDef,
		Intel:          intel,
		Roots:          roots,
		DocPatterns:    docPatterns,
		MaxEmptyLevels: fileCfg.MaxEmptyLevels,
		BudgetChars:    codeOverviewBudgetChars,
		LimitModules:   codeOverviewLimit,
		IncludeTests:   codeOverviewIncludeTests,
	})
	if err != nil {
		return "", err
	}
	if res.Warning != "" && intel == nil {
		return res.Warning, nil
	}
	if res.Warning != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), res.Warning)
	}
	return text, nil
}

func init() {
	codeOverviewCmd.Flags().IntVar(&codeOverviewBudgetChars, "budget-chars", contextpack.DefaultBudgetChars, "max output size in characters")
	codeOverviewCmd.Flags().IntVar(&codeOverviewLimit, "limit-modules", 25, "maximum modules to include")
	codeOverviewCmd.Flags().BoolVar(&codeOverviewIncludeTests, "include-tests", false, "include test files in module summaries")
	codeCmd.AddCommand(codeOverviewCmd)
}

func formatCodeOverviewOutput(markdown string, useColor bool) string {
	if !useColor {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "# "):
			lines[i] = lightGreen + strings.TrimSpace(line) + reset
		case strings.HasPrefix(line, "#### "):
			lines[i] = "#### " + dimWhite + strings.TrimPrefix(line, "#### ") + reset
		case strings.HasPrefix(line, "### "):
			lines[i] = "### " + teal + strings.TrimPrefix(line, "### ") + reset
		case strings.HasPrefix(line, "## "):
			lines[i] = "## " + green + strings.TrimPrefix(line, "## ") + reset
		case strings.HasPrefix(line, "_files "):
			lines[i] = dimWhite + strings.TrimSpace(line) + reset
		case strings.HasPrefix(line, "- Docs: "):
			lines[i] = "- " + dimWhite + "Docs:" + reset + " " + colorizeDocList(strings.TrimPrefix(line, "- Docs: "))
		case strings.HasPrefix(line, "- Anchors: "):
			lines[i] = "- " + dimWhite + "Anchors:" + reset + " " + colorizeAnchors(strings.TrimPrefix(line, "- Anchors: "))
		case strings.HasPrefix(line, "- Tests: "):
			lines[i] = "- " + dimWhite + "Tests:" + reset + " " + dimWhite + strings.TrimPrefix(line, "- Tests: ") + reset
		case strings.HasPrefix(line, "(truncated"):
			lines[i] = dimWhite + strings.TrimSpace(line) + reset
		}
	}
	return strings.Join(lines, "\n")
}

func colorizeDocList(list string) string {
	if strings.TrimSpace(list) == "" {
		return ""
	}
	parts := strings.Split(list, "; ")
	for i, p := range parts {
		title := p
		summary := ""
		if idx := strings.Index(p, " — "); idx >= 0 {
			title = p[:idx]
			summary = p[idx+len(" — "):]
		}
		if summary != "" {
			parts[i] = teal + title + reset + " — " + dimWhite + summary + reset
		} else {
			parts[i] = teal + title + reset
		}
	}
	return strings.Join(parts, "; ")
}

func colorizeAnchors(list string) string {
	if strings.TrimSpace(list) == "" {
		return ""
	}
	parts := strings.Split(list, "; ")
	for i, p := range parts {
		anchor := strings.TrimSpace(p)
		name := anchor
		rest := ""
		if idx := strings.Index(anchor, " "); idx >= 0 {
			name = anchor[:idx]
			rest = strings.TrimSpace(anchor[idx+1:])
		}
		colored := teal + name + reset
		if rest != "" {
			colored += " " + dimWhite + rest + reset
		}
		parts[i] = colored
	}
	return strings.Join(parts, "; ")
}
