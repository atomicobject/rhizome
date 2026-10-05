package obsidian

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestResolveNoteCandidatesMissingExplicitNonMarkdownPath(t *testing.T) {
	tests := []struct {
		name    string
		link    string
		note    string
		aliases []string
	}{
		{name: "same path Markdown title", link: "effort/plan.html", note: "effort/plan.html.md"},
		{name: "basename Markdown fallback", link: "effort/plan.html", note: "plan.html.md"},
		{name: "frontmatter alias", link: "effort/plan.html", note: "other.md", aliases: []string{"effort/plan.html"}},
		{name: "bare HTML filename", link: "plan.html", note: "plan.html.md"},
		{name: "bare HTM alias", link: "plan.HTM", note: "other.md", aliases: []string{"plan.HTM"}},
		{name: "other suffix basename fallback", link: "effort/plan.txt", note: "plan.txt.md"},
		{name: "other suffix alias", link: "effort/plan.txt", note: "other.md", aliases: []string{"effort/plan.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := BuildNotePathCacheWithAliases([]string{tt.note}, map[string][]string{tt.note: tt.aliases})
			if candidates := cache.ResolveNoteCandidates(tt.link + "#scope"); len(candidates) != 0 {
				t.Fatalf("missing explicit non-Markdown path resolved to %#v", candidates)
			}
		})
	}
}

func TestResolveNoteTargetPreservesMarkdownFallback(t *testing.T) {
	cache := BuildNotePathCacheWithAliases([]string{"plan.md"}, map[string][]string{"plan.md": {"proposal"}})
	for _, link := range []string{"effort/plan.md#scope", "effort/plan#scope", "proposal.md#scope"} {
		t.Run(link, func(t *testing.T) {
			target, ok := cache.ResolveNoteTarget(link)
			if !ok || target.Path != "plan.md" || target.Fragment != "scope" {
				t.Fatalf("Markdown fallback: %#v, %v", target, ok)
			}
		})
	}
}

func TestResolveNoteTargetPreservesExplicitNonMarkdownPath(t *testing.T) {
	cache := BuildNotePathCache([]string{"effort/plan.html", "effort/plan.md"})
	target, ok := cache.ResolveNoteTarget("effort/plan.html#scope")
	if !ok || target.Path != "effort/plan.html" || target.Fragment != "scope" {
		t.Fatalf("explicit HTML path: %#v, %v", target, ok)
	}
	if _, ok := cache.ResolveNoteTarget("effort/plan"); ok {
		t.Fatal("extensionless mixed-format target must remain ambiguous")
	}
	if _, ok := cache.ResolveNoteTarget("missing/plan.html"); ok {
		t.Fatal("missing explicit path must not resolve to nearby collateral")
	}
}

func TestResolveNoteTargetPreservesDottedMarkdownPath(t *testing.T) {
	cache := BuildNotePathCacheWithAliases([]string{"docs/plan.v1.md", "other.md"}, map[string][]string{"other.md": {"docs/plan.v1"}})
	for _, link := range []string{"docs/plan.v1#scope", "docs/plan.v1.md#scope"} {
		t.Run(link, func(t *testing.T) {
			target, ok := cache.ResolveNoteTarget(link)
			if !ok || target.Path != "docs/plan.v1.md" || target.Fragment != "scope" {
				t.Fatalf("dotted Markdown path: %#v, %v", target, ok)
			}
		})
	}
}

func TestExplicitNotePathUsesPlatformCaseIdentity(t *testing.T) {
	const authored = "reports/plan.html"
	const link = "Reports/Plan.HTML"
	cache := BuildNotePathCache([]string{authored, "reports/plan.html.md"})
	check := func(want bool) {
		t.Helper()
		target, ok := cache.ResolveNoteTarget(link + "#Scope")
		if ok != want || (ok && (target.Path != authored || target.Fragment != "Scope")) {
			t.Fatalf("case-aware target = %#v, %v; want resolved %v with authored path", target, ok, want)
		}
	}
	check(paths.CaseEqual(authored, link))
	cache.Remove(authored)
	check(false)
	cache.AddOrUpdate(authored, nil)
	check(paths.CaseEqual(authored, link))
}

func TestResolveNoteTargetPreservesExplicitMarkdownWithHTMLCollision(t *testing.T) {
	for _, markdown := range []string{"effort/plan.md", "effort/plan.MD"} {
		t.Run(markdown, func(t *testing.T) {
			cache := BuildNotePathCache([]string{markdown, "effort/plan.html"})
			for _, fragment := range []string{"", "scope", "^block"} {
				link := markdown
				if fragment != "" {
					link += "#" + fragment
				}
				target, ok := cache.ResolveNoteTarget(link)
				if !ok || target.Path != markdown || target.Fragment != fragment {
					t.Fatalf("explicit Markdown target = %#v, %v", target, ok)
				}
			}
			if _, ok := cache.ResolveNoteTarget("effort/plan#scope"); ok {
				t.Fatal("extensionless mixed-format target must remain ambiguous")
			}
		})
	}
}

func TestMissingExplicitMarkdownDoesNotResolveToHTML(t *testing.T) {
	cache := BuildNotePathCache([]string{"effort/plan.html"})
	if target, ok := cache.ResolveNoteTarget("effort/plan.md#scope"); ok {
		t.Fatalf("missing explicit Markdown resolved to %#v", target)
	}
}
