package init

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Utility functions

func splitList(input string) []string {
	parts := strings.Split(input, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Summary functions

func hasAnyCodeConfig(cfg obsidian.LocalCodeConfig) bool {
	return cfg.Enabled ||
		len(cfg.Scan) > 0 ||
		len(cfg.Ignore) > 0 ||
		cfg.Python != nil ||
		cfg.Go != nil ||
		cfg.TypeScript != nil ||
		cfg.JavaScript != nil ||
		cfg.CSharp != nil ||
		len(cfg.DisabledLanguages) > 0
}

func dedupePreserveOrder(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
