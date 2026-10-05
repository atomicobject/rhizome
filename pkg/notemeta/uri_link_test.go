package notemeta

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveURIProjectionLinkUsesURIComponentsAndDocumentBase(t *testing.T) {
	cache := obsidian.BuildNotePathCache([]string{
		"reports/prototype.html",
		"shared/detail.html",
		"root/target.md",
	})
	base := &noteformat.DocumentBaseFact{URI: noteformat.URIReferenceFact{Path: "../shared/", Encoding: noteformat.URIEncodingPercent}}
	link := noteformat.UnresolvedAuthoredLinkFact{
		Resolution: noteformat.LinkResolutionURI,
		URI:        &noteformat.URIReferenceFact{Path: "detail.html", Query: "view=wide", Fragment: "chart", Encoding: noteformat.URIEncodingPercent},
	}
	target, ok := ResolveURIProjectionLink(cache, "reports/prototype.html", base, link)
	require.True(t, ok)
	require.Equal(t, "shared/detail.html", target)

	link.URI.Path = "/root/target.md"
	target, ok = ResolveURIProjectionLink(cache, "reports/prototype.html", nil, link)
	require.True(t, ok)
	require.Equal(t, "root/target.md", target)
}

func TestResolveURIProjectionLinkUsesCommonCandidatesForSuffixesAndAliases(t *testing.T) {
	cache := obsidian.BuildNotePathCacheWithAliases(
		[]string{"notes/report.md", "notes/report.html", "notes/unique.html", "notes/other.html", "notes/spec.md"},
		map[string][]string{
			"notes/unique.html": {"UNIQUE", "SHARED"},
			"notes/other.html":  {"SHARED"},
			"notes/spec.md":     {"SPEC-0042.US1"},
		},
	)
	uriLink := func(path string) noteformat.UnresolvedAuthoredLinkFact {
		uri := noteformat.URIReferenceFact{Path: path, Encoding: noteformat.URIEncodingPercent}
		return noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &uri}
	}

	target, ok := ResolveURIProjectionLink(cache, "notes/source.html", nil, uriLink("report.html"))
	require.True(t, ok)
	require.Equal(t, "notes/report.html", target, "explicit suffix must address the exact authored path")
	_, ok = ResolveURIProjectionLink(cache, "notes/source.html", nil, uriLink("report"))
	require.False(t, ok, "extensionless cross-format candidates must remain ambiguous")
	target, ok = ResolveURIProjectionLink(cache, "notes/source.html", nil, uriLink("UNIQUE"))
	require.True(t, ok)
	require.Equal(t, "notes/unique.html", target)
	target, ok = ResolveURIProjectionLink(cache, "notes/source.html", nil, uriLink("SPEC-0042.US1"))
	require.True(t, ok)
	require.Equal(t, "notes/spec.md", target)
	_, ok = ResolveURIProjectionLink(cache, "notes/source.html", nil, uriLink("SHARED"))
	require.False(t, ok, "aliases participate in the same ambiguity set")
}

func TestResolveURIProjectionLinkTracksIncrementalExplicitPaths(t *testing.T) {
	cache := obsidian.BuildNotePathCache([]string{"notes/report.md"})
	link := uriLink("report.html")

	cache.AddOrUpdate("notes/report.html", nil)
	target, ok := ResolveURIProjectionLink(cache, "notes/source.html", nil, link)
	require.True(t, ok)
	require.Equal(t, "notes/report.html", target)

	cache.Remove("notes/report.html")
	_, ok = ResolveURIProjectionLink(cache, "notes/source.html", nil, link)
	require.False(t, ok)
}

