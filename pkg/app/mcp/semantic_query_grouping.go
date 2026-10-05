package mcp

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
)

func packGroups(groups []semanticQueryGroup, _ semanticQueryIntent, _ docMatcher) []semanticGroup {
	// Group only adjacent results. Joining distant rows with the same display
	// label would move lower-ranked sources ahead of intervening results.
	out := make([]semanticGroup, 0, len(groups))
	for i, g := range groups {
		key, label := groupKeyAndLabel(g.match)
		if len(out) == 0 || out[len(out)-1].key != key {
			out = append(out, semanticGroup{
				key:   key,
				label: label,
				score: g.score,
				path:  g.match.Path,
			})
		}
		group := &out[len(out)-1]
		group.indices = append(group.indices, i)
	}
	return out
}

func assignSemanticRoles(groups []semanticQueryGroup, grouped []semanticGroup, intent semanticQueryIntent, docs docMatcher) {
	for _, group := range grouped {
		if len(group.indices) == 0 {
			continue
		}
		maxScore := groups[group.indices[0]].score
		for _, idx := range group.indices {
			match := groups[idx].match
			match.Role = defaultMatchRole(match, intent, docs)
			groups[idx].match = match
		}
		for _, idx := range group.indices {
			match := groups[idx].match
			if match.Type != "code" || docs.IsDoc(match.Path, match.Type) || isTestPath(match.Path) {
				continue
			}
			if !search.IsEntryPointFile(match.Path) {
				continue
			}
			if groups[idx].score < maxScore*0.9 {
				continue
			}
			match.Role = "entry_point"
			groups[idx].match = match
			break
		}
	}
}

func defaultMatchRole(match SemanticMatchPayload, intent semanticQueryIntent, docs docMatcher) string {
	if isTestPath(match.Path) {
		return "test"
	}
	if strings.EqualFold(match.Granularity, "ontology_context") {
		if strings.Contains(strings.ToLower(match.Path), "/decisions/") || strings.Contains(strings.ToLower(match.Path), "decision") {
			return "decision"
		}
		return "doc"
	}
	if docs.IsDoc(match.Path, match.Type) {
		return "doc"
	}
	if match.Type == "code" {
		if intent.Tests {
			return "support"
		}
		return "impl"
	}
	return "support"
}

func groupKeyAndLabel(match SemanticMatchPayload) (string, string) {
	label := ""
	typ := match.Type
	path := strings.TrimSpace(match.Path)
	if typ == "note" {
		label = noteGroupLabel(path)
		if label == "" {
			label = "notes"
		}
		return "note:" + label, "Notes: " + label
	}
	if typ == "code" {
		label = codeGroupLabel(path)
		if label == "" {
			label = "code"
		}
		return "code:" + label, "Code: " + label
	}
	if path == "" {
		return typ + ":misc", strings.TrimSpace(firstNonEmpty(typ, "Other"))
	}
	label = filepath.Dir(path)
	if label == "." || label == "/" {
		label = path
	}
	return typ + ":" + label, strings.TrimSpace(firstNonEmpty(typ, "Other")) + ": " + label
}

func noteGroupLabel(path string) string {
	p := strings.Trim(path, "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 2 && parts[0] == "docs" && parts[1] == "notes" {
		if len(parts) >= 3 {
			return filepath.ToSlash(filepath.Join(parts[0], parts[1], parts[2]))
		}
		return filepath.ToSlash(filepath.Join(parts[0], parts[1]))
	}
	if len(parts) >= 2 {
		return filepath.ToSlash(filepath.Join(parts[0], parts[1]))
	}
	return parts[0]
}

func codeGroupLabel(path string) string {
	p := strings.Trim(path, "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return ""
	}
	root := parts[0]
	if root == "pkg" || root == "cmd" || root == "scripts" || root == "testdata" || root == "tests" {
		if len(parts) >= 2 {
			return filepath.ToSlash(filepath.Join(root, parts[1]))
		}
		return root
	}
	return root
}

func renderGroupHeader(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return "Group: " + label
}
