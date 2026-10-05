package obsidian

import (
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetFrontmatterPropertyCreatesBlock(t *testing.T) {
	content := "# Title\nbody"
	updated, changed, err := SetFrontmatterProperty(content, "status", "done", false)
	assert.NoError(t, err)
	assert.True(t, changed)
	assertFrontmatterEdit(t, updated, map[string]any{"status": "done"}, "# Title\nbody")
}

func TestSetFrontmatterPropertyRespectsOverwrite(t *testing.T) {
	content := "---\nstatus: open\n---\nbody"
	updated, changed, err := SetFrontmatterProperty(content, "status", "done", false)
	assert.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, content, updated)

	updated, changed, err = SetFrontmatterProperty(content, "status", "done", true)
	assert.NoError(t, err)
	assert.True(t, changed)
	assertFrontmatterEdit(t, updated, map[string]any{"status": "done"}, "body")
}

func TestDeleteFrontmatterProperties(t *testing.T) {
	content := "---\nstatus: open\nowner: me\n---\nbody"
	updated, changed, err := DeleteFrontmatterProperties(content, []string{"status"})
	assert.NoError(t, err)
	assert.True(t, changed)
	assertFrontmatterEdit(t, updated, map[string]any{"owner": "me"}, "body")
}

func TestRenameFrontmatterPropertiesMerge(t *testing.T) {
	content := "---\nstatus: open\nStatus: pending\nlabels:\n  - a\n---\nbody"
	updated, changed, err := RenameFrontmatterProperties(content, []string{"status"}, "labels", true)
	assert.NoError(t, err)
	assert.True(t, changed)
	assertFrontmatterEdit(t, updated, map[string]any{"labels": []any{"a", "open", "pending"}}, "body")
}

func TestRenameFrontmatterPropertiesNoMergeKeepsDestination(t *testing.T) {
	content := "---\nstatus: open\nlabels: a\n---\nbody"
	updated, changed, err := RenameFrontmatterProperties(content, []string{"status"}, "labels", false)
	assert.NoError(t, err)
	assert.True(t, changed)
	assertFrontmatterEdit(t, updated, map[string]any{"labels": "a"}, "body")
}

func assertFrontmatterEdit(t *testing.T, content string, want map[string]any, body string) {
	t.Helper()
	require.True(t, strings.HasPrefix(content, "---\n"))
	parts := strings.SplitN(content, "---\n", 3)
	require.Len(t, parts, 3)
	var got map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &got))
	if wantLabels, ok := want["labels"].([]any); ok {
		gotLabels, ok := got["labels"].([]any)
		require.True(t, ok)
		assert.ElementsMatch(t, wantLabels, gotLabels)
		delete(want, "labels")
		delete(got, "labels")
	}
	assert.Equal(t, want, got)
	assert.Equal(t, body, parts[2])
}
