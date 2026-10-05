package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

const goRelationshipGenerationKey = "intel_go_relationship_generation"

func (s *Store) ReplaceGoPackageRelationships(ctx context.Context, replacements []codeanchor.GoPackageRelationshipReplacement) error {
	if len(replacements) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		changed := false
		for _, replacement := range replacements {
			if err := replaceGoPackageRelationshipsTx(ctx, tx, replacement); err != nil {
				return err
			}
			changed = true
		}
		if changed {
			return incrementGoRelationshipGenerationTx(ctx, tx)
		}
		return nil
	})
}

// GoPackageSnapshotsForPaths returns persisted package membership and source
// metadata in one read snapshot. Content is loaded by the service from the
// vault and verified by AnalyzeGoPackageRelationships before replacement.
// Empty paths select every currently indexed Go package.
func (s *Store) GoPackageSnapshotsForPaths(ctx context.Context, sourcePaths []string) ([]codeanchor.GoPackageSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	keys, err := goPackageKeysForSnapshotTx(ctx, tx, sourcePaths)
	if err != nil {
		return nil, err
	}
	snapshots := make([]codeanchor.GoPackageSnapshot, 0, len(keys))
	for _, packageKey := range keys {
		var key codeanchor.GoPackageKey
		if err := tx.QueryRowContext(ctx, `
			SELECT import_path, directory, package_name, build_variant FROM (
				SELECT import_path, directory, package_name, build_variant, 0 AS priority
				FROM intel_go_package_files WHERE package_key=?
				UNION ALL
				SELECT import_path, directory, package_name, build_variant, 1 AS priority
				FROM intel_go_package_relationship_state WHERE package_key=?
			) ORDER BY priority LIMIT 1
		`, packageKey, packageKey).Scan(&key.ImportPath, &key.Directory, &key.PackageName, &key.BuildVariant); err != nil {
			return nil, err
		}
		members, err := goPackageMembersForDirectoryTx(ctx, tx, key.Directory)
		if err != nil {
			return nil, err
		}
		sources := make([]codeanchor.GoPackageSource, 0, len(members))
		for _, member := range members {
			if member.PackageKey == packageKey {
				sources = append(sources, codeanchor.GoPackageSource{Path: member.Path, Hash: member.Hash, ParseStatus: member.ParseStatus})
			}
		}
		snapshots = append(snapshots, codeanchor.GoPackageSnapshot{
			Package:          key,
			Membership:       members,
			MembershipDigest: codeanchor.GoPackageMembershipDigest(key, members),
			Sources:          sources,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func goPackageKeysForSnapshotTx(ctx context.Context, tx *sql.Tx, sourcePaths []string) ([]string, error) {
	query := `SELECT package_key FROM intel_go_package_files UNION SELECT package_key FROM intel_go_package_relationship_state`
	var args []any
	if sourcePaths = dedupeSortedGoRelationshipStrings(sourcePaths); len(sourcePaths) > 0 {
		directories := make([]string, 0, len(sourcePaths))
		for _, sourcePath := range sourcePaths {
			directories = append(directories, path.Dir(sourcePath))
		}
		directories = dedupeSortedGoRelationshipStrings(directories)
		pathHolders := strings.TrimSuffix(strings.Repeat("?,", len(sourcePaths)), ",")
		directoryHolders := strings.TrimSuffix(strings.Repeat("?,", len(directories)), ",")
		query = `SELECT package_key FROM intel_go_package_files WHERE source_path IN (` + pathHolders + `)
			UNION SELECT package_key FROM intel_go_package_relationship_state WHERE directory IN (` + directoryHolders + `)`
		args = make([]any, 0, len(sourcePaths)+len(directories))
		for i := range sourcePaths {
			args = append(args, sourcePaths[i])
		}
		for i := range directories {
			args = append(args, directories[i])
		}
	}
	query += ` ORDER BY package_key`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func goPackageMembersForDirectoryTx(ctx context.Context, tx *sql.Tx, directory string) ([]codeanchor.GoPackageMember, error) {
	query := `
		SELECT f.path, IFNULL(f.hash, ''), f.parse_status, IFNULL(pf.package_key, '')
		FROM files f LEFT JOIN intel_go_package_files pf ON pf.source_path=f.path
		WHERE f.lang=? AND `
	args := []any{codeanchor.LangGo}
	if directory == "." {
		query += `instr(f.path, '/')=0`
	} else {
		query += `substr(f.path, 1, length(?) + 1)=? || '/'
			AND instr(substr(f.path, length(?) + 2), '/')=0`
		args = append(args, directory, directory, directory)
	}
	query += ` ORDER BY f.path`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []codeanchor.GoPackageMember
	for rows.Next() {
		var member codeanchor.GoPackageMember
		if err := rows.Scan(&member.Path, &member.Hash, &member.ParseStatus, &member.PackageKey); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func replaceGoPackageRelationshipsTx(ctx context.Context, tx *sql.Tx, replacement codeanchor.GoPackageRelationshipReplacement) error {
	packageKey := replacement.Package.StorageKey()
	if strings.TrimSpace(replacement.Package.ImportPath) == "" || strings.TrimSpace(replacement.Package.Directory) == "" || strings.TrimSpace(replacement.Package.PackageName) == "" || strings.TrimSpace(replacement.Package.BuildVariant) == "" {
		return fmt.Errorf("replace Go relationships: incomplete package identity")
	}
	if strings.TrimSpace(replacement.AnalyzerVersion) == "" {
		return fmt.Errorf("replace Go relationships: analyzer version is required")
	}
	persistedDigest, err := goPackageMembershipDigestTx(ctx, tx, replacement.Package)
	if err != nil {
		return err
	}
	if replacement.MembershipDigest == "" || replacement.MembershipDigest != persistedDigest {
		return fmt.Errorf("replace Go relationships: stale package membership for %s", replacement.Package.ImportPath)
	}
	if replacement.Remove {
		_, err := tx.ExecContext(ctx, `DELETE FROM intel_go_package_relationship_state WHERE package_key = ?`, packageKey)
		return err
	}
	diagnostics, err := json.Marshal(replacement.Diagnostics)
	if err != nil {
		return fmt.Errorf("encode Go relationship diagnostics: %w", err)
	}
	complete := 0
	if replacement.Complete {
		complete = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO intel_go_package_relationship_state(
			package_key, import_path, directory, package_name, build_variant,
			membership_digest, analyzer_version, complete, valid, diagnostics_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(package_key) DO UPDATE SET
			import_path=excluded.import_path,
			directory=excluded.directory,
			package_name=excluded.package_name,
			build_variant=excluded.build_variant,
			membership_digest=excluded.membership_digest,
			analyzer_version=excluded.analyzer_version,
			complete=excluded.complete,
			valid=1,
			diagnostics_json=excluded.diagnostics_json,
			updated_at=excluded.updated_at
	`, packageKey, replacement.Package.ImportPath, replacement.Package.Directory, replacement.Package.PackageName, replacement.Package.BuildVariant, replacement.MembershipDigest, replacement.AnalyzerVersion, complete, string(diagnostics), time.Now().Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_go_derived_relationships WHERE package_key = ?`, packageKey); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO intel_go_derived_relationships(package_key, kind, source_path, source_fqn, target_fqn, pointer_only)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, relationship := range replacement.Relationships {
		pointerOnly := 0
		if relationship.PointerOnly {
			pointerOnly = 1
		}
		if _, err := stmt.ExecContext(ctx, packageKey, relationship.Kind, relationship.SourcePath, relationship.SourceFQN, relationship.TargetFQN, pointerOnly); err != nil {
			return err
		}
	}
	return nil
}

func goPackageMembershipDigestTx(ctx context.Context, tx *sql.Tx, key codeanchor.GoPackageKey) (string, error) {
	query := `
		SELECT f.path, IFNULL(f.hash, ''), f.parse_status, IFNULL(pf.package_key, '')
		FROM files f
		LEFT JOIN intel_go_package_files pf ON pf.source_path = f.path
		WHERE f.lang = ? AND `
	args := []any{codeanchor.LangGo}
	if key.Directory == "." {
		query += `instr(f.path, '/') = 0`
	} else {
		query += `substr(f.path, 1, length(?) + 1) = ? || '/'
			AND instr(substr(f.path, length(?) + 2), '/') = 0`
		args = append(args, key.Directory, key.Directory, key.Directory)
	}
	query += ` ORDER BY f.path`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var members []codeanchor.GoPackageMember
	for rows.Next() {
		var member codeanchor.GoPackageMember
		if err := rows.Scan(&member.Path, &member.Hash, &member.ParseStatus, &member.PackageKey); err != nil {
			return "", err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return codeanchor.GoPackageMembershipDigest(key, members), nil
}

// GoDerivedRelationshipsByTargets returns only relationships whose package
// proof is current. Invalidated rows remain unavailable until atomic analysis
// replacement succeeds.
func (s *Store) GoDerivedRelationshipsByTargets(ctx context.Context, kind codeanchor.GoRelationshipKind, targetFQNs []string) (map[string][]codeanchor.GoDerivedRelationship, error) {
	out := make(map[string][]codeanchor.GoDerivedRelationship)
	targetFQNs = dedupeSortedGoRelationshipStrings(targetFQNs)
	if len(targetFQNs) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(targetFQNs)), ",")
	args := make([]any, 0, len(targetFQNs)+2)
	args = append(args, codeanchor.GoRelationshipAnalyzerVersion, kind)
	for _, target := range targetFQNs {
		args = append(args, target)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.kind, r.source_path, r.source_fqn, r.target_fqn, r.pointer_only
		FROM intel_go_derived_relationships r
		JOIN intel_go_package_relationship_state st ON st.package_key = r.package_key AND st.valid = 1 AND st.analyzer_version = ?
		WHERE r.kind = ? AND r.target_fqn IN (`+placeholders+`)
		ORDER BY r.target_fqn, r.source_fqn, r.source_path
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var relationship codeanchor.GoDerivedRelationship
		var pointerOnly int
		if err := rows.Scan(&relationship.Kind, &relationship.SourcePath, &relationship.SourceFQN, &relationship.TargetFQN, &pointerOnly); err != nil {
			return nil, err
		}
		relationship.PointerOnly = pointerOnly != 0
		out[relationship.TargetFQN] = append(out[relationship.TargetFQN], relationship)
	}
	return out, rows.Err()
}

func replaceAndInvalidateGoPackageMembershipTx(ctx context.Context, tx *sql.Tx, summaries []codeanchor.FileSummary, metas []codeanchor.FileMeta) error {
	paths := make([]string, 0, len(summaries)+len(metas))
	for _, summary := range summaries {
		if summary.FilePath != "" {
			paths = append(paths, summary.FilePath)
		}
	}
	for _, meta := range metas {
		if meta.Path != "" {
			paths = append(paths, meta.Path)
		}
	}
	paths = dedupeSortedGoRelationshipStrings(paths)
	if len(paths) == 0 {
		return nil
	}
	oldKeys, err := goPackageKeysForPathsTx(ctx, tx, paths)
	if err != nil {
		return err
	}
	for _, summary := range summaries {
		if _, err := tx.ExecContext(ctx, `DELETE FROM intel_go_package_files WHERE source_path = ?`, summary.FilePath); err != nil {
			return err
		}
		if summary.Lang != codeanchor.LangGo || summary.GoPackage == nil {
			continue
		}
		key := *summary.GoPackage
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO intel_go_package_files(source_path, package_key, import_path, directory, package_name, build_variant)
			VALUES (?, ?, ?, ?, ?, ?)
		`, summary.FilePath, key.StorageKey(), key.ImportPath, key.Directory, key.PackageName, key.BuildVariant); err != nil {
			return err
		}
	}
	newKeys, err := goPackageKeysForPathsTx(ctx, tx, paths)
	if err != nil {
		return err
	}
	for key := range newKeys {
		oldKeys[key] = struct{}{}
	}
	// A newly parse-failed file has no package row. Invalidate every current
	// package in its immediate directory so no method-set proof stays visible.
	for _, meta := range metas {
		if meta.Lang != codeanchor.LangGo || meta.ParseStatus == codeanchor.ParseOK {
			continue
		}
		directory := path.Dir(meta.Path)
		rows, err := tx.QueryContext(ctx, `SELECT package_key FROM intel_go_package_relationship_state WHERE directory = ? AND valid = 1`, directory)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				return err
			}
			oldKeys[key] = struct{}{}
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	if len(oldKeys) == 0 {
		return nil
	}
	keys := make([]string, 0, len(oldKeys))
	for key := range oldKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changed := false
	for _, key := range keys {
		result, err := tx.ExecContext(ctx, `
			UPDATE intel_go_package_relationship_state
			SET valid = 0, complete = 0,
				diagnostics_json = '[{"code":"source_generation_changed"}]',
				updated_at = ?
			WHERE package_key = ? AND valid = 1
		`, time.Now().Unix(), key)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			changed = true
		}
	}
	if changed {
		return incrementGoRelationshipGenerationTx(ctx, tx)
	}
	return nil
}

func goPackageKeysForPathsTx(ctx context.Context, tx *sql.Tx, paths []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if len(paths) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, len(paths))
	for i := range paths {
		args[i] = paths[i]
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT package_key FROM intel_go_package_files WHERE source_path IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = struct{}{}
	}
	return out, rows.Err()
}

func incrementGoRelationshipGenerationTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO index_metadata(key, value) VALUES (?, '1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1
	`, goRelationshipGenerationKey)
	return err
}

func goRelationshipGeneration(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int64, error) {
	var raw string
	err := query.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = ?`, goRelationshipGenerationKey).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	generation, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || generation < 0 {
		return 0, fmt.Errorf("invalid Go relationship generation %q", raw)
	}
	return generation, nil
}

func dedupeSortedGoRelationshipStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
