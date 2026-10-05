package query

import (
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// Render signatures from the executable AST, omitting expanded documentation.
// A fragment deliberately does not redefine unselected output object types.
func writeDiscoveryDefinition(out *strings.Builder, def *ast.Definition) {
	kind := map[ast.DefinitionKind]string{
		ast.Object: "type", ast.Interface: "interface", ast.InputObject: "input",
		ast.Enum: "enum", ast.Union: "union", ast.Scalar: "scalar",
	}[def.Kind]
	out.WriteString(kind + " " + def.Name)
	if len(def.Interfaces) > 0 {
		out.WriteString(" implements " + strings.Join(def.Interfaces, " & "))
	}
	if def.Kind == ast.Scalar {
		out.WriteString("\n\n")
		return
	}
	if def.Kind == ast.Union {
		out.WriteString(" = " + strings.Join(def.Types, " | ") + "\n\n")
		return
	}
	out.WriteString(" {\n")
	for _, value := range def.EnumValues {
		out.WriteString("  " + value.Name + "\n")
	}
	for _, field := range def.Fields {
		out.WriteString("  " + field.Name)
		if len(field.Arguments) > 0 {
			out.WriteByte('(')
			for i, arg := range field.Arguments {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(arg.Name + ": " + arg.Type.String())
				if arg.DefaultValue != nil {
					out.WriteString(" = " + arg.DefaultValue.String())
				}
			}
			out.WriteByte(')')
		}
		out.WriteString(": " + field.Type.String())
		if field.DefaultValue != nil {
			out.WriteString(" = " + field.DefaultValue.String())
		}
		out.WriteByte('\n')
	}
	out.WriteString("}\n\n")
}
