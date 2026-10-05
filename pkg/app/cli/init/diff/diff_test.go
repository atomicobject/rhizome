package diff

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateUnifiedDiff_LineBasedText(t *testing.T) {
	numbered := func(from, to int) string {
		var b strings.Builder
		for i := from; i <= to; i++ {
			fmt.Fprintf(&b, "line %d\n", i)
		}
		return b.String()
	}
	tests := []struct {
		name           string
		old, new       string
		want           string
		removed, added []string
		wantHunk       *Hunk
	}{
		{
			name:    "replacement keeps literal text and line numbers",
			old:     "one\n[]AtomicCon\nthree\n",
			new:     "one\n[[AtomicCon]] café\nthree\n",
			want:    "--- a/n.md\n+++ b/n.md\n@@ -1,3 +1,3 @@\n one\n-[]AtomicCon\n+[[AtomicCon]] café\n three\n",
			removed: []string{"[]AtomicCon"}, added: []string{"[[AtomicCon]] café"},
			wantHunk: &Hunk{OldStart: 2, NewStart: 2, ContextBefore: []string{"one"}, Removed: []string{"[]AtomicCon"}, Added: []string{"[[AtomicCon]] café"}, ContextAfter: []string{"three"}},
		},
		{
			name:  "creation from empty",
			old:   "",
			new:   "a\nb\n",
			want:  "--- a/n.md\n+++ b/n.md\n@@ -0,0 +1,2 @@\n+a\n+b\n",
			added: []string{"a", "b"},
		},
		{
			name: "interior insertion",
			old:  "line1\nline3\n", new: "line1\nline2\nline3\n",
			want:     "--- a/n.md\n+++ b/n.md\n@@ -1,2 +1,3 @@\n line1\n+line2\n line3\n",
			added:    []string{"line2"},
			wantHunk: &Hunk{OldStart: 2, NewStart: 2, ContextBefore: []string{"line1"}, Added: []string{"line2"}, ContextAfter: []string{"line3"}},
		},
		{
			name:    "deletion to empty",
			old:     "a\n",
			new:     "",
			want:    "--- a/n.md\n+++ b/n.md\n@@ -1 +0,0 @@\n-a\n",
			removed: []string{"a"},
		},
		{
			name: "interior deletion",
			old:  "line1\nline2\nline3\n", new: "line1\nline3\n",
			want:     "--- a/n.md\n+++ b/n.md\n@@ -1,3 +1,2 @@\n line1\n-line2\n line3\n",
			removed:  []string{"line2"},
			wantHunk: &Hunk{OldStart: 2, NewStart: 2, ContextBefore: []string{"line1"}, Removed: []string{"line2"}, ContextAfter: []string{"line3"}},
		},
		{
			name: "missing final newline is marked",
			old:  "a\nb",
			new:  "a\nc",
			want: "--- a/n.md\n+++ b/n.md\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n",
		},
		{
			name: "distant changes become separate hunks with three lines of context",
			old:  numbered(1, 20),
			new:  strings.Replace(strings.Replace(numbered(1, 20), "line 2\n", "line two\n", 1), "line 18\n", "", 1),
			want: "--- a/n.md\n+++ b/n.md\n" +
				"@@ -1,5 +1,5 @@\n line 1\n-line 2\n+line two\n line 3\n line 4\n line 5\n" +
				"@@ -15,6 +15,5 @@\n line 15\n line 16\n line 17\n-line 18\n line 19\n line 20\n",
		},
		{
			name: "nearby changes share one hunk",
			old:  numbered(1, 8),
			new:  strings.Replace(strings.Replace(numbered(1, 8), "line 1\n", "L1\n", 1), "line 7\n", "L7\n", 1),
			want: "--- a/n.md\n+++ b/n.md\n@@ -1,8 +1,8 @@\n-line 1\n+L1\n line 2\n line 3\n line 4\n line 5\n line 6\n-line 7\n+L7\n line 8\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, hunks, err := GenerateUnifiedDiff(tc.old, tc.new, "n.md")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			if tc.removed != nil || tc.added != nil {
				require.Len(t, hunks, 1)
				assert.Equal(t, tc.removed, hunks[0].Removed)
				assert.Equal(t, tc.added, hunks[0].Added)
			}
			if tc.wantHunk != nil {
				require.Len(t, hunks, 1)
				assert.Equal(t, *tc.wantHunk, hunks[0])
			}
		})
	}
}

func TestGenerateUnifiedDiff_NoChanges(t *testing.T) {
	content := "line1\nline2\nline3\n"
	diffText, hunks, err := GenerateUnifiedDiff(content, content, "test.txt")
	require.NoError(t, err)
	assert.Empty(t, diffText)
	assert.Empty(t, hunks)
}
