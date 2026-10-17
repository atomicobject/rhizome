package operations

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

var wikilinkPattern = regexp.MustCompile(`!?\[\[([^\]]+)\]\]`)

func normalizeNotePath(path string, vaultDef obsidian.VaultDefinition, vaultPaths paths.VaultPaths) string {
	if path == "" {
		return ""
	}
	path = string(paths.Normalize(path))
	if filepath.IsAbs(path) && vaultDef.BasePath() != "" && vaultPaths.Root() != "" {
		if rel, err := vaultPaths.RelStrict(path); err == nil && rel.String() != "" {
			path = rel.String()
		}
	}
	return string(paths.Normalize(path))
}

func expandFromMatchedNotes(linked []LinkedNoteContext, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, cache *obsidian.NotePathCache, exclude map[string]struct{}, matchExpr []string, limit int) []LinkedNoteContext {
	if len(linked) == 0 || noteMgr == nil || cache == nil || limit <= 0 {
		return linked
	}
	_, expr, err := ParseInputsWithExpression(matchExpr)
	if err != nil || expr == nil {
		return linked
	}

	seen := make(map[string]struct{}, len(linked))
	for _, ln := range linked {
		if ln.Path != "" {
			seen[ln.Path] = struct{}{}
		}
	}

	added := 0
	const hardCap = 25

	for _, src := range linked {
		if src.Path == "" {
			continue
		}
		if src.Frontmatter == nil {
			src.Frontmatter = blessedFrontmatter(noteMgr, src.Path)
		}
		if !linkedNoteMatches(src, expr) {
			continue
		}

		content, err := noteMgr.GetContents(vaultDef, src.Path)
		if err != nil || content == "" {
			continue
		}

		kind := "linkedFromNote"
		if hasLinkedNoteTag(src, "type/hub") {
			kind = "linkedFromHub"
		} else if hasLinkedNoteTag(src, "maps-of-content") || hasLinkedNoteTag(src, "type/moc") {
			kind = "linkedFromMOC"
		}

		matches := wikilinkPattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			target := strings.TrimSpace(m[1])
			if target == "" {
				continue
			}
			if pipe := strings.Index(target, "|"); pipe >= 0 {
				target = target[:pipe]
			}
			if hash := strings.Index(target, "#"); hash >= 0 {
				target = target[:hash]
			}
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}

			resolved, ok := cache.ResolveNote(target)
			if !ok || resolved == "" {
				continue
			}
			resolved = string(paths.NormalizeNotePath(resolved))
			if _, skip := exclude[resolved]; skip {
				continue
			}
			if _, ok := seen[resolved]; ok {
				continue
			}
			seen[resolved] = struct{}{}

			fm := blessedFrontmatter(noteMgr, resolved)
			linked = append(linked, LinkedNoteContext{
				Path:        resolved,
				Title:       paths.NoteTitle(resolved),
				Kind:        kind,
				Snippet:     src.Title,
				Frontmatter: fm,
			})

			added++
			if added >= limit || added >= hardCap {
				return linked
			}
		}
	}

	return linked
}

func linkedNoteMatches(note LinkedNoteContext, expr *InputExpression) bool {
	if expr == nil {
		return false
	}
	switch expr.Type {
	case exprLeaf:
		if expr.Input == nil {
			return false
		}
		in := expr.Input
		switch in.Type {
		case InputTypeTag:
			return hasLinkedNoteTag(note, in.Value)
		case InputTypeFind:
			q := strings.ToLower(strings.TrimSpace(in.Value))
			if q == "" {
				return false
			}
			return strings.Contains(strings.ToLower(note.Title), q) || strings.Contains(strings.ToLower(note.Path), q)
		case InputTypeFile:
			pat := strings.TrimSpace(in.Value)
			if pat == "" {
				return false
			}
			ok, _ := doublestar.Match(filepath.ToSlash(pat), filepath.ToSlash(note.Path))
			if ok {
				return true
			}
			// Also allow exact match on basename for convenience.
			return strings.EqualFold(filepath.Base(pat), filepath.Base(note.Path))
		case InputTypeProperty:
			if note.Frontmatter == nil || in.Property == "" {
				return false
			}
			v, ok := note.Frontmatter[in.Property]
			if !ok {
				return false
			}
			want := strings.TrimSpace(in.Value)
			return strings.EqualFold(strings.TrimSpace(fmt.Sprint(v)), want)
		default:
			return false
		}
	case exprAnd:
		return linkedNoteMatches(note, expr.Left) && linkedNoteMatches(note, expr.Right)
	case exprOr:
		return linkedNoteMatches(note, expr.Left) || linkedNoteMatches(note, expr.Right)
	case exprNot:
		return !linkedNoteMatches(note, expr.Left)
	default:
		return false
	}
}

func hasLinkedNoteTag(note LinkedNoteContext, tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" || note.Frontmatter == nil {
		return false
	}
	raw, ok := note.Frontmatter["tags"]
	if !ok {
		return false
	}
	switch t := raw.(type) {
	case []string:
		for _, v := range t {
			if strings.ToLower(strings.TrimSpace(v)) == tag {
				return true
			}
		}
	case []interface{}:
		for _, v := range t {
			if s, ok := v.(string); ok && strings.ToLower(strings.TrimSpace(s)) == tag {
				return true
			}
		}
	case string:
		// Support comma-separated.
		for _, v := range strings.Split(t, ",") {
			if strings.ToLower(strings.TrimSpace(v)) == tag {
				return true
			}
		}
	}
	return false
}
