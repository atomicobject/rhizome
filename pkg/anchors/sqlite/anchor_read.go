package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

func scanAnchor(rows *sql.Rows) (codeanchor.Anchor, error) {
	var a codeanchor.Anchor
	var kind, lang, baseLang, basePkg, baseName, annLang, annPkg, annName, annArgs string
	var baseMember int
	var pathPrefix sql.NullString
	if err := rows.Scan(&a.ID, &a.Label, &kind, &lang, &baseLang, &basePkg, &baseName, &baseMember, &annLang, &annPkg, &annName, &annArgs, &pathPrefix); err != nil {
		return a, err
	}
	a.Kind = codeanchor.AnchorKind(kind)
	a.Lang = codeanchor.Lang(lang)
	if pathPrefix.Valid {
		a.PathPrefix = pathPrefix.String
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
	return a, nil
}

func existingAnchorsByLabels(ctx context.Context, tx *sql.Tx, labels []string) (map[string]codeanchor.Anchor, error) {
	existing := make(map[string]codeanchor.Anchor)
	if len(labels) == 0 {
		return existing, nil
	}
	holders, args := placeholders(labels)
	rows, err := tx.QueryContext(ctx, `
		SELECT id, label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix
		FROM anchors WHERE label IN (`+holders+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		a, err := scanAnchor(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		existing[a.Label] = a
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Fetch globs for existing glob anchors.
	var globIDs []int64
	for _, a := range existing {
		if a.Kind == codeanchor.AnchorGlob {
			globIDs = append(globIDs, a.ID)
		}
	}
	if len(globIDs) == 0 {
		return existing, nil
	}
	globPlaceholders := make([]string, len(globIDs))
	globArgs := make([]any, len(globIDs))
	for i, id := range globIDs {
		globPlaceholders[i] = "?"
		globArgs[i] = id
	}
	globRows, err := tx.QueryContext(ctx, `
		SELECT anchor_id, pattern FROM anchor_globs WHERE anchor_id IN (`+strings.Join(globPlaceholders, ",")+`)
	`, globArgs...)
	if err != nil {
		return nil, err
	}
	globsByID := make(map[int64][]string)
	for globRows.Next() {
		var anchorID int64
		var pattern string
		if err := globRows.Scan(&anchorID, &pattern); err != nil {
			globRows.Close()
			return nil, err
		}
		globsByID[anchorID] = append(globsByID[anchorID], pattern)
	}
	if err := globRows.Err(); err != nil {
		globRows.Close()
		return nil, err
	}
	globRows.Close()
	// Attach globs to existing anchors.
	for label, a := range existing {
		if globs, ok := globsByID[a.ID]; ok {
			a.Globs = globs
			existing[label] = a
		}
	}

	return existing, nil
}

func (s *Store) readAnchors(ctx context.Context, rows *sql.Rows) ([]codeanchor.Anchor, error) {
	defer rows.Close()

	var res []codeanchor.Anchor
	var globIDs []int64
	for rows.Next() {
		a, err := scanAnchor(rows)
		if err != nil {
			return nil, err
		}
		if a.Kind == codeanchor.AnchorGlob {
			globIDs = append(globIDs, a.ID)
		}
		res = append(res, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(globIDs) > 0 {
		byID, err := s.globsByAnchorIDs(ctx, globIDs)
		if err != nil {
			return nil, err
		}
		for i := range res {
			if res[i].Kind == codeanchor.AnchorGlob {
				res[i].Globs = byID[res[i].ID]
			}
		}
	}
	return res, nil
}
