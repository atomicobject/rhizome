package obsidian

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMdLinkTarget_CanonicalURLPaths(t *testing.T) {
	cache := BuildNotePathCache([]string{
		"Archive/Budget#USD.md", "Archive/Budget.md", "Archive/Budget USD.md",
		"Archive/Budget%20USD.md", "Archive/Budget%USD.md", "Archive/Budget+USD.md",
		"Reviewed/Final.md", "Report.md", "A/Duplicate.md", "B/Duplicate.md",
	})
	for _, tc := range []struct {
		name, target, from, path, fragment string
		ok                                 bool
	}{
		{name: "encoded hash before fragment", target: "Archive/Budget%23USD.md#heading%28$1%29", path: "Archive/Budget#USD.md", fragment: "heading($1)", ok: true},
		{name: "URL percent20 means space", target: "Archive/Budget%20USD.md", path: "Archive/Budget USD.md", ok: true},
		{name: "literal percent20 decodes once", target: "Archive/Budget%2520USD.md", path: "Archive/Budget%20USD.md", ok: true},
		{name: "malformed percent remains literal", target: "Archive/Budget%USD.md", path: "Archive/Budget%USD.md", ok: true},
		{name: "plus is literal", target: "Archive/Budget+USD.md", path: "Archive/Budget+USD.md", ok: true},
		{name: "extensionless returns cache identity", target: "Reviewed/Final", path: "Reviewed/Final.md", ok: true},
		{name: "decoded relative hash", target: "../Archive/Budget%23USD.md", from: "Reviewed/Final.md", path: "Archive/Budget#USD.md", ok: true},
		{name: "missing explicit HTML", target: "Report.html"},
		{name: "ambiguous basename", target: "Duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cache.ResolveMdLinkTarget(tc.target, tc.from)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, ResolvedNoteTarget{Path: tc.path, Fragment: tc.fragment}, got)
		})
	}
}

func TestScanCommentLinks_CodeExamplesDoNotChangeNoteProtection(t *testing.T) {
	content := "`[inline](Note.md) [[Wiki]]`\n\n```go\n[fenced](Note.md) [[Wiki]]\n```\n\n    [indented](Note.md) [[Wiki]]\n\n[ordinary](Note.md)\n"
	require.Len(t, ScanCommentLinks(content), 7)
	noteLinks := ScanStructuredLinks(content)
	require.Len(t, noteLinks, 1)
	assert.Equal(t, "ordinary", noteLinks[0].Display)
	assert.Equal(t, []string{"Note.md"}, ExtractMdLinks(content, DefaultMdLinkOptions))
}
