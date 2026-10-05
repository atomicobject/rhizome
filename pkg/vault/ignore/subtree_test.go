package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

// writeWrapperRepo builds the SPEC-0064 wrapper-repo fixture: the root
// gitignores app/ while app/ is its own repo with a .gitignore excluding
// build output.
func writeWrapperRepo(t *testing.T, rootGitignore string) string {
	t.Helper()
	root := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mustWrite(".gitignore", rootGitignore)
	mustWrite("app/.gitignore", "dist/\n")
	mustWrite("app/src/main.go", "package main\n")
	mustWrite("app/notes/readme.md", "# notes\n")
	mustWrite("app/dist/bundle.js", "js\n")
	mustWrite("app/node_modules/dep/index.js", "js\n")
	return root
}

func writeRhizomeIgnore(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .rhizome: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .rhizome/ignore: %v", err)
	}
}

func TestSubtreeInclusion_NegationReincludesGitignoredSubtree(t *testing.T) {
	cases := []struct {
		name          string
		rootGitignore string
	}{
		{"dir-pattern", "app/\n"},
		{"anchored-pattern", "/app\n"},
		{"anchored-dir-pattern", "/app/\n"},
		{"recursive-pattern", "app/**\n"},
		{"bare-name", "app\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeWrapperRepo(t, tc.rootGitignore)
			// node_modules/ mirrors the copied defaults that rzm init writes
			// into .rhizome/ignore; the negation is the labeled inclusion.
			writeRhizomeIgnore(t, root, "node_modules/\n\n# rhizome: included subtrees\n!/app/\n")

			m := LoadUnifiedMatcher(root, nil)
			if m.IsIgnored("app", true) {
				t.Fatalf("expected app/ to be re-included by !/app/")
			}
			if m.IsIgnored("app/src/main.go", false) {
				t.Fatalf("expected app/src/main.go to be indexed")
			}
			if m.IsIgnored("app/notes/readme.md", false) {
				t.Fatalf("expected app/notes/readme.md to be indexed")
			}
			if !m.IsIgnored("app/dist", true) {
				t.Fatalf("expected app/dist to stay excluded by app/.gitignore")
			}
			if !m.IsIgnored("app/dist/bundle.js", false) {
				t.Fatalf("expected app/dist/bundle.js to stay excluded by app/.gitignore")
			}
			if !m.IsIgnored("app/node_modules/dep/index.js", false) {
				t.Fatalf("expected node_modules to stay excluded inside the re-included subtree")
			}
			// Shallow matching (walk pruning) must agree with deep matching.
			if m.IsIgnoredShallow("app", true) {
				t.Fatalf("expected shallow match to descend into app/")
			}
			if !m.IsIgnoredShallow("app/dist", true) {
				t.Fatalf("expected shallow match to prune app/dist")
			}
		})
	}
}

func TestSubtreeInclusion_LaterRhizomeRuleReexcludesInsideSubtree(t *testing.T) {
	root := writeWrapperRepo(t, "app/\n")
	writeRhizomeIgnore(t, root, "!/app/\napp/secrets/\n")
	if err := os.MkdirAll(filepath.Join(root, "app", "secrets"), 0o755); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}

	m := LoadUnifiedMatcher(root, nil)
	if m.IsIgnored("app/src/main.go", false) {
		t.Fatalf("expected app/src/main.go to be indexed")
	}
	if !m.IsIgnored("app/secrets/key.md", false) {
		t.Fatalf("expected later rhizome rule to re-exclude app/secrets")
	}
}

func TestSubtreeInclusion_ConfigExcludesStillApplyInsideSubtree(t *testing.T) {
	root := writeWrapperRepo(t, "app/\n")
	writeRhizomeIgnore(t, root, "!/app/\n")

	m := LoadUnifiedMatcher(root, []string{"app/notes/"})
	if m.IsIgnored("app/src/main.go", false) {
		t.Fatalf("expected app/src/main.go to be indexed")
	}
	if !m.IsIgnored("app/notes/readme.md", false) {
		t.Fatalf("expected config excludes to re-exclude app/notes")
	}
}

