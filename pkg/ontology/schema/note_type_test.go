package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreferredIdentifierFieldUsesFirstDeclaredPreferredField(t *testing.T) {
	var absent *NoteType
	require.Nil(t, absent.PreferredIdentifierField())
	require.Nil(t, (&NoteType{Fields: []*Field{nil, {Name: "id", IsIdentifier: true}}}).PreferredIdentifierField())
	first := &Field{Name: "first", IsPreferredIdentifier: true}
	second := &Field{Name: "second", IsPreferredIdentifier: true}
	require.Same(t, first, (&NoteType{Fields: []*Field{nil, {Name: "other"}, first, second}}).PreferredIdentifierField())
}
