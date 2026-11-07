package ontology

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestTypedBodyLinkTargetsRetainEligibleAmbiguityAndPathPrecedence(t *testing.T) {
	cache := obsidian.BuildNotePathCache([]string{"sources/one.md", "raw/one.md"})
	types := map[string]string{"sources/one.md": "Source"}
	resolveType := func(path string) string { return types[path] }
	path, ok := TypedBodyLinkTarget(nil, cache.ResolveNoteCandidates("one"), "Source", resolveType)
	require.True(t, ok)
	require.Equal(t, "sources/one.md", path)
	_, ok = TypedBodyLinkTarget(nil, cache.ResolveNoteCandidates("raw/one.md"), "Source", resolveType)
	require.False(t, ok, "an explicit incompatible path must not widen to the matching basename")
	types["raw/one.md"] = "Source"
	_, ok = TypedBodyLinkTarget(nil, cache.ResolveNoteCandidates("one"), "Source", resolveType)
	require.False(t, ok, "two eligible targets stay ambiguous")
}
