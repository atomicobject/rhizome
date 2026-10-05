package codeanchor

import (
	"path/filepath"
	"regexp"
	"strings"
)

type mentionCandidate struct {
	Source string // "at" | "mdlink"

	// For Source="at"
	Qualifier string
	Symbol    string

	// For Source="mdlink"
	TargetPath string // repo-relative, slash-normalized
	Fragment   string // raw fragment after '#'
}

var (
	// Require at least one dot to avoid emails and @bareMentions.
	atMentionRe = regexp.MustCompile(`(^|[^A-Za-z0-9_])@([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+)`)
	// Minimal markdown link capture: [text](dest) with optional title ignored.
	mdLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)`)
)

func extractMentionCandidates(noteRelPath string, content string) []mentionCandidate {
	// Avoid indexing mentions inside fenced code blocks and inline code spans.
	safe := stripMarkdownCode(content)

	var out []mentionCandidate

	// @pkg.tail.Symbol
	for _, m := range atMentionRe.FindAllStringSubmatch(safe, -1) {
		if len(m) < 3 {
			continue
		}
		raw := m[2]
		parts := strings.Split(raw, ".")
		if len(parts) < 2 {
			continue
		}
		sym := parts[len(parts)-1]
		qual := strings.Join(parts[:len(parts)-1], ".")
		if sym == "" || qual == "" {
			continue
		}
		out = append(out, mentionCandidate{
			Source:    "at",
			Qualifier: qual,
			Symbol:    sym,
		})
	}

	// [Text](pkg/..../file.go#Symbol) or relative paths.
	for _, m := range mdLinkRe.FindAllStringSubmatch(safe, -1) {
		if len(m) < 2 {
			continue
		}
		dest := strings.TrimSpace(m[1])
		if dest == "" {
			continue
		}
		if strings.HasPrefix(dest, "#") {
			// In-note heading anchor.
			continue
		}
		if strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
			continue
		}

		pathPart, frag, _ := strings.Cut(dest, "#")
		pathPart = strings.TrimSpace(pathPart)
		if pathPart == "" {
			continue
		}

		target, ok := normalizeRepoRelativeLinkTarget(noteRelPath, pathPart)
		if !ok {
			continue
		}
		out = append(out, mentionCandidate{
			Source:     "mdlink",
			TargetPath: target,
			Fragment:   frag,
		})
	}

	return out
}

func normalizeRepoRelativeLinkTarget(noteRelPath, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	raw = strings.TrimPrefix(raw, "/")

	// Treat repo-root-relative links ("pkg/...") as-is, but resolve ./ and ../
	// relative to the note location for portability in repo-root vaults.
	clean := raw
	if strings.HasPrefix(clean, ".") {
		baseDir := filepath.Dir(filepath.ToSlash(noteRelPath))
		joined := filepath.ToSlash(filepath.Join(baseDir, filepath.FromSlash(clean)))
		clean = joined
	}
	clean = filepath.ToSlash(filepath.Clean(filepath.FromSlash(clean)))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, `:\`) {
		return "", false
	}
	return clean, true
}

func stripMarkdownCode(content string) string {
	// This is intentionally lightweight and best-effort.
	// We only need to avoid false positives in code samples.
	replacements := []struct {
		re   *regexp.Regexp
		with string
	}{
		{regexp.MustCompile("(?s)```.*?```"), " "},
		{regexp.MustCompile("(?s)~~~.*?~~~"), " "},
		{regexp.MustCompile("`[^`]+`"), " "},
	}
	out := content
	for _, r := range replacements {
		out = r.re.ReplaceAllString(out, r.with)
	}
	return out
}

func looksLikeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			// ok
		case i > 0 && (r >= '0' && r <= '9'):
			// ok
		default:
			return false
		}
	}
	return true
}
