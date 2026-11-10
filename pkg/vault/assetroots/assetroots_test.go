package assetroots

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRootsListsFlatThenModules(t *testing.T) {
	repo := t.TempDir()
	mkdirs(t, repo,
		".rhizome/query-recipes",
		".rhizome/modules/zeta/recipes",
		".rhizome/modules/alpha/recipes",
		".rhizome/modules/beta/views",
		".rhizome/modules/Bad_Name/recipes",
	)
	got, err := Roots(repo, Recipes)
	if err != nil {
		t.Fatal(err)
	}
	want := []Root{
		{Rel: ".rhizome/query-recipes"},
		{Module: "alpha", Rel: ".rhizome/modules/alpha/recipes"},
		{Module: "zeta", Rel: ".rhizome/modules/zeta/recipes"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Roots = %#v, want %#v", got, want)
	}
	_, invalid, _ := Modules(repo)
	if !reflect.DeepEqual(invalid, []string{"Bad_Name"}) {
		t.Fatalf("invalid = %v", invalid)
	}
}

func TestRootsWithoutModulesFolder(t *testing.T) {
	repo := t.TempDir()
	got, err := Roots(repo, Ontology)
	if err != nil || len(got) != 0 {
		t.Fatalf("Roots = %v, %v", got, err)
	}
	if roots, _ := Roots(repo, Scripts); len(roots) != 0 {
		t.Fatalf("scripts have no flat folder, got %v", roots)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		rel  string
		root Root
		kind Kind
		sub  string
		ok   bool
	}{
		{".rhizome/ontology/core.graphql", Root{Rel: ".rhizome/ontology"}, Ontology, "core.graphql", true},
		{".rhizome/query-recipes/a.yaml", Root{Rel: ".rhizome/query-recipes"}, Recipes, "a.yaml", true},
		{".rhizome/views/poc/view.tsx", Root{Rel: ".rhizome/views"}, Views, "poc/view.tsx", true},
		{".rhizome/modules/compounding/tasks/mine.yml", Root{Module: "compounding", Rel: ".rhizome/modules/compounding/tasks"}, Tasks, "mine.yml", true},
		{".rhizome/modules/compounding/scripts/lib/x.mjs", Root{Module: "compounding", Rel: ".rhizome/modules/compounding/scripts"}, Scripts, "lib/x.mjs", true},
		{".rhizome/modules/compounding/models.json", Root{}, "", "", false},
		{".rhizome/modules/compounding/other/x", Root{}, "", "", false},
		{".rhizome/modules/Bad/views/x.yaml", Root{}, "", "", false},
		{".rhizome/views", Root{}, "", "", false},
		{".rhizome/config.yml", Root{}, "", "", false},
		{"docs/x.md", Root{}, "", "", false},
	}
	for _, c := range cases {
		root, kind, sub, ok := Classify(c.rel)
		if root != c.root || kind != c.kind || sub != c.sub || ok != c.ok {
			t.Errorf("Classify(%q) = %#v %q %q %v", c.rel, root, kind, sub, ok)
		}
	}
}

func TestAllRootsAndQualify(t *testing.T) {
	got := AllRoots(Tasks, []string{"compounding", "Nope"})
	want := []Root{{Rel: ".rhizome/tasks"}, {Module: "compounding", Rel: ".rhizome/modules/compounding/tasks"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllRoots = %#v", got)
	}
	if Qualify("", "mine") != "mine" || Qualify("compounding", "mine") != "compounding/mine" {
		t.Fatal("Qualify")
	}
}

func TestIsAssetRootOrAncestor(t *testing.T) {
	for _, tc := range []struct {
		rel  string
		kind Kind
		want bool
	}{
		{".rhizome/ontology", Ontology, true},
		{"./.rhizome/ontology/", Ontology, true},
		{".rhizome/modules", Ontology, true},
		{".rhizome/modules/team-1", Ontology, true},
		{".rhizome/modules/team-1/ontology", Ontology, true},
		{".rhizome/modules/Bad_Name/ontology", Ontology, false},
		{".rhizome/modules/Upper", Ontology, false},
		{".rhizome/modules/team/ontology/nested", Ontology, false},
		{".rhizome/ontology/schema.graphql", Ontology, false},
		{".rhizome/modules/team/scripts", Ontology, false},
		{".rhizome/query-recipes", Recipes, true},
		{".rhizome/modules/team/recipes", Recipes, true},
		{".rhizome/modules", Kind("unknown"), false},
		{".rhizome", Ontology, false},
		{"", Scripts, false},
	} {
		t.Run(tc.rel+"/"+string(tc.kind), func(t *testing.T) {
			if got := IsAssetRootOrAncestor(tc.rel, tc.kind); got != tc.want {
				t.Fatalf("IsAssetRootOrAncestor(%q, %q) = %v, want %v", tc.rel, tc.kind, got, tc.want)
			}
		})
	}
}

func TestRootsReportsAnAssetRootThatIsNotAFolder(t *testing.T) {
	repo := t.TempDir()
	mkdirs(t, repo, ".rhizome", ".rhizome/modules/alpha")
	if err := os.WriteFile(filepath.Join(repo, ".rhizome", "ontology"), []byte("oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Roots(repo, Ontology); err == nil {
		t.Fatal("a flat ontology path that is a file must be an error, not an absent root")
	}
	if err := os.WriteFile(filepath.Join(repo, ".rhizome", "modules", "alpha", "views"), []byte("oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkdirs(t, repo, ".rhizome/modules/beta/views")
	roots, err := Roots(repo, Views)
	if err == nil {
		t.Fatal("a module views path that is a file must be an error")
	}
	if len(roots) != 1 || roots[0].Module != "beta" {
		t.Fatalf("usable roots are still returned beside the error: %v", roots)
	}
	if roots, err := Roots(repo, Recipes); err != nil || len(roots) != 0 {
		t.Fatalf("missing roots stay absent: %v %v", roots, err)
	}
}

func TestRootsReportsABrokenLinkedAssetRoot(t *testing.T) {
	repo := t.TempDir()
	mkdirs(t, repo, ".rhizome/modules/demo")
	if err := os.Symlink(filepath.Join(repo, "missing"), filepath.Join(repo, ".rhizome", "modules", "demo", "tasks")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := Roots(repo, Tasks); err == nil {
		t.Fatal("a broken link at a module tasks folder must be reported, not treated as absent")
	}
}
