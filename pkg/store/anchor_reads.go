package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

// Anchors returns all anchors.
func (s *Store) Anchors(ctx context.Context) ([]codeanchor.Anchor, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix
		FROM anchors
	`)
	if err != nil {
		return nil, err
	}
	return s.readAnchors(ctx, rows)
}

// AnchorsByIDs returns anchors filtered by id set.
func (s *Store) AnchorsByIDs(ctx context.Context, ids []int64) ([]codeanchor.Anchor, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	holders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		holders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT id, label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix
		FROM anchors
		WHERE id IN (%s)
	`, strings.Join(holders, ","))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.readAnchors(ctx, rows)
}

// AnchorsByNotePaths returns anchors currently defined by the provided note paths.
func (s *Store) AnchorsByNotePaths(ctx context.Context, paths []string) (map[string][]codeanchor.Anchor, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	holders := make([]string, len(paths))
	args := make([]any, len(paths))
	for i, path := range paths {
		holders[i] = "?"
		args[i] = path
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT n.path, a.id, a.label, a.kind, a.lang, a.base_lang, a.base_pkg, a.base_name, a.base_member, a.ann_lang, a.ann_pkg, a.ann_name, a.ann_args_json, a.path_prefix
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchors a ON a.id = na.anchor_id
		WHERE n.path IN (%s)
	`, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.Anchor, len(paths))
	globIDs := make(map[int64]struct{})
	for rows.Next() {
		var notePath string
		var a codeanchor.Anchor
		var kind, lang, baseLang, basePkg, baseName, annLang, annPkg, annName, annArgs string
		var baseMember int
		var pathPrefix sql.NullString
		if err := rows.Scan(&notePath, &a.ID, &a.Label, &kind, &lang, &baseLang, &basePkg, &baseName, &baseMember, &annLang, &annPkg, &annName, &annArgs, &pathPrefix); err != nil {
			return nil, err
		}
		a.Kind = codeanchor.AnchorKind(kind)
		a.Lang = codeanchor.Lang(lang)
		if pathPrefix.Valid {
			a.PathPrefix = pathPrefix.String
		}
		if a.Kind == codeanchor.AnchorGlob {
			globIDs[a.ID] = struct{}{}
		}
		if baseName != "" {
			a.BaseSym = &codeanchor.SymbolRef{Lang: codeanchor.Lang(baseLang), Pkg: basePkg, Name: baseName, Member: baseMember != 0}
		}
		if annName != "" {
			selector := codeanchor.AnnotationSelector{
				Symbol: codeanchor.SymbolRef{Lang: codeanchor.Lang(annLang), Pkg: annPkg, Name: annName},
			}
			if annArgs != "" {
				_ = json.Unmarshal([]byte(annArgs), &selector.ArgFilters)
			}
			a.Ann = &selector
		}
		out[notePath] = append(out[notePath], a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(globIDs) > 0 {
		ids := make([]int64, 0, len(globIDs))
		for id := range globIDs {
			ids = append(ids, id)
		}
		byID, err := s.globsByAnchorIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for notePath, anchors := range out {
			for i := range anchors {
				if anchors[i].Kind == codeanchor.AnchorGlob {
					anchors[i].Globs = byID[anchors[i].ID]
				}
			}
			out[notePath] = anchors
		}
	}
	return out, nil
}
