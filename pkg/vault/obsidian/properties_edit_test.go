package obsidian

import (
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"strconv"
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

// Editing one entry leaves every other frontmatter byte as authored:
// comments, key order and spelling, quoting, dates and line endings.
func TestFrontmatterEditsKeepOtherLinesByteIdentical(t *testing.T) {
	authored := "---\n# owner note\ntitle: X\nTags: a, b\ndue: 2024-01-01 # soon\nstatus: open\nquote: 'kept'\n---\nbody\n"
	cases := map[string]struct {
		edit func(string) (string, bool, error)
		want string
	}{
		"set existing": {
			edit: func(c string) (string, bool, error) { return SetFrontmatterProperty(c, "status", "done", true) },
			want: "---\n# owner note\ntitle: X\nTags: a, b\ndue: 2024-01-01 # soon\nstatus: done\nquote: 'kept'\n---\nbody\n",
		},
		"set new": {
			edit: func(c string) (string, bool, error) { return SetFrontmatterProperty(c, "owner", "me", false) },
			want: "---\n# owner note\ntitle: X\nTags: a, b\ndue: 2024-01-01 # soon\nstatus: open\nquote: 'kept'\nowner: me\n---\nbody\n",
		},
		"delete": {
			edit: func(c string) (string, bool, error) { return DeleteFrontmatterProperties(c, []string{"STATUS"}) },
			want: "---\n# owner note\ntitle: X\nTags: a, b\ndue: 2024-01-01 # soon\nquote: 'kept'\n---\nbody\n",
		},
		"add tag": {
			edit: func(c string) (string, bool, error) {
				out, changed := AddTags(strings.Replace(c, "Tags: a, b", "tags: [a]", 1), []string{"b"})
				return out, changed, nil
			},
			want: "---\n# owner note\ntitle: X\ntags:\n  - a\n  - b\ndue: 2024-01-01 # soon\nstatus: open\nquote: 'kept'\n---\nbody\n",
		},
	}
	for name, tc := range cases {
		for _, eol := range []string{"\n", "\r\n"} {
			t.Run(name+"/"+strconv.Quote(eol), func(t *testing.T) {
				got, changed, err := tc.edit(strings.ReplaceAll(authored, "\n", eol))
				require.NoError(t, err)
				require.True(t, changed)
				require.Equal(t, strings.ReplaceAll(tc.want, "\n", eol), got)
			})
		}
	}
}
