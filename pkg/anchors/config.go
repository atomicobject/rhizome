package codeanchor

// Docs: [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)

import (
	"path/filepath"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
)

// Config holds code indexing settings for a vault.
type Config struct {
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	IndexPath string `json:"indexPath,omitempty" yaml:"indexPath,omitempty"` // ignored when a unified index path is configured at the vault level

	// DisabledLanguages records codeanchor language IDs the user never wants to
	// be prompted to enable/configure for this vault/repo (init/index flows).
	// Under automatic scope their indexers are also left out.
	//
	// Supported language IDs: "python", "go", "ts", "cs".
	DisabledLanguages []string `json:"disabledLanguages,omitempty" yaml:"disabledLanguages,omitempty"`

	PythonRoots      []string      `json:"pythonRoots,omitempty" yaml:"pythonRoots,omitempty"`           // module roots for python code
	PythonScan       []string      `json:"pythonScan,omitempty" yaml:"pythonScan,omitempty"`             // optional override globs for python
	PythonIgnore     []string      `json:"pythonIgnore,omitempty" yaml:"pythonIgnore,omitempty"`         // optional exclusions for python
	GoRoots          []string      `json:"goRoots,omitempty" yaml:"goRoots,omitempty"`                   // module roots for go code
	GoScan           []string      `json:"goScan,omitempty" yaml:"goScan,omitempty"`                     // optional override globs for go
	GoIgnore         []string      `json:"goIgnore,omitempty" yaml:"goIgnore,omitempty"`                 // optional exclusions for go
	TSRoots          []string      `json:"tsRoots,omitempty" yaml:"tsRoots,omitempty"`                   // module roots for ts/js code
	TSScan           []string      `json:"tsScan,omitempty" yaml:"tsScan,omitempty"`                     // optional override globs for ts/js
	TSIgnore         []string      `json:"tsIgnore,omitempty" yaml:"tsIgnore,omitempty"`                 // optional exclusions for ts/js
	CSharpRoots      []string      `json:"csharpRoots,omitempty" yaml:"csharpRoots,omitempty"`           // module roots for csharp code
	CSharpScan       []string      `json:"csharpScan,omitempty" yaml:"csharpScan,omitempty"`             // optional override globs for csharp
	CSharpIgnore     []string      `json:"csharpIgnore,omitempty" yaml:"csharpIgnore,omitempty"`         // optional exclusions for csharp
	PHPRoots         []string      `json:"phpRoots,omitempty" yaml:"phpRoots,omitempty"`                 // module roots for PHP code
	PHPScan          []string      `json:"phpScan,omitempty" yaml:"phpScan,omitempty"`                   // optional override globs for PHP
	PHPIgnore        []string      `json:"phpIgnore,omitempty" yaml:"phpIgnore,omitempty"`               // optional exclusions for PHP
	RecomputeTimeout time.Duration `json:"recomputeTimeout,omitempty" yaml:"recomputeTimeout,omitempty"` // timeout for initial scope recompute
	TSParseTimeout   time.Duration `json:"tsParseTimeout,omitempty" yaml:"tsParseTimeout,omitempty"`     // tree-sitter parse timeout

	// AutomaticScope means config names no code folders, so code indexing
	// covers the whole project (every language root is ".") and module
	// naming uses each indexer's default source folders.
	AutomaticScope bool `json:"-" yaml:"-"`
}

// LanguageDisabled reports whether code.disabledLanguages lists lang. The
// list uses "python" where the indexer uses "py".
func (c Config) LanguageDisabled(lang Lang) bool {
	for _, id := range c.DisabledLanguages {
		if Lang(id) == lang || id == "python" && lang == LangPy {
			return true
		}
	}
	return false
}

// CodeRoots returns the code folders of every language, without duplicates.
// Code ownership is their union; each file's language comes from its
// extension.
func (c Config) CodeRoots() []string {
	seen := map[string]bool{}
	var out []string
	for _, roots := range [][]string{c.PythonRoots, c.GoRoots, c.TSRoots, c.CSharpRoots, c.PHPRoots} {
		for _, root := range roots {
			if key := filepath.Clean(filepath.FromSlash(root)); !seen[key] {
				seen[key] = true
				out = append(out, root)
			}
		}
	}
	return out
}

