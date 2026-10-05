package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

const (
	defaultExternalReferenceLimit = 20
	maxExternalReferenceLimit     = 100
)

// ExternalReferences projects local uses of one canonical pathless target.
// The operation is deliberately uncached: edits and local-definition precedence
// must be visible on the next read.
func (s *Store) ExternalReferences(ctx context.Context, query codeanchor.ExternalReferenceQuery) (codeanchor.ExternalReferenceQueryResult, error) {
	query.Handle = strings.TrimSpace(query.Handle)
	query.Ecosystem = codeanchor.ExternalEcosystem(strings.TrimSpace(string(query.Ecosystem)))
	query.Module = strings.TrimSpace(query.Module)
	query.SymbolPrefix = strings.TrimSpace(query.SymbolPrefix)
	handleMode := query.Handle != ""
	structuredMode := query.Ecosystem != "" || query.Module != "" || query.SymbolPrefix != ""
	if handleMode == structuredMode {
		return codeanchor.ExternalReferenceQueryResult{}, fmt.Errorf("external reference query requires exactly one lookup mode: handle or structured identity")
	}
	if structuredMode && (query.Ecosystem == "" || query.Module == "") {
		return codeanchor.ExternalReferenceQueryResult{}, fmt.Errorf("structured external reference query requires ecosystem and module")
	}
	if structuredMode {
		canonical, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
			Ecosystem: query.Ecosystem, Module: query.Module, Kind: codeanchor.ExternalTargetModule,
		})
		if err != nil {
			return codeanchor.ExternalReferenceQueryResult{}, fmt.Errorf("invalid structured external reference identity: %w", err)
		}
		query.Module = canonical.Module
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultExternalReferenceLimit
	}
	if limit > maxExternalReferenceLimit {
		limit = maxExternalReferenceLimit
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return codeanchor.ExternalReferenceQueryResult{}, err
	}
	defer tx.Rollback()
	finish := func(result codeanchor.ExternalReferenceQueryResult) (codeanchor.ExternalReferenceQueryResult, error) {
		if err := tx.Commit(); err != nil {
			return codeanchor.ExternalReferenceQueryResult{}, err
		}
		return result, nil
	}
	candidates, moreCandidates, err := externalReferenceCandidates(ctx, tx, query, limit)
	if err != nil {
		return codeanchor.ExternalReferenceQueryResult{}, err
	}
	result := codeanchor.ExternalReferenceQueryResult{Candidates: candidates, Truncated: moreCandidates}
	if len(candidates) == 0 {
		result.Status = "not_found"
		return finish(result)
	}
	if len(candidates) != 1 || moreCandidates {
		result.Status = "ambiguous"
		return finish(result)
	}
	result.Status = "resolved"
	result.Target = &candidates[0]
	result.Candidates = nil

	groups, truncated, err := externalSymbolReferenceGroups(ctx, tx, candidates[0].ID, limit)
	if err != nil {
		return codeanchor.ExternalReferenceQueryResult{}, err
	}
	result.Calls = groups[codeanchor.RefKindCalls]
	result.Types = groups[codeanchor.RefKindTypeRef]
	result.Members = groups[codeanchor.RefKindMemberRef]
	result.Truncated = result.Truncated || truncated
	result.Imports, truncated, err = externalImportReferences(ctx, tx, candidates[0].ID, limit)
	if err != nil {
		return codeanchor.ExternalReferenceQueryResult{}, err
	}
	result.Truncated = result.Truncated || truncated
	return finish(result)
}

type externalReferenceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func externalReferenceCandidates(ctx context.Context, q externalReferenceQueryer, query codeanchor.ExternalReferenceQuery, limit int) ([]codeanchor.ExternalReferenceTarget, bool, error) {
	var rows *sql.Rows
	var err error
	if query.Handle != "" {
		rows, err = q.QueryContext(ctx, `SELECT external_id, handle, ecosystem, module, symbol_path, target_kind
			FROM intel_external_targets WHERE handle=? ORDER BY handle LIMIT 1`, query.Handle)
	} else if query.SymbolPrefix == "" {
		rows, err = q.QueryContext(ctx, `SELECT external_id, handle, ecosystem, module, symbol_path, target_kind
			FROM intel_external_targets
			WHERE ecosystem=? AND module=?
			ORDER BY handle LIMIT ?`, query.Ecosystem, query.Module, limit+1)
	} else {
		upperBound := prefixUpperBound(query.SymbolPrefix)
		rows, err = q.QueryContext(ctx, `SELECT external_id, handle, ecosystem, module, symbol_path, target_kind
			FROM intel_external_targets
			WHERE ecosystem=? AND module=? AND symbol_path>=? AND symbol_path<?
			ORDER BY handle LIMIT ?`, query.Ecosystem, query.Module, query.SymbolPrefix, upperBound, limit+1)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := make([]codeanchor.ExternalReferenceTarget, 0, limit+1)
	for rows.Next() {
		var target codeanchor.ExternalReferenceTarget
		if err := rows.Scan(&target.ID, &target.Handle, &target.Ecosystem, &target.Module, &target.SymbolPath, &target.Kind); err != nil {
			return nil, false, err
		}
		target.External = true
		target.Pathless = true
		result = append(result, target)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(result) > limit
	if truncated {
		result = result[:limit]
	}
	return result, truncated, nil
}

// prefixUpperBound returns the exclusive upper boundary for all SQLite BINARY
// TEXT values beginning with prefix. Incrementing the final UTF-8 byte is safe
// as a comparison sentinel even when it is not itself valid UTF-8: valid UTF-8
// never contains 0xff, and SQLite's default BINARY collation compares bytes.
func prefixUpperBound(prefix string) string {
	upper := []byte(prefix)
	upper[len(upper)-1]++
	return string(upper)
}

func externalSymbolReferenceGroups(ctx context.Context, q externalReferenceQueryer, externalID codeanchor.ExternalTargetID, limit int) (map[codeanchor.RefKind][]codeanchor.ExternalReferenceUse, bool, error) {
	groups := make(map[codeanchor.RefKind][]codeanchor.ExternalReferenceUse, 3)
	truncated := false
	for _, kind := range []codeanchor.RefKind{codeanchor.RefKindCalls, codeanchor.RefKindTypeRef, codeanchor.RefKindMemberRef} {
		rows, err := q.QueryContext(ctx, `SELECT se.owner_fqn, f.path, se.evidence_kind, se.confidence,
			se.imported_name, se.local_name, se.manifest_path, se.declared_range, se.version_scope
			FROM intel_external_symbol_evidence se
			JOIN intel_symbol_ref_files f ON f.file_id=se.src_file_id
			JOIN intel_symbol_ref_targets rt ON rt.target_id=se.raw_target_id
			WHERE se.external_id=? AND se.ref_kind=?
			  AND NOT EXISTS (SELECT 1 FROM symbols s WHERE s.lang=rt.dst_lang AND s.fqn=rt.dst_fqn)
			ORDER BY f.path, se.owner_fqn, se.raw_target_id LIMIT ?`, externalID, kind, limit+1)
		if err != nil {
			return nil, false, err
		}
		uses, err := scanExternalReferenceUses(rows)
		if err != nil {
			return nil, false, err
		}
		if len(uses) > limit {
			uses = uses[:limit]
			truncated = true
		}
		groups[kind] = uses
	}
	return groups, truncated, nil
}

func externalImportReferences(ctx context.Context, q externalReferenceQueryer, externalID codeanchor.ExternalTargetID, limit int) ([]codeanchor.ExternalReferenceUse, bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT '', src_path, evidence_kind, confidence,
		imported_name, local_name, manifest_path, declared_range, version_scope
		FROM intel_external_import_evidence ie WHERE external_id=?
		  AND NOT EXISTS (
		    SELECT 1 FROM files f JOIN intel_module_defs d ON d.lang=f.lang
		    WHERE f.path=ie.src_path AND d.module=ie.module
		  )
		ORDER BY src_path, module, binding_ordinal LIMIT ?`, externalID, limit+1)
	if err != nil {
		return nil, false, err
	}
	uses, err := scanExternalReferenceUses(rows)
	if err != nil {
		return nil, false, err
	}
	truncated := len(uses) > limit
	if truncated {
		uses = uses[:limit]
	}
	return uses, truncated, nil
}

func scanExternalReferenceUses(rows *sql.Rows) ([]codeanchor.ExternalReferenceUse, error) {
	defer rows.Close()
	var result []codeanchor.ExternalReferenceUse
	for rows.Next() {
		var use codeanchor.ExternalReferenceUse
		if err := rows.Scan(&use.OwnerFQN, &use.Path, &use.Evidence, &use.Confidence,
			&use.ImportedName, &use.LocalName, &use.ManifestPath, &use.DeclaredRange, &use.VersionScope); err != nil {
			return nil, err
		}
		result = append(result, use)
	}
	return result, rows.Err()
}
