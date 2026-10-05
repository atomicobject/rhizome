package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var listVaultsCmd = &cobra.Command{
	Use: "list",
	Aliases: []string{
		"list-vaults",
	},
	Short: "List vault mappings stored in rhizome preferences",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaults, defaultName, err := obsidian.ListPreferenceVaults()
		if err != nil {
			return err
		}

		fmt.Println("Manual vaults (rhizome preferences):")
		if len(vaults) == 0 {
			fmt.Println("  (none)")
		} else {
			names := make([]string, 0, len(vaults))
			for name := range vaults {
				names = append(names, name)
			}
			sort.Strings(names)

			for _, name := range names {
				def := vaults[name]
				defaultMarker := ""
				if name == defaultName {
					defaultMarker = " (default)"
				}

				if def.IsCollection() {
					fmt.Printf("  %s%s [collection]\n", name, defaultMarker)
					fmt.Printf("    root: %s\n", def.Root)
					fmt.Printf("    includes: %s\n", strings.Join(def.Includes, ", "))
					if len(def.Excludes) > 0 {
						fmt.Printf("    excludes: %s\n", strings.Join(def.Excludes, ", "))
					}
					if def.Links != "" {
						fmt.Printf("    links: %s\n", def.Links)
					}
				} else {
					fmt.Printf("  %s%s -> %s\n", name, defaultMarker, def.Path)
					if def.Links != "" {
						fmt.Printf("    links: %s\n", def.Links)
					}
				}
			}
		}

		fmt.Println()
		fmt.Println("Obsidian app vaults (obsidian.json):")
		appVaults, err := obsidian.ListObsidianVaults()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read Obsidian vaults: %v\n", err)
			return nil
		}

		if len(appVaults) == 0 {
			fmt.Println("  (none)")
			return nil
		}

		keys := make([]string, 0, len(appVaults))
		for key := range appVaults {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			entry := appVaults[key]
			if entry.Path == "" {
				continue
			}
			name := filepath.Base(entry.Path)
			fmt.Printf("  %s -> %s\n", name, entry.Path)
		}
		return nil
	},
}

func init() {
	vaultCmd.AddCommand(listVaultsCmd)
}
