package obsidian

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMdLinkTarget_CanonicalURLPaths(t *testing.T) {
	cache := BuildNotePathCache([]string{
		"Archive/Budget#USD.md", "Archive/Budget.md", "Archive/Budget USD.md",
		"Archive/Budget%20USD.md", "Archive/Budget%USD.md", "Archive/Budget+USD.md",
		"Reviewed/Final.md", "Report.md", "A/Duplicate.md", "B/Duplicate.md",
		"a:b/Final.md", "a:b/Final#part.md", "Other/Only.md", "Other/C:Only.md", "Notes/a:b/Local.md",
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
		{name: "authored path whitespace", target: " Reviewed/Final.md ", path: "Reviewed/Final.md", ok: true},
		{name: "root relative path", target: "/Reviewed/Final.md", from: "Other/Only.md", path: "Reviewed/Final.md", ok: true},
		{name: "decoded relative hash", target: "../Archive/Budget%23USD.md", from: "Reviewed/Final.md", path: "Archive/Budget#USD.md", ok: true},
		{name: "literal cached colon", target: "a:b/Final.md", path: "a:b/Final.md", ok: true},
		{name: "encoded cached colon", target: "a%3Ab/Final.md", path: "a:b/Final.md", ok: true},
		{name: "extensionless cached colon", target: "a%3Ab/Final", path: "a:b/Final.md", ok: true},
		{name: "cached colon and encoded hash", target: "a%3Ab/Final%23part.md#heading%28$1%29", path: "a:b/Final#part.md", fragment: "heading($1)", ok: true},
		{name: "parent escape cannot use cached basename", target: "../Other/Only.md"},
		{name: "encoded parent escape cannot use cached basename", target: "%2e%2e/Other/Only.md"},
		{name: "Windows absolute cannot use cached basename", target: "C:/Other/Only.md"},
		{name: "encoded Windows absolute cannot use cached basename", target: "C%3A/Other/Only.md"},
		{name: "missing colon path cannot use cached basename", target: "a%3Ab/Only.md"},
		{name: "missing extensionless colon path cannot use cached basename", target: "a%3Ab/Only"},
		{name: "drive relative cannot use nested cached basename", target: "C%3AOnly"},
		{name: "nested source encoded drive cannot use basename", target: "C%3A/Other/Only.md", from: "Notes/Source.md"},
		{name: "nested source absent colon cannot use basename", target: "a%3Ab/Only.md", from: "Notes/Source.md"},
		{name: "nested source encoded drive relative cannot use basename", target: "C%3AOnly", from: "Notes/Source.md"},
		{name: "nested exact cached colon", target: "a%3Ab/Local.md", from: "Notes/Source.md", path: "Notes/a:b/Local.md", ok: true},
		{name: "nested extensionless cached colon", target: "a%3Ab/Local", from: "Notes/Source.md", path: "Notes/a:b/Local.md", ok: true},
		{name: "confined extensionless parent path", target: "../Other/Only", from: "Notes/Source.md", path: "Other/Only.md", ok: true},
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
	require.Len(t, markdownsyntax.ScanCommentLinks(content), 7)
	noteLinks := markdownsyntax.ScanStructuredLinks(content)
	require.Len(t, noteLinks, 1)
	assert.Equal(t, "ordinary", noteLinks[0].Display)
	assert.Equal(t, []string{"Note.md"}, markdownsyntax.ExtractMdLinks(content, markdownsyntax.DefaultMdLinkOptions))
}
