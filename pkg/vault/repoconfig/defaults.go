package repoconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
)

// DefaultCodeConfig values applied when omitted.
var DefaultCodeConfig = LocalCodeConfig{
	Scan:   []string{},
	Ignore: codepatterns.DefaultIgnoreGlobs(),
}

// FileContextConfigDefaults define defaults for file_context doc discovery.
var FileContextConfigDefaults = LocalFileContextConfig{
	DocPatterns:        []string{"CONTEXT.md"},
	MaxEmptyLevels:     3,
	ContextBudget:      DefaultBudgetChars,
	IncludeDocsInGraph: boolPtr(true),
	ExpandNoteLinks: []string{
		"tag:type/hub",
		"OR",
		"tag:maps-of-content",
		"OR",
		"tag:type/moc",
	},
	ExpandNoteLinksLimit: 8,
}

// FileContextConfigFromLocal applies defaults to a LocalFileContextConfig.
func FileContextConfigFromLocal(local LocalFileContextConfig) LocalFileContextConfig {
	result := LocalFileContextConfig{
		DocPatterns:          local.DocPatterns,
		MaxEmptyLevels:       local.MaxEmptyLevels,
		ContextBudget:        local.ContextBudget,
		IncludeDocsInGraph:   local.IncludeDocsInGraph,
		ExpandNoteLinks:      local.ExpandNoteLinks,
		ExpandNoteLinksLimit: local.ExpandNoteLinksLimit,
	}

	if len(result.DocPatterns) == 0 {
		result.DocPatterns = FileContextConfigDefaults.DocPatterns
	}
	if result.MaxEmptyLevels <= 0 {
		result.MaxEmptyLevels = FileContextConfigDefaults.MaxEmptyLevels
	}
	if result.ContextBudget <= 0 {
		result.ContextBudget = FileContextConfigDefaults.ContextBudget
	}
	if result.IncludeDocsInGraph == nil {
		result.IncludeDocsInGraph = FileContextConfigDefaults.IncludeDocsInGraph
	}
	if len(result.ExpandNoteLinks) == 0 {
		result.ExpandNoteLinks = FileContextConfigDefaults.ExpandNoteLinks
	}
	if result.ExpandNoteLinksLimit <= 0 {
		result.ExpandNoteLinksLimit = FileContextConfigDefaults.ExpandNoteLinksLimit
	}

	return result
}

// FileContextConfigFromLocalOrDefault returns defaults when the local config is missing.
func FileContextConfigFromLocalOrDefault(localCfg *LocalConfig) LocalFileContextConfig {
	if localCfg == nil {
		return FileContextConfigDefaults
	}
	return FileContextConfigFromLocal(localCfg.FileCtx)
}

// NormalizeCodeRefPatterns returns includes/excludes for coderefs scanning.
// Order of precedence:
// 1) code.scan / code.ignore
// 2) enabled language blocks and their defaults
// 3) disabled (no includes)
func NormalizeCodeRefPatterns(local LocalConfig) (includes, excludes []string) {
	// New config
	if len(local.Code.Scan) > 0 {
		includes = append([]string{}, local.Code.Scan...)
	}
	if len(local.Code.Ignore) > 0 {
		excludes = append([]string{}, local.Code.Ignore...)
	} else {
		excludes = append([]string{}, DefaultCodeConfig.Ignore...)
	}

	// Language blocks also imply coderefs for that language; add defaults.
	if local.Code.Python != nil {
		if len(local.Code.Python.Scan) > 0 {
			includes = append(includes, local.Code.Python.Scan...)
		} else {
			includes = append(includes, codepatterns.DefaultPythonGlobs()...)
		}
		if len(local.Code.Python.Ignore) > 0 {
			excludes = append(excludes, local.Code.Python.Ignore...)
		}
	}
	if local.Code.Go != nil {
		if len(local.Code.Go.Scan) > 0 {
			includes = append(includes, local.Code.Go.Scan...)
		} else {
			includes = append(includes, codepatterns.DefaultGoGlobs()...)
		}
		if len(local.Code.Go.Ignore) > 0 {
			excludes = append(excludes, local.Code.Go.Ignore...)
		}
	}
	if local.Code.TypeScript != nil || local.Code.JavaScript != nil {
		langCfgs := []*LocalCodeLangConfig{local.Code.TypeScript, local.Code.JavaScript}
		// If either block provides scan globs, prefer those; otherwise include default TS+JS globs.
		haveScan := false
		for _, cfg := range langCfgs {
			if cfg != nil && len(cfg.Scan) > 0 {
				haveScan = true
				includes = append(includes, cfg.Scan...)
			}
		}
		if !haveScan {
			includes = append(includes, codepatterns.DefaultTypeScriptGlobs()...)
		}
		for _, cfg := range langCfgs {
			if cfg != nil && len(cfg.Ignore) > 0 {
				excludes = append(excludes, cfg.Ignore...)
			}
		}
	}
	if local.Code.PHP != nil {
		if len(local.Code.PHP.Scan) > 0 {
			includes = append(includes, local.Code.PHP.Scan...)
		} else {
			includes = append(includes, codepatterns.DefaultPHPGlobs()...)
		}
		if len(local.Code.PHP.Ignore) > 0 {
			excludes = append(excludes, local.Code.PHP.Ignore...)
		}
	}

	// If explicitly enabled and no includes were specified, use defaults.
	if local.Code.Enabled && len(includes) == 0 {
		includes = append(includes, codepatterns.DefaultScanGlobs()...)
	}

	// Deduplicate empty values handled by caller.
	return includes, excludes
}

