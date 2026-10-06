// First-run options for scripts and the desktop app: add-on starters, skip
// edits, and a search key, so one non-interactive run makes every choice the
// desktop setup sheet offers.
//
// Docs: [[desktop-repository-setup#^SPEC-0118-US1]]
package init

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ErrNotFirstRun reports a first-run option or machine-readable output on a
// folder that is already set up.
var ErrNotFirstRun = errors.New("this folder is already set up for Rhizome; --json, --addons, --skip, --keep-indexed, and --search-key-stdin apply only to first-run setup")

func hasFirstRunOption(opts RunOptions) bool {
	return strings.TrimSpace(opts.Addons) != "" || len(opts.Skip) > 0 || len(opts.KeepIndexed) > 0 || strings.TrimSpace(opts.SearchKey) != ""
}

// skipOptions validates --skip and --keep-indexed paths. A skipped path must
// exist; a kept path only needs to be inside the repository.
func skipOptions(root string, opts RunOptions) ([]skip, []string, error) {
	var skips []skip
	for _, input := range opts.Skip {
		rel, err := existingSkipPath(root, input)
		if err != nil {
			return nil, nil, err
		}
		skips = append(skips, skip{path: rel, reason: reasonManual})
	}
	var keep []string
	for _, input := range opts.KeepIndexed {
		rel := cleanSkipPath(input)
		if rel == "" {
			return nil, nil, fmt.Errorf("%s is not a path in this repository", input)
		}
		keep = append(keep, rel)
	}
	return skips, keep, nil
}

// existingSkipPath turns input into a root-relative skip path, with a
// trailing slash for a folder.
func existingSkipPath(root, input string) (string, error) {
	rel := cleanSkipPath(input)
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if rel == "" || err != nil {
		return "", fmt.Errorf("%s is not a folder or file in this repository", input)
	}
	if info.IsDir() {
		rel += "/"
	}
	return rel, nil
}

// chooseAddons records --addons: listed add-ons install, and default add-ons
// of the chosen workflows that are not listed are disabled.
func chooseAddons(s *setup, raw string) error {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return err
	}
	listed, err := parseAddons(raw, registry)
	if err != nil {
		return err
	}
	set, err := resolveTemplateSet(s.workflows, obsidian.WorkflowTemplateAddons{}, registry)
	if err != nil {
		return err
	}
	var disabled []string
	for _, id := range set.Effective {
		if set.Causes[id] == templateCauseDefaultAddon && !contains(listed, id) {
			disabled = append(disabled, id)
		}
	}
	s.cfg.WorkflowTemplateAddons = obsidian.WorkflowTemplateAddons{Enabled: normalizeMetadataIDs(listed), Disabled: normalizeMetadataIDs(disabled)}
	return nil
}

// parseAddons turns --addons into add-on ids: a comma-separated list, or
// none for no add-ons.
func parseAddons(raw string, registry map[string]starterTemplateMetadata) ([]string, error) {
	if strings.EqualFold(strings.TrimSpace(raw), "none") {
		return nil, nil
	}
	available := addonIDs(registry)
	var listed []string
	for _, name := range splitList(raw) {
		id := canonicalTemplateID(name)
		if !contains(available, id) {
			return nil, fmt.Errorf("unknown add-on %q for --addons; use %s, or none", name, strings.Join(available, ", "))
		}
		listed = append(listed, id)
	}
	return listed, nil
}

// checkAddonsOption rejects unknown --addons ids before a run writes
// anything, including a key from --search-key-stdin.
func checkAddonsOption(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return err
	}
	_, err = parseAddons(raw, registry)
	return err
}

// addonIDs lists starters that some starter activates by default.
func addonIDs(registry map[string]starterTemplateMetadata) []string {
	set := map[string]bool{}
	for _, meta := range registry {
		for _, id := range meta.ActivatesByDefault {
			set[id] = true
		}
	}
	ids := sortedStringSet(set)
	sort.Strings(ids)
	return ids
}
