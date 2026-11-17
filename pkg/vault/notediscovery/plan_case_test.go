package notediscovery

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestIncludesFollowTheConfiguredCasePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pathCase paths.CasePolicy
		includes []string
		path     string
		want     Owner
		provider noteformat.FormatID
	}{
		{name: "fold accepts a differently cased folder", pathCase: paths.CaseFold, includes: []string{"MOCs/**/*.md"}, path: "mocs/riders.md", want: Note, provider: "markdown"},
		{name: "exact keeps a differently cased folder unowned", pathCase: paths.CaseExact, includes: []string{"MOCs/**/*.md"}, path: "mocs/riders.md", want: Unowned},
		{name: "fold accepts an explicit HTML include spelled differently", pathCase: paths.CaseFold, includes: []string{"Docs/Report.html"}, path: "docs/report.html", want: Note, provider: "html"},
		{name: "exact keeps the literal HTML basename case sensitive", pathCase: paths.CaseExact, includes: []string{"Docs/Report.html"}, path: "docs/report.html", want: Unowned},
		{name: "fold never accepts different letters", pathCase: paths.CaseFold, includes: []string{"MOCs/**/*.md"}, path: "docs/riders.md", want: Unowned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, err := Compile(Config{Mode: CollectionVault, Includes: tt.includes, Registry: testRegistry(t), Case: tt.pathCase})
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			rel, err := paths.CleanRelPath(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := plan.Classify(rel, false)
			if err != nil {
				t.Fatalf("Classify(%q): %v", tt.path, err)
			}
			if got.Owner != tt.want || got.Provider != tt.provider {
				t.Fatalf("Classify(%q) = %v/%q, want %v/%q", tt.path, got.Owner, got.Provider, tt.want, tt.provider)
			}
			if got.Path != rel {
				t.Fatalf("decision must keep the authored spelling, got %q", got.Path)
			}
		})
	}
}
