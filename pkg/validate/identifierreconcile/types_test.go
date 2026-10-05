package identifierreconcile

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestCanonicalNodeKeyNormalizesAndSerializesUnambiguously(t *testing.T) {
	t.Parallel()

	key, err := NewCanonicalNodeKey(` ./docs\spec.md `, " #^story-a ", " UserStory ", " id ")
	require.NoError(t, err)
	require.Equal(t, "docs/spec.md", key.NotePath)
	require.Equal(t, "^story-a", key.Fragment)
	require.Equal(t, "UserStory", key.TypeName)
	require.Equal(t, "id", key.IdentifierField)
	require.Equal(t, `["docs/spec.md","^story-a","UserStory","id"]`, key.String())

	other, err := NewCanonicalNodeKey("docs/spec.md", "^story-a|UserStory", "Embedded", "id")
	require.NoError(t, err)
	require.NotEqual(t, key.String(), other.String())
}

func TestCanonicalNodeKeyRejectsPathsOutsideVault(t *testing.T) {
	t.Parallel()

	_, err := NewCanonicalNodeKey("../outside.md", "", "Spec", "id")
	require.ErrorContains(t, err, "vault-relative")
}

func TestPoolKeyUsesStructuredIdentifierFormat(t *testing.T) {
	t.Parallel()

	pool, err := NewPoolKey(&ontology.IdentifierFormat{
		Strategy:  ontology.IdentifierStrategySequential,
		Prefix:    " SPEC ",
		Separator: "-",
		Pad:       4,
	})
	require.NoError(t, err)
	require.Equal(t, ontology.IdentifierStrategySequential, pool.Strategy)
	require.Equal(t, "SPEC", pool.Prefix)
	require.Equal(t, `["SEQUENTIAL","SPEC","-",4]`, pool.String())

	dotted, err := NewPoolKey(&ontology.IdentifierFormat{
		Strategy:  ontology.IdentifierStrategySequential,
		Prefix:    "SPEC",
		Separator: ".",
		Pad:       4,
	})
	require.NoError(t, err)
	require.NotEqual(t, pool.String(), dotted.String())
}
