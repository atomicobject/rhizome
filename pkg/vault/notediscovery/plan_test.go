package notediscovery

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestClassifyPreservesMarkdownDefaultsAndRequiresExplicitHTMLIncludes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mode          Mode
		includes      []string
		path          string
		codeCandidate bool
		want          Owner
		provider      noteformat.FormatID
	}{
		{name: "classic markdown", mode: ClassicVault, path: "notes/readme.MD", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "classic HTML remains code", mode: ClassicVault, path: "notes/readme.html", codeCandidate: true, want: Code},
		{name: "collection default markdown", mode: CollectionVault, path: "notes/readme.md", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "collection default markdown accepts uppercase extension", mode: CollectionVault, path: "notes/readme.MD", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "collection default markdown accepts mixed-case extension", mode: CollectionVault, path: "notes/readme.mD", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "collection default HTML remains code", mode: CollectionVault, path: "notes/readme.html", codeCandidate: true, want: Code},
		{name: "broad include keeps markdown", mode: CollectionVault, includes: []string{"docs/**"}, path: "docs/readme.md", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "configured lowercase markdown pattern accepts uppercase extension", mode: CollectionVault, includes: []string{"docs/*.md"}, path: "docs/readme.MD", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "configured lowercase markdown pattern accepts mixed-case extension", mode: CollectionVault, includes: []string{"docs/*.md"}, path: "docs/readme.mD", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "configured uppercase markdown pattern accepts lowercase extension", mode: CollectionVault, includes: []string{"docs/*.MD"}, path: "docs/readme.md", codeCandidate: true, want: Note, provider: "markdown"},
		{name: "broad include does not enable HTML", mode: CollectionVault, includes: []string{"docs/**"}, path: "docs/readme.html", codeCandidate: true, want: Code},
		{name: "exact HTML include", mode: CollectionVault, includes: []string{"docs/report.html"}, path: "docs/report.html", codeCandidate: true, want: Note, provider: "html"},
		{name: "literal basename case remains case sensitive", mode: CollectionVault, includes: []string{"docs/report.html"}, path: "docs/REPORT.HTML", codeCandidate: true, want: Code},
		{name: "wildcard HTML include preserves extension casing", mode: CollectionVault, includes: []string{"docs/*.html"}, path: "docs/report.HTML", codeCandidate: true, want: Note, provider: "html"},
		{name: "uppercase pattern matches lower extension", mode: CollectionVault, includes: []string{"docs/*.HTM"}, path: "docs/report.htm", codeCandidate: true, want: Note, provider: "html"},
		{name: "brace HTML include", mode: CollectionVault, includes: []string{"docs/*.{html,htm}"}, path: "docs/report.HTM", codeCandidate: true, want: Note, provider: "html"},
		{name: "mixed brace include does not enable HTML", mode: CollectionVault, includes: []string{"docs/*.{html,md}"}, path: "docs/report.html", codeCandidate: true, want: Code},
		{name: "character class does not enable HTML", mode: CollectionVault, includes: []string{"docs/*.[hH][tT][mM]"}, path: "docs/report.htm", codeCandidate: true, want: Code},
		{name: "suffix wildcard does not enable HTML", mode: CollectionVault, includes: []string{"docs/*.html*"}, path: "docs/report.html", codeCandidate: true, want: Code},
		{name: "unsupported HTML-like extension remains code", mode: CollectionVault, includes: []string{"docs/**"}, path: "docs/report.xhtml", codeCandidate: true, want: Code},
		{name: "unowned path remains unowned", mode: CollectionVault, includes: []string{"docs/**/*.md"}, path: "assets/report.txt", want: Unowned},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plan := testPlan(t, tt.mode, tt.includes, nil)
			rel, err := paths.CleanRelPath(tt.path)
			if err != nil {
				t.Fatalf("CleanRelPath(%q): %v", tt.path, err)
			}
			got, err := plan.Classify(rel, tt.codeCandidate)
			if err != nil {
				t.Fatalf("Classify(%q): %v", tt.path, err)
			}
			if got.Owner != tt.want {
				t.Fatalf("owner = %v, want %v", got.Owner, tt.want)
			}
			if got.Provider != tt.provider {
				t.Fatalf("provider = %q, want %q", got.Provider, tt.provider)
			}
		})
	}
}

func TestClassifyAppliesPrecomputedIgnoreBeforeProviderSelection(t *testing.T) {
	t.Parallel()

	plan := testPlan(t, CollectionVault, []string{"docs/**/*.html"}, func(rel paths.RelPath) bool {
		return rel.String() == "docs/private/secret.html"
	})
	rel, err := paths.CleanRelPath("docs/private/secret.html")
	if err != nil {
		t.Fatalf("CleanRelPath: %v", err)
	}
	got, err := plan.Classify(rel, true)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got.Owner != Ignored {
		t.Fatalf("owner = %v, want ignored", got.Owner)
	}
	if got.Provider != "" {
		t.Fatalf("provider = %q, want empty", got.Provider)
	}
}

