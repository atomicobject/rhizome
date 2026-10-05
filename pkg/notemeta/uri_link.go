package notemeta

import (
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ResolveProjectedLink applies the provider's closed link semantics through
// the shared note candidate model. Providers supply decoded components and
// the resolver owns source-relative paths, extension matching, ambiguity, and
// the external/traversal boundary.
func ResolveProjectedLink(cache *obsidian.NotePathCache, sourcePath string, base *noteformat.DocumentBaseFact, link noteformat.UnresolvedAuthoredLinkFact) (string, bool) {
	if cache == nil {
		return "", false
	}
	switch link.Resolution {
	case noteformat.LinkResolutionNoteReference:
		return cache.ResolveNote(link.ResolverInput)
	case noteformat.LinkResolutionRelativePath:
		if resolved, ok := cache.ResolveMdLink(link.ResolverInput, sourcePath); ok {
			return resolved, true
		}
		return cache.ResolveNote(link.ResolverInput)
	case noteformat.LinkResolutionURI:
		return ResolveURIProjectionLink(cache, sourcePath, base, link)
	default:
		return "", false
	}
}

// ResolveURIProjectionLink resolves one provider-decoded URI reference. The
// URI path is the only component used for note identity; query and fragment
// remain navigation state and never affect graph identity.
//
// The candidate set is intentionally source-neutral. An explicit suffix must
// name an authored path under the platform filesystem casing rules. An
// extensionless target is matched against all note paths and aliases together,
// so Markdown never wins by default and a
// same-name HTML/Markdown pair remains ambiguous. Directory-like targets do
// not infer index files.
func ResolveURIProjectionLink(cache *obsidian.NotePathCache, sourcePath string, base *noteformat.DocumentBaseFact, link noteformat.UnresolvedAuthoredLinkFact) (string, bool) {
	if cache == nil || link.URI == nil {
		return "", false
	}
	ref := *link.URI
	if ref.Encoding != noteformat.URIEncodingPercent || ref.Scheme != "" || ref.Authority != "" || ref.ProtocolRelative {
		return "", false
	}

	basePath := sourcePath
	if base != nil {
		baseURI := base.URI
		if baseURI.Scheme != "" || baseURI.Authority != "" || baseURI.ProtocolRelative {
			// A relative href inherits an external document base. It must not
			// become a vault edge merely because its path happens to match.
			return "", false
		}
		if baseURI.Path != "" {
			basePath = uriVaultPath(sourcePath, baseURI.Path)
			if strings.HasSuffix(baseURI.Path, "/") {
				basePath += "/"
			}
		}
	}
	// An empty URI path resolves to the effective document URL. With no base it
	// is the source document; an internal file base makes it that target.
	if ref.Path == "" {
		return uniqueURIPathCandidate(cache, path.Clean(strings.ReplaceAll(basePath, "\\", "/")))
	}
	if strings.HasSuffix(ref.Path, "/") {
		return "", false
	}
	target := uriVaultPath(basePath, ref.Path)
	if !isVaultRelativeURIPath(target) {
		return "", false
	}
	return uniqueURIPathCandidate(cache, target)
}

func isVaultRelativeURIPath(target string) bool {
	return target != "" && target != "." && target != ".." && !strings.HasPrefix(target, "../") && !path.IsAbs(target)
}

func uniqueURIPathCandidate(cache *obsidian.NotePathCache, target string) (string, bool) {
	if !isVaultRelativeURIPath(target) {
		return "", false
	}
	// A supported explicit suffix is an exact authored path. A dotted alias
	// whose suffix is not represented by a note path remains an alias candidate
	// in the shared extensionless set.
	if uriHasExplicitSuffix(cache, target) {
		return cache.ResolveExplicitPath(target)
	}

	candidates := make(map[string]struct{})
	for notePath := range cache.NotePaths {
		if uriPathCandidateMatches(notePath, target) {
			candidates[notePath] = struct{}{}
		}
	}
	aliasTarget := path.Base(target)
	for alias, notePaths := range cache.Aliases {
		if alias != target && alias != aliasTarget {
			continue
		}
		for _, notePath := range notePaths {
			if _, exists := cache.NotePaths[notePath]; exists {
				candidates[notePath] = struct{}{}
			}
		}
	}
	if len(candidates) != 1 {
		return "", false
	}
	resolved := make([]string, 0, 1)
	for notePath := range candidates {
		resolved = append(resolved, notePath)
	}
	sort.Strings(resolved)
	return resolved[0], true
}

func uriHasExplicitSuffix(cache *obsidian.NotePathCache, target string) bool {
	ext := path.Ext(target)
	if ext == "" {
		return false
	}
	return cache.HasNoteExtension(ext)
}

func uriPathCandidateMatches(notePath, target string) bool {
	notePath = string(path.Clean(strings.ReplaceAll(notePath, "\\", "/")))
	if notePath == target {
		return true
	}
	withoutExtension := strings.TrimSuffix(notePath, path.Ext(notePath))
	if withoutExtension == target {
		return true
	}
	return !strings.Contains(target, "/") && path.Base(withoutExtension) == target
}

func uriVaultPath(basePath, referencePath string) string {
	if referencePath == "" {
		return path.Clean(strings.ReplaceAll(basePath, "\\", "/"))
	}
	referencePath = strings.ReplaceAll(referencePath, "\\", "/")
	if strings.HasPrefix(referencePath, "/") {
		return path.Clean(strings.TrimPrefix(referencePath, "/"))
	}
	basePath = strings.ReplaceAll(basePath, "\\", "/")
	baseDir := path.Dir(basePath)
	if strings.HasSuffix(basePath, "/") {
		baseDir = strings.TrimSuffix(basePath, "/")
	}
	return path.Clean(path.Join(baseDir, referencePath))
}
