package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestGoPackageRelationshipReplacementInvalidatesOnSourceChangeAndDeletion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "go-relationships.db")
	store, err := Open(dbPath)
	require.NoError(t, err)

	key := codeanchor.GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: codeanchor.CurrentGoBuildVariant()}
	summary := goRelationshipSummary(key, "dispatch/worker.go", "hash-v1")
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{Summaries: []codeanchor.FileSummary{summary}}))
	digestV1 := codeanchor.GoPackageMembershipDigest(key, []codeanchor.GoPackageMember{{Path: summary.FilePath, Hash: summary.Hash, ParseStatus: codeanchor.ParseOK, PackageKey: key.StorageKey()}})
	relationship := codeanchor.GoDerivedRelationship{Kind: codeanchor.GoRelationshipCalls, SourcePath: summary.FilePath, SourceFQN: key.ImportPath + ".Worker.Sync", TargetFQN: key.ImportPath + ".Queue.Drain"}
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{Package: key, MembershipDigest: digestV1, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: []codeanchor.GoDerivedRelationship{relationship}}}))

	visible, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Equal(t, []codeanchor.GoDerivedRelationship{relationship}, visible[relationship.TargetFQN])
	_, err = store.db.ExecContext(ctx, `UPDATE intel_go_package_relationship_state SET analyzer_version = 'v1' WHERE package_key = ?`, key.StorageKey())
	require.NoError(t, err)
	visible, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Empty(t, visible[relationship.TargetFQN], "proof from an older analyzer must stay hidden until rebuild")
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{Package: key, MembershipDigest: digestV1, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: []codeanchor.GoDerivedRelationship{relationship}}}))
	generationV1, _, err := store.SearchCodeCorpusFingerprint(ctx)
	require.NoError(t, err)

	broken := codeanchor.FileMeta{Path: "dispatch/broken.go", Lang: codeanchor.LangGo, Hash: "broken-hash", ParseStatus: codeanchor.ParseErrored}
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{Metas: []codeanchor.FileMeta{broken}}))
	visible, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Empty(t, visible[relationship.TargetFQN], "a new unclassified parse-failed sibling must invalidate the current proof")
	err = store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{Package: key, MembershipDigest: digestV1, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: []codeanchor.GoDerivedRelationship{relationship}}})
	require.ErrorContains(t, err, "stale package membership")
	require.NoError(t, store.DeleteFile(ctx, broken.Path))
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{Package: key, MembershipDigest: digestV1, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: []codeanchor.GoDerivedRelationship{relationship}}}))

	summary.Hash = "hash-v2"
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{Summaries: []codeanchor.FileSummary{summary}}))
	visible, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Empty(t, visible[relationship.TargetFQN], "source persistence must hide stale package relationships before reanalysis")
	generationV2, _, err := store.SearchCodeCorpusFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, generationV1, generationV2)

	err = store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{Package: key, MembershipDigest: digestV1, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: []codeanchor.GoDerivedRelationship{relationship}}})
	require.ErrorContains(t, err, "stale package membership")
	digestV2 := codeanchor.GoPackageMembershipDigest(key, []codeanchor.GoPackageMember{{Path: summary.FilePath, Hash: summary.Hash, ParseStatus: codeanchor.ParseOK, PackageKey: key.StorageKey()}})
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{Package: key, MembershipDigest: digestV2, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: []codeanchor.GoDerivedRelationship{relationship}}}))

	require.NoError(t, store.DeleteFile(ctx, summary.FilePath))
	visible, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Empty(t, visible[relationship.TargetFQN], "deleting a package member must hide its package relationships")
	emptyDigest := codeanchor.GoPackageMembershipDigest(key, nil)
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{Package: key, MembershipDigest: emptyDigest, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Remove: true}}))
	var stateCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_go_package_relationship_state WHERE package_key = ?`, key.StorageKey()).Scan(&stateCount))
	require.Zero(t, stateCount)
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	visible, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Empty(t, visible[relationship.TargetFQN])
}

func TestGoPackageRelationshipReplacementRollsBackAndIncompleteStateClearsOldProof(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "go-relationships-rollback.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	key := codeanchor.GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: codeanchor.CurrentGoBuildVariant()}
	summary := goRelationshipSummary(key, "dispatch/store.go", "hash-v1")
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{Summaries: []codeanchor.FileSummary{summary}}))
	digest := codeanchor.GoPackageMembershipDigest(key, []codeanchor.GoPackageMember{{Path: summary.FilePath, Hash: summary.Hash, ParseStatus: codeanchor.ParseOK, PackageKey: key.StorageKey()}})
	relationship := codeanchor.GoDerivedRelationship{Kind: codeanchor.GoRelationshipImplements, SourcePath: summary.FilePath, SourceFQN: key.ImportPath + ".MemoryStore", TargetFQN: key.ImportPath + ".Store", PointerOnly: true}
	replacement := codeanchor.GoPackageRelationshipReplacement{Package: key, MembershipDigest: digest, AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: []codeanchor.GoDerivedRelationship{relationship}}
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{replacement}))

	invalid := replacement
	invalid.Relationships = []codeanchor.GoDerivedRelationship{{Kind: "invalid", SourcePath: summary.FilePath, SourceFQN: "bad", TargetFQN: "bad"}}
	require.Error(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{invalid}))
	visible, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipImplements, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Equal(t, []codeanchor.GoDerivedRelationship{relationship}, visible[relationship.TargetFQN], "failed replacement must roll back the prior proof")

	incomplete := replacement
	incomplete.Complete = false
	incomplete.Diagnostics = []codeanchor.GoRelationshipDiagnostic{{Code: "source_parse_untrusted", Path: "dispatch/methods.go"}}
	incomplete.Relationships = nil
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{incomplete}))
	visible, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipImplements, []string{relationship.TargetFQN})
	require.NoError(t, err)
	require.Empty(t, visible[relationship.TargetFQN], "an incomplete current snapshot must remove an old complete implementation proof")
}

func TestGoRelationshipMigrationCreatesValidatedSchema(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "go-relationship-schema.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for _, name := range []string{
		"intel_go_package_relationship_state",
		"intel_go_package_files",
		"intel_go_derived_relationships",
		"idx_intel_go_package_files_package",
		"idx_intel_go_derived_relationship_target",
		"intel_go_package_files_insert_invalidate",
		"intel_go_package_files_delete_invalidate",
	} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count))
		require.Equal(t, 1, count, name)
	}
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error { return store.validateIntelSchemaTx(ctx, tx) }))
}

func TestGoRelationshipSchemaDriftRecoversInsteadOfLeavingInvalidationDisabled(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "go-relationship-trigger-drift.db")
	store, err := Open(dbPath)
	require.NoError(t, err)

	key := codeanchor.GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: codeanchor.CurrentGoBuildVariant()}
	summary := goRelationshipSummary(key, "dispatch/worker.go", "hash-v1")
	summary.Symbols = []codeanchor.Symbol{{Lang: codeanchor.LangGo, Kind: codeanchor.SymStruct, File: summary.FilePath, Pkg: key.ImportPath, Name: "Worker", FQN: key.ImportPath + ".Worker"}}
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{Summaries: []codeanchor.FileSummary{summary}}))

	_, err = store.db.ExecContext(ctx, `
		DROP TRIGGER intel_go_package_files_update_invalidate;
		CREATE TRIGGER intel_go_package_files_update_invalidate
		AFTER UPDATE ON intel_go_package_files BEGIN SELECT 1; END;
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var triggerSQL string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name='intel_go_package_files_update_invalidate'`).Scan(&triggerSQL))
	require.Equal(t, normalizeGoRelationshipSchemaSQL(goRelationshipSchemaSQL(t, "trigger", "intel_go_package_files_update_invalidate")), normalizeGoRelationshipSchemaSQL(triggerSQL))
	var staleSymbols int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbols WHERE fqn = ?`, key.ImportPath+".Worker").Scan(&staleSymbols))
	require.Zero(t, staleSymbols, "current-schema drift follows the Intel domain recovery policy")
}

