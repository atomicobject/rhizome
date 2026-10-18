package schema

import (
	"errors"
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"
)

// CompileError identifies an ontology rule independently of its diagnostic prose.
type CompileError struct {
	Code      string
	TypeName  string
	FieldName string
	Position  *ast.Position
	Message   string
}

func (e *CompileError) Error() string {
	if e.Position == nil || e.Position.Src == nil {
		return "ontology compile error: " + e.Message
	}
	return fmt.Sprintf("%s:%d:%d: %s", e.Position.Src.Name, e.Position.Line, e.Position.Column, e.Message)
}

func compileError(pos *ast.Position, code, typeName, fieldName, format string, args ...any) *CompileError {
	return &CompileError{Code: code, TypeName: typeName, FieldName: fieldName, Position: pos, Message: fmt.Sprintf(format, args...)}
}

func withCompileContext(err error, pos *ast.Position, typeName, fieldName string) error {
	var rule *CompileError
	if !errors.As(err, &rule) {
		return compileError(pos, "invalid_schema", typeName, fieldName, "%v", err)
	}
	contextual := *rule
	contextual.Position = pos
	contextual.TypeName = typeName
	if fieldName != "" {
		contextual.FieldName = fieldName
	}
	return &contextual
}
