package obsidian

import (
	"errors"
	"slices"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// LoadCodeConfig returns code indexing config merged with defaults for the vault.
func LoadCodeConfig(vaultPath string) (codeanchor.Config, error) {
	base := codeanchor.DefaultConfig(vaultPath)

	localCfg, err := LoadLocalConfig(vaultPath)
	if err != nil {
		if errors.Is(err, ErrNoLocalConfig) {
			return base, nil
		}
		return codeanchor.Config{}, err
	}
	base.IndexPath = unifiedIndexPath(vaultPath, localCfg.IndexPath)
	return mergeLocalCodeToAnchorConfig(base, localCfg.Code), nil
}

// CodeAnchorRootsConfigured reports whether local code config gives at least
// one code-anchor language non-empty roots and that language is not listed in
// code.disabledLanguages. Without roots, indexing cannot produce code anchors.
func CodeAnchorRootsConfigured(local LocalCodeConfig) bool {
	cfg := mergeLocalCodeToAnchorConfig(codeanchor.DefaultConfig(""), local)
	languages := []struct {
		id    string
		roots []string
	}{
		{"python", cfg.PythonRoots},
		{"go", cfg.GoRoots},
		{"ts", cfg.TSRoots},
		{"cs", cfg.CSharpRoots},
		{"php", cfg.PHPRoots},
	}
	for _, language := range languages {
		if len(language.roots) > 0 && !slices.Contains(cfg.DisabledLanguages, language.id) {
			return true
		}
	}
	return false
}

// mergeLocalCodeToAnchorConfig converts LocalCodeConfig to codeanchor.Config format.
func mergeLocalCodeToAnchorConfig(base codeanchor.Config, local LocalCodeConfig) codeanchor.Config {
	result := base

	// Check if any code config is present
	if !local.IndexesCode() {
		return result
	}

	result.Enabled = true

	// Copy disabled languages
	if len(local.DisabledLanguages) > 0 {
		result.DisabledLanguages = local.DisabledLanguages
	}
	if local.TSParseTimeout > 0 {
		result.TSParseTimeout = local.TSParseTimeout
	}

	// Python
	if local.Python != nil {
		if len(local.Python.Roots) > 0 {
			result.PythonRoots = local.Python.Roots
		}
		if len(local.Python.Scan) > 0 {
			result.PythonScan = local.Python.Scan
		}
		if len(local.Python.Ignore) > 0 {
			result.PythonIgnore = local.Python.Ignore
		}
	}

	// Go
	if local.Go != nil {
		if len(local.Go.Roots) > 0 {
			result.GoRoots = local.Go.Roots
		}
		if len(local.Go.Scan) > 0 {
			result.GoScan = local.Go.Scan
		}
		if len(local.Go.Ignore) > 0 {
			result.GoIgnore = local.Go.Ignore
		}
	}

	// TypeScript (maps to TS in codeanchor)
	if local.TypeScript != nil {
		if len(local.TypeScript.Roots) > 0 {
			result.TSRoots = local.TypeScript.Roots
		}
		if len(local.TypeScript.Scan) > 0 {
			result.TSScan = local.TypeScript.Scan
		}
		if len(local.TypeScript.Ignore) > 0 {
			result.TSIgnore = local.TypeScript.Ignore
		}
	}

	// JavaScript (also maps to TS in codeanchor - they share the same indexer)
	if local.JavaScript != nil {
		if len(local.JavaScript.Roots) > 0 && len(result.TSRoots) == 0 {
			result.TSRoots = local.JavaScript.Roots
		}
		if len(local.JavaScript.Scan) > 0 && len(result.TSScan) == 0 {
			result.TSScan = local.JavaScript.Scan
		}
		if len(local.JavaScript.Ignore) > 0 && len(result.TSIgnore) == 0 {
			result.TSIgnore = local.JavaScript.Ignore
		}
	}

	// CSharp
	if local.CSharp != nil {
		if len(local.CSharp.Roots) > 0 {
			result.CSharpRoots = local.CSharp.Roots
		}
		if len(local.CSharp.Scan) > 0 {
			result.CSharpScan = local.CSharp.Scan
		}
		if len(local.CSharp.Ignore) > 0 {
			result.CSharpIgnore = local.CSharp.Ignore
		}
	}

	// PHP
	if local.PHP != nil {
		if len(local.PHP.Roots) > 0 {
			result.PHPRoots = local.PHP.Roots
		}
		if len(local.PHP.Scan) > 0 {
			result.PHPScan = local.PHP.Scan
		}
		if len(local.PHP.Ignore) > 0 {
			result.PHPIgnore = local.PHP.Ignore
		}
	}

	applyAutomaticCodeScope(&result, local)
	return result
}

// applyAutomaticCodeScope gives languages without code folders the whole
// project. When no language names folders, every language covers the whole
// project, so code added after setup indexes without rerunning init. When
// some language names folders, those folders limit code indexing and only
// language blocks without folders cover the whole project.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US9-AC1]]
func applyAutomaticCodeScope(result *codeanchor.Config, local LocalCodeConfig) {
	ts := local.TypeScript
	if ts == nil {
		ts = local.JavaScript
	}
	languages := []struct {
		block *LocalCodeLangConfig
		roots *[]string
	}{
		{local.Python, &result.PythonRoots},
		{local.Go, &result.GoRoots},
		{ts, &result.TSRoots},
		{local.CSharp, &result.CSharpRoots},
		{local.PHP, &result.PHPRoots},
	}
	limited := false
	for _, block := range local.LanguageBlocks() {
		if *block != nil && len((*block).Roots) > 0 {
			limited = true
		}
	}
	for _, language := range languages {
		if len(*language.roots) == 0 && (!limited || language.block != nil) {
			*language.roots = []string{"."}
		}
	}
	result.AutomaticScope = !limited
}

// SaveCodeConfig updates the canonical code section in local config.
// It persists only the enabled flag and disabled languages to keep config minimal.
func SaveCodeConfig(vaultPath string, codeCfg codeanchor.Config) error {
	localCfg, err := LoadLocalConfig(vaultPath)
	if err != nil {
		return err
	}

	// Only persist enabled flag and disabled languages; language-specific config
	// (Go/Python/TS/CSharp/PHP blocks) are preserved as-is from the existing config.
	localCfg.Code.Enabled = codeCfg.Enabled
	if len(codeCfg.DisabledLanguages) > 0 {
		localCfg.Code.DisabledLanguages = codeCfg.DisabledLanguages
	}

	return SaveLocalConfig(vaultPath, *localCfg)
}
