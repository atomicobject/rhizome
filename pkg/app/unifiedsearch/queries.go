package unifiedsearch

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
)

func JoinQueries(primary string, extras []string) string {
	return JoinQueryInputs(NormalizeQueryInputs(primary, extras, ""))
}

func NormalizeQueryInputs(primary string, extras []string, mode string) []QueryInput {
	items := make([]string, 0, 1+len(extras))
	seen := make(map[string]struct{}, 1+len(extras))

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		items = append(items, s)
	}

	add(primary)
	for _, q := range extras {
		add(q)
	}
	out := make([]QueryInput, 0, len(items))
	for _, item := range items {
		out = append(out, QueryInput{Text: item, Mode: strings.TrimSpace(mode)})
	}
	return out
}

// NormalizeExplicitQueryInputs canonicalizes explicit facets before they are
// used for request identity, retrieval, or answer coverage. Empty modes inherit
// the effective request intent so equivalent facets share one lane.
func NormalizeExplicitQueryInputs(inputs []QueryInput, defaultMode string) []QueryInput {
	out := make([]QueryInput, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	defaultMode = strings.TrimSpace(defaultMode)
	if defaultMode == "" {
		defaultMode = string(search.IntentSearch)
	}
	for _, input := range inputs {
		input.Text = strings.TrimSpace(input.Text)
		input.Mode = strings.TrimSpace(input.Mode)
		if input.Text == "" {
			continue
		}
		if input.Mode == "" {
			input.Mode = defaultMode
		}
		key := input.Mode + "\x00" + input.Text
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, input)
	}
	return out
}

func JoinQueryInputs(inputs []QueryInput) string {
	items := make([]string, 0, len(inputs))
	for _, input := range inputs {
		text := strings.TrimSpace(input.Text)
		if text != "" {
			items = append(items, text)
		}
	}
	return strings.Join(items, "\n\n")
}

func MergeSeedTokens(seeds, files []string) []string {
	out := make([]string, 0, len(seeds)+len(files))
	seen := make(map[string]struct{}, len(seeds)+len(files))
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range seeds {
		add(s)
	}
	for _, f := range files {
		add(f)
	}
	return out
}