func TestSubtreeInclusion_RemovingNegationRestoresExclusion(t *testing.T) {
	root := writeWrapperRepo(t, "app/\n")
	writeRhizomeIgnore(t, root, "!/app/\n")

	if LoadUnifiedMatcher(root, nil).IsIgnored("app/src/main.go", false) {
		t.Fatalf("expected app/src/main.go indexed while negation present")
	}
	writeRhizomeIgnore(t, root, "# nothing included\nnode_modules/\n")
	if !LoadUnifiedMatcher(root, nil).IsIgnored("app/src/main.go", false) {
		t.Fatalf("expected app/src/main.go excluded after negation removal")
	}
}

func TestSubtreeInclusion_NestedBoundary(t *testing.T) {
	root := writeWrapperRepo(t, "modules/\n")
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mustWrite("modules/app/src/main.go", "package main\n")
	mustWrite("modules/other/file.go", "package other\n")
	writeRhizomeIgnore(t, root, "!/modules/app/\n")

	m := LoadUnifiedMatcher(root, nil)
	if m.IsIgnored("modules/app/src/main.go", false) {
		t.Fatalf("expected nested boundary modules/app to be indexed")
	}
	if !m.IsIgnored("modules/other/file.go", false) {
		t.Fatalf("expected sibling modules/other to stay excluded")
	}
}

func TestExplain_AttributesDecidingLayer(t *testing.T) {
	root := writeWrapperRepo(t, "app/\n")
	writeRhizomeIgnore(t, root, "node_modules/\n!/app/\n")

	m := LoadUnifiedMatcher(root, nil)

	d := m.Explain("app/dist/bundle.js", false)
	if !d.Ignored {
		t.Fatalf("expected app/dist/bundle.js ignored, got %+v", d)
	}
	if d.Rule == nil || d.Rule.Layer != LayerGitignore || d.Rule.Source != "app/.gitignore" || d.Rule.Pattern != "dist/" {
		t.Fatalf("expected app/.gitignore dist/ attribution, got %+v", d.Rule)
	}

	d = m.Explain("app/src/main.go", false)
	if d.Ignored {
		t.Fatalf("expected app/src/main.go included, got %+v", d)
	}
	if d.Boundary == nil || d.Boundary.Pattern != "!/app/" || d.Boundary.Layer != LayerRhizome {
		t.Fatalf("expected boundary attribution for re-included path, got %+v", d.Boundary)
	}

	d = m.Explain("app/node_modules/dep/index.js", false)
	if !d.Ignored || d.Rule == nil || d.Rule.Layer != LayerRhizome || d.Rule.Pattern != "node_modules/" {
		t.Fatalf("expected rhizome node_modules/ attribution, got %+v", d.Rule)
	}

	d = m.Explain("zz/never-written.md", false)
	if d.Ignored || d.Rule != nil {
		t.Fatalf("expected nonexistent unmatched path to be included with no rule, got %+v", d)
	}
}

func TestExplain_GitignoreLineNumbersAndAncestors(t *testing.T) {
	root := t.TempDir()
	gitignore := "# build output\nout/\napp/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	m := LoadUnifiedMatcher(root, nil)
	d := m.Explain("app/deep/file.md", false)
	if !d.Ignored {
		t.Fatalf("expected ignored")
	}
	if d.Rule == nil || d.Rule.Source != ".gitignore" || d.Rule.Line != 3 || d.Rule.Pattern != "app/" {
		t.Fatalf("expected .gitignore line 3 app/ attribution, got %+v", d.Rule)
	}
	if d.IgnoredAncestor != "app" {
		t.Fatalf("ignored ancestor = %q, want app", d.IgnoredAncestor)
	}

	d = m.Explain("kept.md", false)
	if d.Ignored || d.Rule != nil || d.Boundary != nil {
		t.Fatalf("expected kept.md plainly included, got %+v", d)
	}
}

func TestExplain_DefaultAndConfigLayers(t *testing.T) {
	root := t.TempDir()
	m := LoadUnifiedMatcher(root, []string{"drafts/"})

	d := m.Explain("node_modules/x.js", false)
	if !d.Ignored || d.Rule == nil || d.Rule.Layer != LayerDefault || d.Rule.Pattern != "node_modules/" {
		t.Fatalf("expected default-layer attribution, got %+v", d.Rule)
	}

	d = m.Explain("drafts/wip.md", false)
	if !d.Ignored || d.Rule == nil || d.Rule.Layer != LayerConfig || d.Rule.Pattern != "drafts/" {
		t.Fatalf("expected config-layer attribution, got %+v", d.Rule)
	}
}
