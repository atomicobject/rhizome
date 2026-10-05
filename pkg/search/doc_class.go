package search

import (
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

func InferDocClass(path, typ string) DocClass {
	if !isDocCandidate(path, typ) {
		return DocClassNone
	}
	path = strings.ToLower(NormalizeLocalityPath(path))
	switch {
	case strings.HasPrefix(path, "docs/hubs/"):
		return DocClassHub
	case strings.HasPrefix(path, "docs/specs/"):
		return DocClassSpec
	case strings.HasPrefix(path, "docs/efforts/"):
		return DocClassEffort
	// Checked before docs/reference/ so analysis artifacts are not treated as
	// curated reference documentation.
	case strings.HasPrefix(path, "docs/reference/analysis/"):
		return DocClassAnalysis
	case strings.HasPrefix(path, "docs/reference/"):
		return DocClassReference
	case isGeneratedDocPath(path):
		return DocClassGenerated
	case isRepoGlobalDocPath(path):
		return DocClassRepoGlobal
	case strings.EqualFold(filepath.Base(path), "context.md"):
		return DocClassModule
	default:
		return DocClassNone
	}
}

func ClassifyCandidate(c Candidate) Candidate {
	if c.DocClass == "" || c.DocClass == DocClassNone {
		c.DocClass = InferDocClass(c.Path, c.Type)
	}
	return c
}

func isDocCandidate(path, typ string) bool {
	path = strings.TrimSpace(path)
	typ = strings.TrimSpace(strings.ToLower(typ))
	if path == "" {
		return false
	}
	if typ == "note" {
		return true
	}
	return false
}

func isRepoGlobalDocPath(path string) bool {
	switch path {
	case "readme.md", "agents.md", "rhizome.md", "claude.md":
		return true
	}
	return false
}

func isGeneratedDocPath(path string) bool {
	switch {
	case strings.HasPrefix(path, ".codex/"), strings.HasPrefix(path, ".claude/"), strings.HasPrefix(path, ".cursor/"):
		return true
	case strings.Contains(path, "/templates/skills/"), strings.Contains(path, "/rhizome-md-templates/"):
		return true
	default:
		return false
	}
}

func IsTestPath(path string) bool {
	return codeanchor.IsTestPath(path)
}

func IsEntryPointFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "main.go", "search.go", "index.go", "service.go", "server.go", "handler.go", "router.go", "api.go", "cli.go", "semantic.go":
		return true
	}
	dir := strings.ToLower(filepath.Base(filepath.Dir(path)))
	return dir != "" && base == dir+".go"
}