func TestClassifyRejectsEscapingPaths(t *testing.T) {
	t.Parallel()

	plan := testPlan(t, CollectionVault, []string{"**/*.html"}, nil)
	if _, err := plan.Classify(paths.RelPath("../outside.html"), true); err == nil {
		t.Fatal("Classify accepted vault-escaping path")
	}
}

func TestCompileRejectsMalformedCollectionInclude(t *testing.T) {
	t.Parallel()

	registry := testRegistry(t)
	if _, err := Compile(Config{Mode: CollectionVault, Includes: []string{"docs/["}, Registry: registry}); err == nil {
		t.Fatal("Compile accepted malformed include")
	}
}

func TestCompileRejectsEmptyOrDefaultlessRegistry(t *testing.T) {
	t.Parallel()

	if _, err := Compile(Config{Mode: ClassicVault}); err == nil {
		t.Fatal("Compile accepted empty registry")
	}
	explicitOnly, err := noteformat.NewRegistry(testProvider{descriptor: noteformat.Descriptor{
		ID:                "html",
		Extensions:        []string{".html"},
		ProviderVersion:   "v1",
		ProjectionVersion: "v1",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
	}})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := Compile(Config{Mode: ClassicVault, Registry: explicitOnly}); err == nil {
		t.Fatal("Compile accepted registry without default ownership provider")
	}
}

func TestCompileDerivesCollectionDefaultsFromDefaultProvider(t *testing.T) {
	t.Parallel()

	registry, err := noteformat.NewRegistry(testProvider{descriptor: noteformat.Descriptor{
		ID:                "journal",
		Extensions:        []string{".JOURNAL"},
		ProviderVersion:   "v1",
		ProjectionVersion: "v1",
		OwnershipPolicy:   noteformat.OwnershipDefault,
	}})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	plan, err := Compile(Config{Mode: CollectionVault, Registry: registry})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	path, err := paths.CleanRelPath("notes/today.journal")
	if err != nil {
		t.Fatalf("CleanRelPath: %v", err)
	}
	got, err := plan.Classify(path, true)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got.Owner != Note || got.Provider != "journal" {
		t.Fatalf("default provider classification = %#v, want note journal", got)
	}
}

func TestCompileRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	if _, err := Compile(Config{Mode: Mode(99), Registry: testRegistry(t)}); err == nil {
		t.Fatal("Compile accepted unknown mode")
	}
}

func TestClassifyUsesProviderOwnershipPolicyRatherThanFormatID(t *testing.T) {
	t.Parallel()

	registry, err := noteformat.NewRegistry(
		testProvider{descriptor: noteformat.Descriptor{
			ID:                "MARKDOWN",
			Extensions:        []string{".md"},
			ProviderVersion:   "v1",
			ProjectionVersion: "v1",
			OwnershipPolicy:   noteformat.OwnershipDefault,
		}},
		testProvider{descriptor: noteformat.Descriptor{
			ID:                "future-format",
			Extensions:        []string{".future"},
			ProviderVersion:   "v1",
			ProjectionVersion: "v1",
			OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		}},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	plan, err := Compile(Config{Mode: CollectionVault, Includes: []string{"notes/*.future"}, Registry: registry})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	futurePath, err := paths.CleanRelPath("notes/example.FUTURE")
	if err != nil {
		t.Fatalf("CleanRelPath: %v", err)
	}
	got, err := plan.Classify(futurePath, true)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got.Owner != Note || got.Provider != "future-format" {
		t.Fatalf("future provider classification = %#v, want note future-format", got)
	}

	classicPlan, err := Compile(Config{Mode: ClassicVault, Registry: registry})
	if err != nil {
		t.Fatalf("Compile classic: %v", err)
	}
	markdownPath, err := paths.CleanRelPath("notes/example.MD")
	if err != nil {
		t.Fatalf("CleanRelPath: %v", err)
	}
	got, err = classicPlan.Classify(markdownPath, true)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got.Owner != Note || got.Provider != "markdown" {
		t.Fatalf("canonical default provider classification = %#v, want note markdown", got)
	}
}

func testPlan(t *testing.T, mode Mode, includes []string, ignore IgnoreFunc) *Plan {
	t.Helper()
	plan, err := Compile(Config{
		Mode:     mode,
		Includes: includes,
		Ignore:   ignore,
		Registry: testRegistry(t),
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return plan
}

func testRegistry(t *testing.T) noteformat.Registry {
	t.Helper()
	registry, err := noteformat.NewRegistry(
		testProvider{descriptor: noteformat.Descriptor{ID: "markdown", Extensions: []string{".md"}, ProviderVersion: "v1", ProjectionVersion: "v1", OwnershipPolicy: noteformat.OwnershipDefault}},
		testProvider{descriptor: noteformat.Descriptor{ID: "html", Extensions: []string{".html", ".htm"}, ProviderVersion: "v1", ProjectionVersion: "v1", OwnershipPolicy: noteformat.OwnershipExplicitInclude}},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

type testProvider struct {
	descriptor noteformat.Descriptor
}

func (p testProvider) Descriptor() noteformat.Descriptor {
	return p.descriptor
}
