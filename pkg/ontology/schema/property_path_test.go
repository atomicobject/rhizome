package schema

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPropertyPathCompile(t *testing.T) {
	s := mustLoadSDL(t, `type Assertion @node(paths: ["assertions/*.md"]) { speaker: String @field(path: "provenance.speaker") literal: String @field(source: "provenance.speaker") }`)
	require.Equal(t, "provenance.speaker", s.Types["Assertion"].ByName["speaker"].SourcePath)
	require.Empty(t, s.Types["Assertion"].ByName["literal"].SourcePath)
	for _, args := range []string{`path: ""`, `path: "a..b"`, `path: "a.*"`, `path: "a[0]"`, `path: "a.b", source: "x"`, `path: "a.b", sources: ["x"]`, `path: "a.b", sourceKind: INLINE`} {
		_, err := loadSDL(t, `type Assertion @node(paths: ["assertions/*.md"]) { speaker: String @field(`+args+`) }`)
		require.Error(t, err, args)
	}
}

func TestFrontmatterPathMissing(t *testing.T) {
	for _, value := range []any{nil, "text", []any{"x"}, map[string]any{}, map[string]any{"speaker": nil}} {
		got, present := FrontmatterPathValue(map[string]any{"provenance": value}, "provenance.speaker")
		require.Nil(t, got)
		require.False(t, present)
	}
	got, present := FrontmatterPathValue(map[string]any{"Provenance": map[string]any{"Speaker": "A", "SPEAKER": "B"}}, "provenance.speaker")
	require.True(t, present)
	require.Equal(t, "B", got)
}
