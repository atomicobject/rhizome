package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFrontmatterRepairsPreserveOtherSource(t *testing.T) {
	const source = "---\n# owner comment\ntitle: Zeta\ndate: 2024-01-01\naliases: [a]\nstatus: draft # inline\n---\n\nBody\n"
	updated, err := ApplySetFrontmatterInMemory(source, "type", "Meeting", nil)
	require.NoError(t, err)
	require.Equal(t, "---\n# owner comment\ntitle: Zeta\ndate: 2024-01-01\naliases: [a]\nstatus: draft # inline\ntype: Meeting\n---\n\nBody\n", updated)
	updated, err = ApplyAliasAppendInMemory(source, "b")
	require.NoError(t, err)
	require.Equal(t, "---\n# owner comment\ntitle: Zeta\ndate: 2024-01-01\naliases: [a, b]\nstatus: draft # inline\n---\n\nBody\n", updated)
}