func TestOpenUpgradesV67ToGoRelationshipsWithoutLosingParserFacts(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "go-relationship-v67.db")
	store, err := Open(dbPath)
	require.NoError(t, err)

	file := "dispatch/worker.go"
	fqn := "example.com/app/dispatch.Worker"
	summary := codeanchor.FileSummary{
		FilePath:    file,
		Lang:        codeanchor.LangGo,
		Hash:        "hash-v1",
		ParseStatus: codeanchor.ParseOK,
		Symbols: []codeanchor.Symbol{{
			Lang: codeanchor.LangGo, Kind: codeanchor.SymStruct, File: file,
			Pkg: "example.com/app/dispatch", Name: "Worker", FQN: fqn,
		}},
	}
	require.NoError(t, store.ReplaceFileSummariesBatch(ctx, []codeanchor.FileSummary{summary}))
	_, err = store.db.ExecContext(ctx, `
		DROP TABLE intel_go_derived_relationships;
		DROP TABLE intel_go_package_files;
		DROP TABLE intel_go_package_relationship_state;
		UPDATE schema_version SET version=67;
		UPDATE rzm_migration_state SET version=67 WHERE domain='intel';
		DELETE FROM rzm_migration_log WHERE domain='intel' AND from_version>=67;
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var gotPath, gotHash string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT s.file, f.hash FROM symbols s JOIN files f ON f.path=s.file WHERE s.fqn=?
	`, fqn).Scan(&gotPath, &gotHash))
	require.Equal(t, file, gotPath)
	require.Equal(t, summary.Hash, gotHash)
	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain='intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error { return validateGoRelationshipSchema(ctx, tx) }))
}

func goRelationshipSchemaSQL(t *testing.T, kind, name string) string {
	t.Helper()
	for _, object := range goRelationshipSchemaObjects {
		if object.kind == kind && object.name == name {
			return object.sql
		}
	}
	t.Fatalf("missing Go relationship schema object %s %s", kind, name)
	return ""
}

func goRelationshipSummary(key codeanchor.GoPackageKey, sourcePath, hash string) codeanchor.FileSummary {
	return codeanchor.FileSummary{FilePath: sourcePath, Lang: codeanchor.LangGo, Hash: hash, ParseStatus: codeanchor.ParseOK, GoPackage: &key}
}
