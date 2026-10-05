package mcp

import "strings"

func normalizeSeedTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func normalizeQueryStrings(queries []string) []string {
	if len(queries) == 0 {
		return nil
	}
	out := make([]string, 0, len(queries))
	for _, q := range queries {
		q = strings.ReplaceAll(q, "\r\n", "\n")
		if strings.Contains(q, "\n\n") {
			parts := strings.Split(q, "\n\n")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				out = append(out, part)
			}
			continue
		}
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