func boolPtr(b bool) *bool { return &b }

// EffectiveBudgetChars returns the budget from config or the default if unset.
func (c *LocalConfig) EffectiveBudgetChars() int {
	if c != nil && c.BudgetChars > 0 {
		return c.BudgetChars
	}
	return DefaultBudgetChars
}

// ScopeConfig contains only fields that affect index scan scope.
// Changes to these fields require re-evaluating which files should be indexed.
type ScopeConfig struct {
	NotesIncludes []string `json:"notesIncludes,omitempty"`
	NotesExcludes []string `json:"notesExcludes,omitempty"`
	CodeEnabled   bool     `json:"codeEnabled,omitempty"`
	PythonRoots   []string `json:"pythonRoots,omitempty"`
	PythonScan    []string `json:"pythonScan,omitempty"`
	PythonIgnore  []string `json:"pythonIgnore,omitempty"`
	GoRoots       []string `json:"goRoots,omitempty"`
	GoScan        []string `json:"goScan,omitempty"`
	GoIgnore      []string `json:"goIgnore,omitempty"`
	TSRoots       []string `json:"tsRoots,omitempty"`
	TSScan        []string `json:"tsScan,omitempty"`
	TSIgnore      []string `json:"tsIgnore,omitempty"`
	JSRoots       []string `json:"jsRoots,omitempty"`
	JSScan        []string `json:"jsScan,omitempty"`
	JSIgnore      []string `json:"jsIgnore,omitempty"`
	CSharpRoots   []string `json:"csharpRoots,omitempty"`
	CSharpScan    []string `json:"csharpScan,omitempty"`
	CSharpIgnore  []string `json:"csharpIgnore,omitempty"`
	PHPRoots      []string `json:"phpRoots,omitempty"`
	PHPScan       []string `json:"phpScan,omitempty"`
	PHPIgnore     []string `json:"phpIgnore,omitempty"`
	CodeScan      []string `json:"codeScan,omitempty"`
	CodeIgnore    []string `json:"codeIgnore,omitempty"`
}

// ScopeConfigHash returns a SHA256 hash of config fields that affect index scan scope.
// When this hash changes, files that were previously out-of-scope may now be in-scope
// and need to be indexed.
func (c *LocalConfig) ScopeConfigHash() string {
	if c == nil {
		return ""
	}

	sc := ScopeConfig{
		NotesIncludes: c.Notes.Includes,
		NotesExcludes: c.Notes.Excludes,
		CodeEnabled:   c.Code.Enabled,
		CodeScan:      c.Code.Scan,
		CodeIgnore:    c.Code.Ignore,
	}

	if c.Code.Python != nil {
		sc.PythonRoots = c.Code.Python.Roots
		sc.PythonScan = c.Code.Python.Scan
		sc.PythonIgnore = c.Code.Python.Ignore
	}
	if c.Code.Go != nil {
		sc.GoRoots = c.Code.Go.Roots
		sc.GoScan = c.Code.Go.Scan
		sc.GoIgnore = c.Code.Go.Ignore
	}
	if c.Code.TypeScript != nil {
		sc.TSRoots = c.Code.TypeScript.Roots
		sc.TSScan = c.Code.TypeScript.Scan
		sc.TSIgnore = c.Code.TypeScript.Ignore
	}
	if c.Code.JavaScript != nil {
		sc.JSRoots = c.Code.JavaScript.Roots
		sc.JSScan = c.Code.JavaScript.Scan
		sc.JSIgnore = c.Code.JavaScript.Ignore
	}
	if c.Code.CSharp != nil {
		sc.CSharpRoots = c.Code.CSharp.Roots
		sc.CSharpScan = c.Code.CSharp.Scan
		sc.CSharpIgnore = c.Code.CSharp.Ignore
	}
	if c.Code.PHP != nil {
		sc.PHPRoots = c.Code.PHP.Roots
		sc.PHPScan = c.Code.PHP.Scan
		sc.PHPIgnore = c.Code.PHP.Ignore
	}

	data, err := json.Marshal(sc)
	if err != nil {
		return ""
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// DefaultBudgetChars bounds generated agent context when no override is configured.
const DefaultBudgetChars = 70000
