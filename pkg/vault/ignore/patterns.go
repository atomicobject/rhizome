package ignore

import (
	"os"
	"strings"

	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

const utf8BOM = "\uFEFF"

// ruleSource records where a pattern came from for Explain attribution.
type ruleSource struct {
	layer string // LayerDefault, LayerGitignore, LayerRhizome, LayerConfig, or ""
	file  string // root-relative source file ("" for builtin defaults / config excludes)
	line  int    // 1-based line number in the source file (0 when synthetic)
	raw   string // original pattern text, including any leading '!'
}

type patternWithMeta struct {
	pattern gitignore.Pattern
	dirOnly bool
	source  ruleSource
	// boundaryPath marks a declarative include boundary: a literal dir-only
	// negation such as `!/app/`. Boundary patterns do not participate in plain
	// last-match-wins evaluation; instead they suppress earlier rules that
	// excluded the boundary directory itself (see applyBoundaries).
	boundaryPath string
}

func parsePatterns(lines []string, domain []string) []patternWithMeta {
	return parsePatternsWithSource(lines, domain, "", "")
}

func parsePatternsWithSource(lines []string, domain []string, layer, file string) []patternWithMeta {
	out := make([]patternWithMeta, 0, len(lines))
	for i, line := range lines {
		raw := strings.TrimSuffix(line, "\r")
		if i == 0 {
			raw = strings.TrimPrefix(raw, utf8BOM)
		}
		normalized, dirOnly, ok := normalizeGitignoreLine(line, i == 0)
		if !ok {
			continue
		}
		out = append(out, patternWithMeta{
			pattern:      gitignore.ParsePattern(normalized, domain),
			dirOnly:      dirOnly,
			source:       ruleSource{layer: layer, file: file, line: i + 1, raw: raw},
			boundaryPath: boundaryPathFor(normalized, dirOnly, domain),
		})
	}
	return out
}

func normalizeGitignoreLine(line string, first bool) (string, bool, bool) {
	if first {
		line = strings.TrimPrefix(line, utf8BOM)
	}
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", false, false
	}
	if strings.HasPrefix(line, "#") {
		return "", false, false
	}

	dirOnly := isDirOnlyPattern(line)

	var b strings.Builder
	b.Grow(len(line))
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch != '\\' || i+1 >= len(line) {
			b.WriteByte(ch)
			continue
		}
		next := line[i+1]
		switch next {
		case '#':
			b.WriteString("[#]")
		case '!':
			b.WriteString("[!]")
		case '*':
			b.WriteString("[*]")
		case '?':
			b.WriteString("[?]")
		case '[':
			b.WriteString("[[]")
		case ']':
			b.WriteString("[]]")
		case ' ':
			b.WriteString("[ ]")
		default:
			b.WriteByte(next)
		}
		i++
	}
	return b.String(), dirOnly, true
}

func readIgnoreLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(data), "\n")
}

func isDirOnlyPattern(line string) bool {
	line = strings.TrimSuffix(line, "\r")
	if strings.HasPrefix(line, utf8BOM) {
		line = strings.TrimPrefix(line, utf8BOM)
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return false
	}

	if strings.HasPrefix(line, "!") {
		line = line[1:]
	}
	if !strings.HasSuffix(line, "\\ ") {
		line = strings.TrimRight(line, " ")
	}
	return strings.HasSuffix(line, "/")
}

type compiledMatcher interface {
	Match(path []string, isDir bool) bool
}

type patternMatcher struct {
	patterns []patternWithMeta
}

func newPatternMatcher(patterns []patternWithMeta) compiledMatcher {
	if len(patterns) == 0 {
		return nil
	}
	return &patternMatcher{patterns: patterns}
}

func (m *patternMatcher) Match(path []string, isDir bool) bool {
	_, res := decide(m.patterns, path, isDir)
	return res == gitignore.Exclude
}

// decide returns the last-match-wins deciding pattern index and result.
// Index is -1 when no pattern matches.
func decide(patterns []patternWithMeta, path []string, isDir bool) (int, gitignore.MatchResult) {
	for i := len(patterns) - 1; i >= 0; i-- {
		p := patterns[i]
		if !isDir && p.dirOnly {
			continue
		}
		if match := p.pattern.Match(path, isDir); match > gitignore.NoMatch {
			return i, match
		}
	}
	return -1, gitignore.NoMatch
}
