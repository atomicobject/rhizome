package codeanchor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParserConfigHashIncludesResolvedParserSettings(t *testing.T) {
	base := DefaultConfig("/fixture")
	for name, change := range map[string]func(*Config){
		"enablement":        func(c *Config) { c.Enabled = true },
		"automatic scope":   func(c *Config) { c.AutomaticScope = true },
		"disabled language": func(c *Config) { c.DisabledLanguages = []string{"python"} },
		"python roots":      func(c *Config) { c.PythonRoots = []string{"."} },
		"go roots":          func(c *Config) { c.GoRoots = []string{"."} },
		"typescript roots":  func(c *Config) { c.TSRoots = []string{"."} },
		"csharp roots":      func(c *Config) { c.CSharpRoots = []string{"."} },
		"php roots":         func(c *Config) { c.PHPRoots = []string{"."} },
		"parse limits":      func(c *Config) { c.TSParseTimeout = time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			change(&changed)
			require.NotEqual(t, base.ParserConfigHash(), changed.ParserConfigHash())
		})
	}
}
