package schemadoc

import (
	"fmt"
	"strings"
)

func renderRecordGuide(b *strings.Builder, typ *guideType) {
	source := typ.Record
	fmt.Fprintf(b, "\n### Resolution\n\nNative RECORD values selected from `%s` by `%s`.\n", strings.Join(typ.SourcePaths, "`, `"), source.Select.Text)
	key := source.Key.Field
	if source.Key.MappingKey {
		key = "mapping key"
	}
	if len(source.Key.Template) > 0 {
		key = "compiled template"
	}
	if source.Key.Positional {
		key = "position within the captured host revision"
	}
	fmt.Fprintf(b, "\nIdentity: %s. Logical space: `%s`; kind: `%s`. Title fallback: `%s`.\n", key, source.Space, source.LogicalKind, strings.Join(source.Title, "`, `"))
	b.WriteString("\nRead native values through the workspace source payload. This source binding supplies no editing operation.\n")
	b.WriteString("\n### Fields\n")
	for _, field := range typ.Fields {
		fmt.Fprintf(b, "\n- `%s`: `%s`", field.Name, field.TypeName)
		if field.Record != nil {
			fmt.Fprintf(b, "; @field(path: %q)", field.Record.Path.Text)
		}
		if field.RecordContains != nil {
			fmt.Fprintf(b, "; @contains(shape: RECORD, select: %q)", field.RecordContains.Select.Text)
		}
		for _, note := range fieldToolingNotes(field) {
			fmt.Fprintf(b, "; %s", note)
		}
	}
	b.WriteString("\n\n### Tooling Semantics\n")
	for _, note := range toolingNotes(typ) {
		fmt.Fprintf(b, "\n- %s", note)
	}
	b.WriteString("\n\nKeyed locators are durable. Positional locators require the captured source hash and become stale after any host edit.\n")
}
