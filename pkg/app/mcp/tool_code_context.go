package mcp

import (
	"bufio"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
	"os"
	"path/filepath"
	"strings"
)

func readAnchorSnippet(config Config, anchor codeanchor.IntelAnchor, contextLines, maxBytes int) (string, int, int, int, bool, string) {
	root := effectiveVaultRoot(config)
	vp, err := paths.NewVaultPaths(root)
	if err != nil || vp.Root() == "" {
		return "", 0, 0, 0, false, ""
	}
	rel, err := vp.RelCodeStrict(anchor.Path)
	if err != nil {
		return "", 0, 0, 0, false, ""
	}
	abs, err := vp.AbsCode(rel)
	if err != nil {
		return "", 0, 0, 0, false, ""
	}
	data, err := os.ReadFile(abs.String())
	if err != nil {
		return "", 0, 0, 0, false, ""
	}
	start := int(anchor.StartLine)
	end := int(anchor.EndLine)
	return sliceTextLines(string(data), start, end, contextLines, maxBytes)
}

func sliceTextLines(text string, startLine, endLine, contextLines, maxBytes int) (string, int, int, int, bool, string) {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	total := len(lines)
	if total == 1 && lines[0] == "" {
		total = 0
		lines = nil
	}
	if total == 0 {
		return "", 0, 0, 0, false, ""
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine < startLine {
		endLine = startLine
	}
	startLine -= contextLines
	endLine += contextLines
	if startLine < 1 {
		startLine = 1
	}
	if endLine > total {
		endLine = total
	}
	content := strings.Join(lines[startLine-1:endLine], "\n")
	truncated := false
	continuation := ""
	if maxBytes > 0 && len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
		continuation = fmt.Sprintf("increase maxBytes or read %s:%d", "next range", endLine+1)
	}
	if endLine < total && continuation == "" {
		continuation = fmt.Sprintf("nextStartLine=%d", endLine+1)
	}
	return content, startLine, endLine, total, truncated, continuation
}

func findTestSnippets(config Config, needle string, limit int, maxBytes int) []CodeSymbolPayload {
	needle = strings.TrimSpace(needle)
	root := effectiveVaultRoot(config)
	if needle == "" || root == "" {
		return nil
	}
	vp, err := paths.NewVaultPaths(root)
	if err != nil || vp.Root() == "" {
		return nil
	}
	var out []CodeSymbolPayload
	_ = filepath.WalkDir(vp.Root(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if limit > 0 && len(out) >= limit {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			if shouldSkipCodeContextDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := vp.RelCodeStrict(path)
		if err != nil {
			return nil
		}
		if !looksLikeTestPath(rel.String()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), needle) {
			return nil
		}
		line := firstLineContaining(string(data), needle)
		content, start, end, total, truncated, continuation := sliceTextLines(string(data), line, line, 3, maxBytes)
		out = append(out, CodeSymbolPayload{
			Path:         rel.String(),
			Symbol:       needle,
			StartLine:    int64(line),
			EndLine:      int64(line),
			Content:      content,
			ContentStart: start,
			ContentEnd:   end,
			TotalLines:   total,
			Truncated:    truncated,
			Continuation: continuation,
		})
		return nil
	})
	return out
}

func shouldSkipCodeContextDir(name string) bool {
	switch name {
	case ".git", ".gocache", ".gomodcache", ".gotmp", "node_modules", "bin", "dist", "coverage":
		return true
	default:
		return false
	}
}

func firstLineContaining(text string, needle string) int {
	scanner := bufio.NewScanner(strings.NewReader(text))
	line := 1
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), needle) {
			return line
		}
		line++
	}
	return 1
}

func looksLikeTestPath(path string) bool {
	lower := strings.ToLower(path)
	base := filepath.Base(lower)
	return strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.HasSuffix(lower, "_test.go") ||
		(codefile.IsTypeScriptJavaScriptPath(base) && strings.Contains(base, ".test.")) ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, "test.cs")
}

func renderCodeSymbolContext(resp CodeSymbolContextResponse) string {
	var b strings.Builder
	if resp.Definition != nil {
		fmt.Fprintf(&b, "Definition: %s %s %s\n", resp.Definition.Language, resp.Definition.FQN, resp.Definition.Path)
		if strings.TrimSpace(resp.Definition.Content) != "" {
			fmt.Fprintf(&b, "\n%s\n", resp.Definition.Content)
		}
	}
	if len(resp.Callers) > 0 {
		b.WriteString("\nCallers:\n")
		for _, caller := range resp.Callers {
			fmt.Fprintf(&b, "- %s %s\n", caller.FQN, caller.Path)
		}
	}
	if len(resp.Callees) > 0 {
		b.WriteString("\nCallees:\n")
		for _, callee := range resp.Callees {
			fmt.Fprintf(&b, "- %s %s\n", callee.FQN, callee.Path)
		}
	}
	if len(resp.Tests) > 0 {
		b.WriteString("\nTests:\n")
		for _, test := range resp.Tests {
			fmt.Fprintf(&b, "- %s:%d\n", test.Path, test.StartLine)
		}
	}
	return strings.TrimSpace(b.String())
}