// DefaultRecomputeTimeout is the default timeout for initial scope recompute.
const DefaultRecomputeTimeout = 10 * time.Second

// DefaultConfig returns sensible defaults for code indexing.
func DefaultConfig(vaultPath string) Config {
	return Config{
		Enabled:           false,
		IndexPath:         DefaultIndexPath(vaultPath),
		DisabledLanguages: nil,
		PythonRoots:       []string{}, // empty; user must configure
		PythonScan:        codepatterns.DefaultPythonGlobs(),
		GoRoots:           []string{}, // empty; user must configure
		GoScan:            codepatterns.DefaultGoGlobs(),
		TSRoots:           []string{}, // empty; user must configure
		TSScan:            codepatterns.DefaultTypeScriptGlobs(),
		CSharpRoots:       []string{}, // empty; user must configure
		CSharpScan:        codepatterns.DefaultCSharpGlobs(),
		PHPRoots:          []string{}, // empty; user must configure
		PHPScan:           codepatterns.DefaultPHPGlobs(),
		RecomputeTimeout:  DefaultRecomputeTimeout,
	}
}

// DefaultIndexPath returns the default path for the code index database.
func DefaultIndexPath(vaultPath string) string {
	return filepath.Join(vaultPath, ".rhizome", "db.sqlite")
}

// Merge combines cfg with overrides, preferring non-zero override values.
func (c Config) Merge(override Config) Config {
	result := c
	if override.IndexPath != "" {
		result.IndexPath = override.IndexPath
	}
	if len(override.DisabledLanguages) > 0 {
		result.DisabledLanguages = override.DisabledLanguages
	}
	if len(override.PythonRoots) > 0 {
		result.PythonRoots = override.PythonRoots
	}
	if len(override.PythonScan) > 0 {
		result.PythonScan = override.PythonScan
	}
	if len(override.PythonIgnore) > 0 {
		result.PythonIgnore = override.PythonIgnore
	}
	if len(override.GoRoots) > 0 {
		result.GoRoots = override.GoRoots
	}
	if len(override.GoScan) > 0 {
		result.GoScan = override.GoScan
	}
	if len(override.GoIgnore) > 0 {
		result.GoIgnore = override.GoIgnore
	}
	if len(override.TSRoots) > 0 {
		result.TSRoots = override.TSRoots
	}
	if len(override.TSScan) > 0 {
		result.TSScan = override.TSScan
	}
	if len(override.TSIgnore) > 0 {
		result.TSIgnore = override.TSIgnore
	}
	if len(override.CSharpRoots) > 0 {
		result.CSharpRoots = override.CSharpRoots
	}
	if len(override.CSharpScan) > 0 {
		result.CSharpScan = override.CSharpScan
	}
	if len(override.CSharpIgnore) > 0 {
		result.CSharpIgnore = override.CSharpIgnore
	}
	if len(override.PHPRoots) > 0 {
		result.PHPRoots = override.PHPRoots
	}
	if len(override.PHPScan) > 0 {
		result.PHPScan = override.PHPScan
	}
	if len(override.PHPIgnore) > 0 {
		result.PHPIgnore = override.PHPIgnore
	}
	if override.RecomputeTimeout > 0 {
		result.RecomputeTimeout = override.RecomputeTimeout
	}
	if override.TSParseTimeout > 0 {
		result.TSParseTimeout = override.TSParseTimeout
	}
	return result
}

// TreeSitterLimits returns the configured tree-sitter limits.
func (c Config) TreeSitterLimits() TreeSitterLimits {
	return TreeSitterLimits{
		ParseTimeout: c.TSParseTimeout,
	}
}

// EffectiveRecomputeTimeout returns the configured timeout or the default.
func (c Config) EffectiveRecomputeTimeout() time.Duration {
	if c.RecomputeTimeout > 0 {
		return c.RecomputeTimeout
	}
	return DefaultRecomputeTimeout
}
