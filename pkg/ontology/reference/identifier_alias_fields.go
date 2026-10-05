package reference

import (
	"fmt"
	"slices"
	"strings"
)

// NormalizeAliasFields detaches and canonicalizes the additional source-field
// authority without changing the established mirror destination.
func (r *IdentifierRewrite) NormalizeAliasFields() error {
	r.AliasesField = strings.TrimSpace(r.AliasesField)
	fields := make([]string, 0, len(r.AdditionalAliasesFields))
	for _, raw := range r.AdditionalAliasesFields {
		field := strings.TrimSpace(raw)
		if field == "" {
			return fmt.Errorf("additional aliases field is empty")
		}
		if field != r.AliasesField {
			fields = append(fields, field)
		}
	}
	slices.Sort(fields)
	r.AdditionalAliasesFields = slices.Compact(fields)
	if len(r.AdditionalAliasesFields) == 0 {
		r.AdditionalAliasesFields = nil
	}
	return nil
}

// HasAliasField identifies an authorized authored alias collection.
func (r IdentifierRewrite) HasAliasField(field string) bool {
	return field == r.AliasesField || slices.Contains(r.AdditionalAliasesFields, field)
}

// AliasFieldNames includes the mirror destination and every additional source.
func (r IdentifierRewrite) AliasFieldNames() []string {
	return append([]string{r.AliasesField}, r.AdditionalAliasesFields...)
}
