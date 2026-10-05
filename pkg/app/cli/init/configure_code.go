package init

import (
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// enableAutomaticCodeScope turns code indexing on without folder limits, so
// every language in the repository indexes, including code added later.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US9-AC1]]
func enableAutomaticCodeScope(cfg *obsidian.LocalConfig) {
	cfg.Code = obsidian.LocalCodeConfig{
		Enabled:           true,
		Scan:              cfg.Code.Scan,
		Ignore:            cfg.Code.Ignore,
		TSParseTimeout:    cfg.Code.TSParseTimeout,
		DisabledLanguages: cfg.Code.DisabledLanguages,
	}
}

// codeFolderLimits lists the folders config limits code indexing to; none
// means the whole repository.
func codeFolderLimits(code obsidian.LocalCodeConfig) []string {
	var out []string
	for _, block := range code.LanguageBlocks() {
		if *block != nil {
			out = append(out, (*block).Roots...)
		}
	}
	out = dedupePreserveOrder(out)
	if contains(out, ".") {
		return nil
	}
	return out
}

// removeCodeFolderLimits drops every language's folders and any language
// block left empty.
func removeCodeFolderLimits(code obsidian.LocalCodeConfig) obsidian.LocalCodeConfig {
	for _, block := range code.LanguageBlocks() {
		if *block == nil {
			continue
		}
		next := **block
		next.Roots = nil
		if len(next.Scan) == 0 && len(next.Ignore) == 0 {
			*block = nil
		} else {
			*block = &next
		}
	}
	code.Enabled = true
	return code
}

// proposedSkip reports whether init proposes skipping rel by its name, so
// content it would skip does not count as missing from folder limits.
func proposedSkip(rel string) bool {
	dir, _ := skipFolder(rel)
	return dir != "" || hasAnySuffix(rel, bundledSuffixes) || hasAnySuffix(rel, generatedSuffixes)
}

// codeOutsideLimits lists the top-level folders holding code files that the
// folder limits leave out, ignoring content init proposes to skip. "." stands
// for files at the top level.
func codeOutsideLimits(files, limits, disabled []string) []string {
	off := codeanchor.Config{DisabledLanguages: disabled}
	var outside []string
	for _, rel := range files {
		if lang, ok := codeanchor.LangForPath(rel); !ok || off.LanguageDisabled(lang) || rootCovered(limits, rel) ||
			proposedSkip(rel) {
			continue
		}
		dir, _, nested := strings.Cut(rel, "/")
		if !nested {
			dir = "."
		}
		outside = appendUnique(outside, dir)
	}
	sort.Strings(outside)
	return outside
}
