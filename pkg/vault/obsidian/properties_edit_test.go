package obsidian

import (
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

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

// Editing one entry leaves every other frontmatter entry as authored:
// comments, key order and spelling, quoting, dates and line endings. The
// edited frontmatter is compared as YAML; untouched authored lines and the
// body are compared byte for byte.
func TestFrontmatterEditsKeepOtherLinesByteIdentical(t *testing.T) {
	authored := "---\n# owner note\ntitle: X\nTags: a, b\ndue: 2024-01-01 # soon\nstatus: open\nquote: 'kept'\n---\nbody\n"
	untouched := []string{"# owner note", "title: X", "due: 2024-01-01 # soon", "quote: 'kept'"}
	base := map[string]any{"title": "X", "Tags": "a, b", "due": "2024-01-01", "status": "open", "quote": "kept"}
	with := func(changes map[string]any) map[string]any {
		out := maps.Clone(base)
		for key, value := range changes {
			if value == nil {
				delete(out, key)
			} else {
				out[key] = value
			}
		}
		return out
	}
	cases := map[string]struct {
		content string
		edit    func(string) (string, bool, error)
		want    map[string]any
		kept    []string
	}{
		"set existing": {
			edit: func(c string) (string, bool, error) { return SetFrontmatterProperty(c, "status", "done", true) },
			want: with(map[string]any{"status": "done"}),
			kept: append([]string{"Tags: a, b"}, untouched...),
		},
		"set new": {
			edit: func(c string) (string, bool, error) { return SetFrontmatterProperty(c, "owner", "me", false) },
			want: with(map[string]any{"owner": "me"}),
			kept: append([]string{"Tags: a, b", "status: open"}, untouched...),
		},
		"delete": {
			edit: func(c string) (string, bool, error) { return DeleteFrontmatterProperties(c, []string{"STATUS"}) },
			want: with(map[string]any{"status": nil}),
			kept: append([]string{"Tags: a, b"}, untouched...),
		},
		"add tag": {
			content: strings.Replace(authored, "Tags: a, b", "tags: [a]", 1),
			edit: func(c string) (string, bool, error) {
				out, changed := AddTags(c, []string{"b"})
				return out, changed, nil
			},
			want: with(map[string]any{"Tags": nil, "tags": []any{"a", "b"}}),
			kept: append([]string{"status: open"}, untouched...),
		},
		"add tag beside capitalized Tags": {
			content: strings.Replace(authored, "Tags: a, b", "Tags: [a]", 1),
			edit: func(c string) (string, bool, error) {
				out, changed := AddTags(c, []string{"b"})
				return out, changed, nil
			},
			want: with(map[string]any{"Tags": []any{"a"}, "tags": []any{"b"}}),
			kept: append([]string{"Tags: [a]", "status: open"}, untouched...),
		},
		"indented root keys": {
			content: "---\n  title: X\n  status: old\n---\nbody\n",
			edit:    func(c string) (string, bool, error) { return SetFrontmatterProperty(c, "status", "new", true) },
			want:    map[string]any{"title": "X", "status": "new"},
		},
	}
	for name, tc := range cases {
		for _, eol := range []string{"\n", "\r\n"} {
			t.Run(name+"/"+strconv.Quote(eol), func(t *testing.T) {
				content := tc.content
				if content == "" {
					content = authored
				}
				got, changed, err := tc.edit(strings.ReplaceAll(content, "\n", eol))
				require.NoError(t, err)
				require.True(t, changed)
				parts := strings.SplitN(strings.ReplaceAll(got, "\r\n", "\n"), "---\n", 3)
				require.Len(t, parts, 3)
				var decoded map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &decoded))
				if due, ok := decoded["due"].(time.Time); ok {
					decoded["due"] = due.Format(time.DateOnly)
				}
				require.Equal(t, tc.want, decoded)
				require.True(t, strings.HasSuffix(got, "---"+eol+"body"+eol), "the body keeps its bytes: %q", got)
				for _, line := range tc.kept {
					require.Contains(t, got, line+eol)
				}
			})
		}
	}
}
