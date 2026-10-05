package ignore

import "testing"

func TestMatcher_DirNegationUnignoresContents(t *testing.T) {
	m := NewMatcher([]string{
		"ignored/",
		"!ignored/",
	})

	if m.IsIgnored("ignored", true) {
		t.Fatalf("expected ignored/ dir to be unignored by !ignored/")
	}
	if m.IsIgnored("ignored/file.txt", false) {
		t.Fatalf("expected ignored/file.txt to be unignored by !ignored/")
	}

	m = NewMatcher([]string{
		"ignored/",
		"!ignored/",
		"!ignored/**",
	})
	if m.IsIgnored("ignored/file.txt", false) {
		t.Fatalf("expected ignored/file.txt to remain unignored")
	}
}

func TestMatcher_AnchoredPatternsOnlyMatchRoot(t *testing.T) {
	m := NewMatcher([]string{"/root-only.md"})

	if !m.IsIgnored("root-only.md", false) {
		t.Fatalf("expected /root-only.md to ignore root-only.md")
	}
	if m.IsIgnored("nested/root-only.md", false) {
		t.Fatalf("expected /root-only.md to not ignore nested/root-only.md")
	}
}

func TestMatcher_PatternWithoutSlashMatchesRootAndNested(t *testing.T) {
	m := NewMatcher([]string{"foo"})

	if !m.IsIgnored("foo", false) {
		t.Fatalf("expected foo to ignore root foo")
	}
	if !m.IsIgnored("nested/foo", false) {
		t.Fatalf("expected foo to ignore nested/foo")
	}
}

func TestMatcher_DirPatternWithoutSlashMatchesRootAndNested(t *testing.T) {
	m := NewMatcher([]string{"vendor/"})

	if !m.IsIgnored("vendor", true) {
		t.Fatalf("expected vendor/ to ignore root vendor dir")
	}
	if !m.IsIgnored("vendor/file.go", false) {
		t.Fatalf("expected vendor/ to ignore files under root vendor")
	}
	if !m.IsIgnored("nested/vendor", true) {
		t.Fatalf("expected vendor/ to ignore nested/vendor dir")
	}
	if !m.IsIgnored("nested/vendor/file.go", false) {
		t.Fatalf("expected vendor/ to ignore files under nested/vendor")
	}
}

func TestMatcher_DirNegationDoesNotOverrideFilePattern(t *testing.T) {
	m := NewMatcher([]string{
		"dir/*",
		"!dir/",
	})

	if m.IsIgnored("dir", true) {
		t.Fatalf("expected dir/ to be unignored by !dir/")
	}
	if !m.IsIgnored("dir/file.txt", false) {
		t.Fatalf("expected dir/file.txt to remain ignored by dir/* despite !dir/")
	}
}

// SPEC-0064: a dir-only negation is an include boundary that suppresses
// earlier rules excluding the directory itself — including `dir/**`, which
// matches the directory. Contents-only patterns like `dir/*` are not
// suppressed (see TestMatcher_DirNegationDoesNotOverrideFilePattern).
func TestMatcher_DirNegationSuppressesRecursivePattern(t *testing.T) {
	m := NewMatcher([]string{
		"dir/**",
		"!dir/",
	})

	if m.IsIgnored("dir", true) {
		t.Fatalf("expected dir/ to be unignored by !dir/")
	}
	if m.IsIgnored("dir/sub/file.txt", false) {
		t.Fatalf("expected dir/sub/file.txt to be re-included by the !dir/ boundary")
	}
}

func TestMatcher_FileNegationRequiresParentUnignored(t *testing.T) {
	m := NewMatcher([]string{
		"dir/",
		"!dir/file.txt",
	})

	if !m.IsIgnored("dir/file.txt", false) {
		t.Fatalf("expected dir/file.txt to remain ignored without !dir/")
	}

	m = NewMatcher([]string{
		"dir/",
		"!dir/",
		"!dir/file.txt",
	})
	if m.IsIgnored("dir/file.txt", false) {
		t.Fatalf("expected dir/file.txt to be unignored after !dir/ and !dir/file.txt")
	}
}

func TestMatcher_CommentsAndEscapes(t *testing.T) {
	m := NewMatcher([]string{
		"# comment",
		"\\#literal",
		"\\!bang",
	})

	if m.IsIgnored("comment", false) {
		t.Fatalf("expected comment line to be ignored")
	}
	if !m.IsIgnored("#literal", false) {
		t.Fatalf("expected \\#literal to match #literal")
	}
	if !m.IsIgnored("!bang", false) {
		t.Fatalf("expected \\!bang to match !bang")
	}
}

func TestMatcher_TrailingSpaces(t *testing.T) {
	m := NewMatcher([]string{"foo "})
	if !m.IsIgnored("foo", false) {
		t.Fatalf("expected trailing spaces to be ignored")
	}
	if m.IsIgnored("foo ", false) {
		t.Fatalf("expected unescaped trailing space to not match literal space")
	}

	m = NewMatcher([]string{"foo\\ "})
	if !m.IsIgnored("foo ", false) {
		t.Fatalf("expected escaped trailing space to match literal space")
	}
	if m.IsIgnored("foo", false) {
		t.Fatalf("expected escaped trailing space to not match foo")
	}
}

func TestMatcher_EscapedWildcardLiteral(t *testing.T) {
	m := NewMatcher([]string{"\\*"})
	if !m.IsIgnored("*", false) {
		t.Fatalf("expected \\* to match literal *")
	}
	if m.IsIgnored("foo", false) {
		t.Fatalf("expected \\* to not match foo")
	}
}

func TestMatcher_IncludeBoundaryTrailingSpaces(t *testing.T) {
	for _, boundary := range []string{"!dir/ ", "!/dir/   "} {
		t.Run(boundary, func(t *testing.T) {
			m := NewMatcher([]string{"dir/", boundary})
			if m.IsIgnored("dir/sub/file.md", false) {
				t.Fatal("expected include boundary with trailing spaces to re-include dir/sub/file.md")
			}
			d := m.Explain("dir/sub/file.md", false)
			if d.Ignored || d.Boundary == nil || d.Boundary.Pattern != boundary {
				t.Fatalf("expected original include rule in boundary attribution, got %+v", d)
			}
		})
	}
}
