package schema

import (
	"strings"
	"unicode"
)

// FieldDisplay carries field-level @display presentation hints. The zero value
// means "show normally": importance NORMAL, visible in hover previews, no role.
type FieldDisplayRole string

const (
	FieldDisplayRoleNone    FieldDisplayRole = ""
	FieldDisplayRoleSummary FieldDisplayRole = "SUMMARY"
	FieldDisplayRoleParent  FieldDisplayRole = "PARENT"
	FieldDisplayRoleRank    FieldDisplayRole = "RANK"
)

type FieldDisplayImportance string

const (
	FieldImportanceKey    FieldDisplayImportance = "KEY"
	FieldImportanceNormal FieldDisplayImportance = "NORMAL"
	FieldImportanceDetail FieldDisplayImportance = "DETAIL"
)

type FieldDisplay struct {
	Role       FieldDisplayRole       `json:"role,omitempty"`
	Importance FieldDisplayImportance `json:"importance,omitempty"`
	HideHover  bool                   `json:"hideHover,omitempty"`
}

func (d FieldDisplay) EffectiveImportance() FieldDisplayImportance {
	if d.Importance == "" {
		return FieldImportanceNormal
	}
	return d.Importance
}

// SummaryField returns the field carrying the SUMMARY display role. A field
// named "summary" holds the role by convention unless another field claims it.
func SummaryField(fields []*Field) *Field {
	var conventional *Field
	for _, field := range fields {
		if field == nil {
			continue
		}
		if field.Display.Role == FieldDisplayRoleSummary {
			return field
		}
		if field.Name == "summary" {
			conventional = field
		}
	}
	return conventional
}

// ParentField returns the field carrying the PARENT display role, which links
// a record to its parent record of the same type tree. Compile guarantees at
// most one.
func ParentField(fields []*Field) *Field {
	for _, field := range fields {
		if field != nil && field.Display.Role == FieldDisplayRoleParent {
			return field
		}
	}
	return nil
}

// RankField returns the field carrying the RANK display role, which orders a
// type's records ascending wherever they are listed without a view's own sort.
// Compile guarantees at most one.
func RankField(fields []*Field) *Field {
	for _, field := range fields {
		if field != nil && field.Display.Role == FieldDisplayRoleRank {
			return field
		}
	}
	return nil
}

// HumanizeFieldName turns a camelCase, kebab-case, or snake_case name into a
// sentence-case label.
func HumanizeFieldName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.IndexFunc(name, unicode.IsSpace) >= 0 {
		return name
	}
	runes := []rune(name)
	words := make([]string, 0, 4)
	start := 0
	flush := func(end int) {
		if end > start {
			words = append(words, string(runes[start:end]))
		}
	}
	for i, r := range runes {
		if r == '-' || r == '_' {
			flush(i)
			start = i + 1
			continue
		}
		if i == start {
			continue
		}
		prev := runes[i-1]
		nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		boundary := unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextLower)
		boundary = boundary || unicode.IsDigit(r) && !unicode.IsDigit(prev) || !unicode.IsDigit(r) && unicode.IsDigit(prev)
		if boundary {
			flush(i)
			start = i
		}
	}
	flush(len(runes))

	acronyms := map[string]string{
		"api": "API", "css": "CSS", "html": "HTML", "http": "HTTP",
		"https": "HTTPS", "id": "ID", "json": "JSON", "ui": "UI",
		"url": "URL", "ux": "UX",
	}
	for i, word := range words {
		lower := strings.ToLower(word)
		if acronym, ok := acronyms[lower]; ok {
			words[i] = acronym
			continue
		}
		// An authored all-caps run such as the IP in IPAsset stays an acronym.
		if len(word) > 1 && word == strings.ToUpper(word) {
			continue
		}
		words[i] = lower
		if i == 0 {
			r := []rune(words[i])
			if len(r) > 0 {
				r[0] = unicode.ToUpper(r[0])
				words[i] = string(r)
			}
		}
	}
	return strings.Join(words, " ")
}
