package ignore

import (
	"io/fs"
	"os"
	"strings"
)

// Rules lists the rules m evaluates, layer by layer in evaluation order: the
// built-in list while it applies, every .gitignore file Rhizome reads, the
// .rhizome/ignore rules, and config excludes.
//
// Nested .gitignore files are found with one walk that prunes hidden
// directories and directories the matcher ignores, as discovery does, so a
// .gitignore inside an ignored folder is not listed: it never applies.
func (m *Matcher) Rules() []RuleRef {
	if m == nil {
		return nil
	}
	var out []RuleRef
	add := func(patterns []patternWithMeta) {
		for _, p := range patterns {
			out = append(out, *ruleRefFor(p))
		}
	}
	if m.allPatterns != nil {
		add(m.allPatterns)
		return out
	}
	add(m.defaultPatterns)
	add(m.gitignorePatterns())
	add(m.rhizomePatterns)
	add(m.userPatterns)
	return out
}

func (m *Matcher) gitignorePatterns() []patternWithMeta {
	if strings.TrimSpace(m.root) == "" {
		return nil
	}
	var out []patternWithMeta
	_ = fs.WalkDir(os.DirFS(m.root), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory is skipped; only the root stops the walk.
			if rel == "." {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if rel == "." {
			rel = ""
		} else if strings.HasPrefix(d.Name(), ".") || m.IsIgnoredShallow(rel, true) {
			return fs.SkipDir
		}
		out = append(out, m.gitignorePatternsForDir(rel)...)
		return nil
	})
	return out
}