func TestResolveURIProjectionLinkHandlesFragmentOnlyExternalBaseAndSingleDecode(t *testing.T) {
	cache := obsidian.BuildNotePathCache([]string{"notes/source.html", "notes/target.html", "notes/space name.html", "notes/%20.html"})
	fragment := noteformat.URIReferenceFact{Fragment: "only", Encoding: noteformat.URIEncodingPercent, FragmentRange: noteformat.OptionalSourceRange{Present: true}}
	target, ok := ResolveURIProjectionLink(cache, "notes/source.html", nil, noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &fragment})
	require.True(t, ok)
	require.Equal(t, "notes/source.html", target)

	internalBase := &noteformat.DocumentBaseFact{URI: noteformat.URIReferenceFact{Path: "target.html", Encoding: noteformat.URIEncodingPercent}}
	target, ok = ResolveURIProjectionLink(cache, "notes/source.html", internalBase, noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &fragment})
	require.True(t, ok)
	require.Equal(t, "notes/target.html", target, "a fragment-only reference resolves against an internal document base")

	base := &noteformat.DocumentBaseFact{URI: noteformat.URIReferenceFact{Scheme: "https", Authority: "example.test", Path: "/docs/", Encoding: noteformat.URIEncodingPercent}}
	_, ok = ResolveURIProjectionLink(cache, "notes/source.html", base, noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &noteformat.URIReferenceFact{Path: "source.html", Encoding: noteformat.URIEncodingPercent}})
	require.False(t, ok, "a relative href under an external base must stay external")

	percent := noteformat.URIReferenceFact{Path: "space name.html", Encoding: noteformat.URIEncodingPercent}
	target, ok = ResolveURIProjectionLink(cache, "notes/source.html", nil, noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &percent})
	require.True(t, ok)
	require.Equal(t, "notes/space name.html", target)
	doubleEncoded := noteformat.URIReferenceFact{Path: "%20.html", Encoding: noteformat.URIEncodingPercent}
	target, ok = ResolveURIProjectionLink(cache, "notes/source.html", nil, noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &doubleEncoded})
	require.True(t, ok)
	require.Equal(t, "notes/%20.html", target, "resolver must not decode provider components a second time")
}

func uriLink(target string) noteformat.UnresolvedAuthoredLinkFact {
	uri := noteformat.URIReferenceFact{Path: target, Encoding: noteformat.URIEncodingPercent}
	return noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &uri}
}

func TestResolveURIProjectionLinkRejectsExternalAndTraversalReferences(t *testing.T) {
	cache := obsidian.BuildNotePathCache([]string{"target.html"})
	for name, uri := range map[string]noteformat.URIReferenceFact{
		"scheme":    {Scheme: "https", Authority: "example.test", Path: "/target.html", Encoding: noteformat.URIEncodingPercent},
		"authority": {Authority: "example.test", ProtocolRelative: true, Path: "/target.html", Encoding: noteformat.URIEncodingPercent},
		"traversal": {Path: "../../target.html", Encoding: noteformat.URIEncodingPercent},
		"directory": {Path: "target.html/", Encoding: noteformat.URIEncodingPercent},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := ResolveURIProjectionLink(cache, "reports/prototype.html", nil, noteformat.UnresolvedAuthoredLinkFact{Resolution: noteformat.LinkResolutionURI, URI: &uri})
			require.False(t, ok)
		})
	}
}

func TestResolveProjectedURILinkExplicitPathUsesPlatformCasing(t *testing.T) {
	cache := obsidian.BuildNotePathCache([]string{"Specs/Feature.MD"})
	link := noteformat.UnresolvedAuthoredLinkFact{
		Resolution: noteformat.LinkResolutionURI,
		URI:        &noteformat.URIReferenceFact{Path: "../specs/feature.md", Encoding: noteformat.URIEncodingPercent},
	}
	target, ok := ResolveProjectedLink(cache, "efforts/workspace.html", nil, link)
	require.Equal(t, obsidian.IsCaseInsensitiveFS(), ok)
	if ok {
		require.Equal(t, "Specs/Feature.MD", target, "return the stored path with authored casing")
	} else {
		require.Empty(t, target)
	}
	require.Equal(t, "../specs/feature.md", link.URI.Path, "do not rewrite the authored URI")

	cache = obsidian.BuildNotePathCache([]string{"Specs/Feature.MD", "specs/feature.md"})
	target, ok = ResolveProjectedLink(cache, "efforts/workspace.html", nil, link)
	if obsidian.IsCaseInsensitiveFS() {
		require.False(t, ok, "equivalent stored paths must remain ambiguous")
	} else {
		require.True(t, ok)
		require.Equal(t, "specs/feature.md", target)
	}
}
