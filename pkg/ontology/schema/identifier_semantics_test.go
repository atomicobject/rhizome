package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentifierSemanticValueComparesSourceSpellings(t *testing.T) {
	for _, raw := range []string{
		" Mixed-ID ", "^Mixed-ID", "^ [[Mixed-ID]]", "[[^Mixed-ID|Display]]", "Mixed-ID|Display",
	} {
		require.Equal(t, "mixed-id", NormalizeIdentifierSemanticValue(raw), raw)
	}
	require.Equal(t, "[[mixed-id", NormalizeIdentifierSemanticValue("[[Mixed-ID"))
	require.Equal(t, "mixed-id]]", NormalizeIdentifierSemanticValue("Mixed-ID]]"))
}
