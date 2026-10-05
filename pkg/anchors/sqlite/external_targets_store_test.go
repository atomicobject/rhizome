package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestExternalTargetSchemaMigratesV58ToCurrent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "intel.db")
	store, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 58)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer store.Close()

	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
	for _, table := range []string{
		"intel_external_targets",
		"intel_external_target_map",
		"intel_external_symbol_evidence",
		"intel_external_import_evidence",
	} {
		var found string
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found))
		require.Equal(t, table, found)
	}
	for _, redundant := range []string{"idx_intel_external_targets_identity", "idx_intel_external_import_evidence_source"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, redundant).Scan(&count))
		require.Zero(t, count, redundant)
	}
	for _, table := range []string{"intel_external_symbol_evidence", "intel_external_import_evidence"} {
		var ddl string
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl))
		require.Contains(t, ddl, "evidence_kind IN")
		require.Contains(t, ddl, "version_scope IN")
	}
}

func TestOpenUpgradesV68WithoutLosingExternalSymbolEvidence(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "external-evidence-v68.db")
	store, err := openWithOptionsAtSchemaVersion(dbPath, OpenOptions{}, 68)
	require.NoError(t, err)

	path := "src/component.ts"
	ref := codeanchor.SymbolRefRow{
		SrcPath: path, OwnerFQN: "src.Component", RefKind: codeanchor.RefKindCalls,
		DstLang: codeanchor.LangTS, DstPkg: "react", DstName: "useState", DstFQN: "react.useState",
	}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{path: {ref}}))
	target := mustExternalTarget(t, "react", "useState")
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		path: {Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
			OwnerFQN: ref.OwnerFQN, RefKind: ref.RefKind,
			Raw:    codeanchor.RawSymbolTargetKey{DstLang: ref.DstLang, DstPkg: ref.DstPkg, DstName: ref.DstName, DstFQN: ref.DstFQN},
			Target: target,
			Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh,
				ImportedName: "useState", LocalName: "useState"},
		}}},
	}))
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	for table, want := range map[string]int{
		"intel_external_target_map":      1,
		"intel_external_symbol_evidence": 1,
		"intel_symbol_refs":              1,
		"intel_symbol_ref_targets":       1,
	} {
		var got int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got))
		require.Equal(t, want, got, table)
	}
	classified, err := store.ExternalReferenceClassificationsForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, classified, 1)
	require.Equal(t, target.Handle, classified[0].Target.Handle)
}

func TestReplaceExternalEvidenceUsesNaturalKeysAndReusesCatalogTarget(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
		Ecosystem:  codeanchor.ExternalEcosystemNPM,
		Module:     "react",
		SymbolPath: "useState",
		Kind:       codeanchor.ExternalTargetSymbol,
	})
	require.NoError(t, err)

	refs := map[string][]codeanchor.SymbolRefRow{
		"src/a.ts": {{SrcPath: "src/a.ts", OwnerFQN: "src.a.Component", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}},
		"src/b.ts": {{SrcPath: "src/b.ts", OwnerFQN: "src.b.Component", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}},
	}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, refs))
	batch := func(owner string) codeanchor.ExternalEvidenceBatch {
		return codeanchor.ExternalEvidenceBatch{Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
			OwnerFQN: owner,
			RefKind:  codeanchor.RefKindCalls,
			Raw:      codeanchor.RawSymbolTargetKey{DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
			Target:   target,
			Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh, ImportedName: "useState", LocalName: "useState"},
		}}}
	}
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": batch("src.a.Component"),
		"src/b.ts": batch("src.b.Component"),
	}))

	var catalog, mappings, evidence int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_targets`).Scan(&catalog))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_target_map`).Scan(&mappings))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_symbol_evidence`).Scan(&evidence))
	require.Equal(t, 1, catalog)
	require.Equal(t, 1, mappings)
	require.Equal(t, 2, evidence)
}

func TestReplaceExternalEvidenceMatchesTopLevelReferenceWithEmptyOwner(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "top-level-external.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	target := mustExternalTarget(t, "react", "createElement")
	path := "src/bootstrap.ts"
	ref := codeanchor.SymbolRefRow{
		SrcPath: path, OwnerFQN: "", RefKind: codeanchor.RefKindCalls,
		DstLang: codeanchor.LangTS, DstPkg: "react", DstName: "createElement", DstFQN: "react.createElement",
	}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{path: {ref}}))
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		path: {Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
			OwnerFQN: "", RefKind: ref.RefKind,
			Raw:      codeanchor.RawSymbolTargetKey{DstLang: ref.DstLang, DstPkg: ref.DstPkg, DstName: ref.DstName, DstFQN: ref.DstFQN},
			Target:   target,
			Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh, ImportedName: "createElement", LocalName: "createElement"},
		}}},
	}))

	rows, err := store.ExternalReferenceClassificationsForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].Association.OwnerFQN)
	require.Equal(t, target.Handle, rows[0].Target.Handle)
}

func TestReplaceExternalEvidenceRejectsUnmatchedRawAssociationWithoutOrphans(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target := mustExternalTarget(t, "react", "useState")
	err = store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/missing.ts": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{externalSymbolInput("src.Component", target)}},
	})
	require.ErrorContains(t, err, "unmatched durable symbol-reference associations")
	for _, table := range []string{"intel_symbol_ref_targets", "intel_external_targets", "intel_external_target_map", "intel_external_symbol_evidence"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestReplaceExternalEvidenceRejectsNonRelativeSourceAndManifestPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", Kind: codeanchor.ExternalTargetModule})
	require.NoError(t, err)
	base := codeanchor.ExternalEvidenceBatch{Imports: []codeanchor.ExternalImportEvidenceInput{{
		Module: "react", BindingOrdinal: 0, Target: target,
		Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMSideEffect, Confidence: codeanchor.ExternalConfidenceHigh},
	}}}
	for _, badPath := range []string{"/abs/src.ts", "../src.ts", "nested/../../src.ts", `C:\\src.ts`} {
		err = store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{badPath: base})
		require.ErrorContains(t, err, "vault-root-relative", badPath)
	}
	for _, badManifest := range []string{"/abs/package.json", "../package.json", "nested/../../package.json", `C:\\package.json`} {
		batch := base
		batch.Imports = append([]codeanchor.ExternalImportEvidenceInput(nil), base.Imports...)
		batch.Imports[0].Evidence.Version = &codeanchor.ExternalVersionProvenance{ManifestPath: badManifest, DeclaredRange: "^19", Scope: codeanchor.ExternalVersionDependency}
		err = store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{"src/a.ts": batch})
		require.ErrorContains(t, err, "vault-root-relative", badManifest)
	}
}

func TestReplaceExternalEvidenceRejectsInvalidEvidenceBeforeWriting(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", Kind: codeanchor.ExternalTargetModule})
	require.NoError(t, err)
	err = store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": {Imports: []codeanchor.ExternalImportEvidenceInput{{Module: "react", Target: target, Evidence: codeanchor.ExternalEvidence{Kind: "invented", Confidence: codeanchor.ExternalConfidenceHigh}}}},
	})
	require.ErrorContains(t, err, "unsupported external evidence kind")
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_targets`).Scan(&count))
	require.Zero(t, count)
}

func TestExternalEvidenceSchemaRejectsInvalidVocabulary(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_external_targets(handle, ecosystem, module, symbol_path, target_kind) VALUES ('h', 'npm', 'react', '', 'module')`)
	require.NoError(t, err)
	for _, columnValue := range []struct{ column, value string }{{"evidence_kind", "invented"}, {"version_scope", "invented"}} {
		_, err = store.db.ExecContext(ctx, `INSERT INTO intel_external_import_evidence(src_path,module,binding_ordinal,external_id,evidence_kind,confidence,version_scope) VALUES ('src/a.ts','react',0,1,'esm_side_effect','high','')`)
		require.NoError(t, err)
		_, err = store.db.ExecContext(ctx, `UPDATE intel_external_import_evidence SET `+columnValue.column+`=?`, columnValue.value)
		require.Error(t, err, columnValue.column)
		_, err = store.db.ExecContext(ctx, `DELETE FROM intel_external_import_evidence`)
		require.NoError(t, err)
	}
}

func TestReplaceExternalEvidenceRejectsConflictingRawTargetMapping(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	ref := codeanchor.SymbolRefRow{SrcPath: "src/a.ts", OwnerFQN: "src.Component", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/a.ts": {ref}}))
	react := mustExternalTarget(t, "react", "useState")
	preact := mustExternalTarget(t, "preact/hooks", "useState")
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{externalSymbolInput("src.Component", react)}},
	}))
	err = store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{externalSymbolInput("src.Component", preact)}},
	})
	require.ErrorContains(t, err, "existing raw-target mappings")
	var handle string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT e.handle FROM intel_external_target_map m JOIN intel_external_targets e ON e.external_id=m.external_id`).Scan(&handle))
	require.Equal(t, react.Handle, handle)
}

func TestReplaceExternalEvidenceRejectsIntraBatchRawTargetConflict(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	refs := []codeanchor.SymbolRefRow{
		{SrcPath: "src/a.ts", OwnerFQN: "src.A", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
		{SrcPath: "src/a.ts", OwnerFQN: "src.B", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
	}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/a.ts": refs}))
	react := externalSymbolInput("src.A", mustExternalTarget(t, "react", "useState"))
	preact := externalSymbolInput("src.B", mustExternalTarget(t, "preact/hooks", "useState"))
	err = store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{react, preact}},
	})
	require.ErrorContains(t, err, "conflicting raw-target mappings")
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_targets`).Scan(&count))
	require.Zero(t, count)
}

func TestReplaceExternalEvidenceSeparatesImportsAndReplacesVersionProvenance(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", Kind: codeanchor.ExternalTargetModule})
	require.NoError(t, err)
	write := func(declaredRange string) {
		require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
			"src/a.ts": {Imports: []codeanchor.ExternalImportEvidenceInput{{
				Module: "react", BindingOrdinal: 0, Target: target,
				Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMSideEffect, Confidence: codeanchor.ExternalConfidenceHigh, Version: &codeanchor.ExternalVersionProvenance{ManifestPath: "package.json", DeclaredRange: declaredRange, Scope: codeanchor.ExternalVersionDependency}},
			}}},
		}))
	}
	write("^18.0.0")
	write("^19.0.0")
	imports, err := store.ExternalImportEvidenceForPath(ctx, "src/a.ts")
	require.NoError(t, err)
	require.Len(t, imports, 1)
	require.NotNil(t, imports[0].Evidence.Version)
	require.Equal(t, "^19.0.0", imports[0].Evidence.Version.DeclaredRange)
	var localImports int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_import_refs`).Scan(&localImports))
	require.Zero(t, localImports)
}

func TestExternalImportEvidencePersistsCJSNamedBinding(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target := mustExternalTarget(t, "react", "createElement")
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/common.cjs": {Imports: []codeanchor.ExternalImportEvidenceInput{{
			Module: "react", BindingOrdinal: 1, Target: target,
			Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceCJSNamed, Confidence: codeanchor.ExternalConfidenceHigh, ImportedName: "createElement", LocalName: "h"},
		}}},
	}))
	rows, err := store.ExternalImportEvidenceForPath(ctx, "src/common.cjs")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, codeanchor.ExternalEvidenceCJSNamed, rows[0].Evidence.Kind)
	require.Equal(t, "createElement", rows[0].Evidence.ImportedName)
	require.Equal(t, "h", rows[0].Evidence.LocalName)
	require.Equal(t, 1, rows[0].Association.BindingOrdinal)
}

func TestExternalVersionAndAliasEvidenceRemainAssociationScoped(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target := mustExternalTarget(t, "react", "useState")
	refs := map[string][]codeanchor.SymbolRefRow{}
	batches := map[string]codeanchor.ExternalEvidenceBatch{}
	for _, item := range []struct {
		path, owner, alias, declaredRange string
	}{
		{path: "packages/legacy/a.ts", owner: "legacy.Component", alias: "legacyState", declaredRange: "^18.0.0"},
		{path: "packages/current/b.ts", owner: "current.Component", alias: "state", declaredRange: "^19.0.0"},
	} {
		refs[item.path] = []codeanchor.SymbolRefRow{{SrcPath: item.path, OwnerFQN: item.owner, RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}}
		input := externalSymbolInput(item.owner, target)
		input.Evidence.ImportedName = "useState"
		input.Evidence.LocalName = item.alias
		input.Evidence.Version = &codeanchor.ExternalVersionProvenance{ManifestPath: filepath.ToSlash(filepath.Join(filepath.Dir(item.path), "package.json")), DeclaredRange: item.declaredRange, Scope: codeanchor.ExternalVersionDependency}
		batches[item.path] = codeanchor.ExternalEvidenceBatch{Symbols: []codeanchor.ExternalSymbolEvidenceInput{input}}
	}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, refs))
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, batches))

	catalog, err := store.ExternalTargets(ctx)
	require.NoError(t, err)
	require.Len(t, catalog, 1)
	for path, want := range map[string]struct{ alias, declaredRange string }{
		"packages/legacy/a.ts":  {alias: "legacyState", declaredRange: "^18.0.0"},
		"packages/current/b.ts": {alias: "state", declaredRange: "^19.0.0"},
	} {
		rows, readErr := store.ExternalReferenceClassificationsForPath(ctx, path)
		require.NoError(t, readErr)
		require.Len(t, rows, 1)
		require.Equal(t, catalog[0].ID, rows[0].Target.ID)
		require.Equal(t, want.alias, rows[0].Evidence.LocalName)
		require.NotNil(t, rows[0].Evidence.Version)
		require.Equal(t, want.declaredRange, rows[0].Evidence.Version.DeclaredRange)
	}
}

func TestExternalTablesAreAbsentFromSourceBackedSurfaces(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", Kind: codeanchor.ExternalTargetModule})
	require.NoError(t, err)
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": {Imports: []codeanchor.ExternalImportEvidenceInput{{Module: "react", BindingOrdinal: 0, Target: target, Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMSideEffect, Confidence: codeanchor.ExternalConfidenceHigh}}}},
	}))
	var externalCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_import_evidence`).Scan(&externalCount))
	require.Equal(t, 1, externalCount)
	for _, table := range []string{"intel_code_anchors", "intel_chunks", "intel_edges", "ontology_nodes"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count)
	}
}

func TestExternalClassificationTracksLocalDefinitionWithoutEvidenceRewrite(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	refs := []codeanchor.SymbolRefRow{
		{SrcPath: "src/a.ts", OwnerFQN: "src.Component", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
		{SrcPath: "src/a.ts", OwnerFQN: "src.Component", RefKind: codeanchor.RefKindTypeRef, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
		{SrcPath: "src/a.ts", OwnerFQN: "src.Component", RefKind: codeanchor.RefKindMemberRef, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
	}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/a.ts": refs}))
	target := mustExternalTarget(t, "react", "useState")
	call := externalSymbolInput("src.Component", target)
	typeRef := externalSymbolInput("src.Component", target)
	typeRef.RefKind = codeanchor.RefKindTypeRef
	memberRef := externalSymbolInput("src.Component", target)
	memberRef.RefKind = codeanchor.RefKindMemberRef
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{call, typeRef, memberRef}},
	}))
	assertClass := func(want codeanchor.ExternalReferenceClass) {
		rows, readErr := store.ExternalReferenceClassificationsForPath(ctx, "src/a.ts")
		require.NoError(t, readErr)
		require.Len(t, rows, 3)
		kinds := make([]codeanchor.RefKind, 0, len(rows))
		for _, row := range rows {
			require.Equal(t, want, row.Class)
			kinds = append(kinds, row.Association.RefKind)
		}
		require.ElementsMatch(t, []codeanchor.RefKind{codeanchor.RefKindCalls, codeanchor.RefKindTypeRef, codeanchor.RefKindMemberRef}, kinds)
	}
	assertClass(codeanchor.ExternalClassExternalClassified)
	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
		FilePath: "src/local.ts", Lang: codeanchor.LangTS, Hash: "local-hash", ParseStatus: codeanchor.ParseOK,
		Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangTS, Kind: codeanchor.SymFunc, File: "src/local.ts", Pkg: "react", Name: "useState", FQN: "react.useState"}},
	}))
	assertClass(codeanchor.ExternalClassLocalResolved)
	require.NoError(t, store.DeleteFile(ctx, "src/local.ts"))
	assertClass(codeanchor.ExternalClassExternalClassified)
	var evidenceCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_symbol_evidence`).Scan(&evidenceCount))
	require.Equal(t, 3, evidenceCount)
}

func TestExternalEvidenceLifecycleReplaceDeletePurgeAndReopen(t *testing.T) {
	ctx := context.Background()
	t.Run("replacement and reopen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "intel.db")
		store, err := Open(path)
		require.NoError(t, err)
		seedExternalSymbolEvidence(t, ctx, store, "src/a.ts")
		require.NoError(t, store.Close())
		store, err = Open(path)
		require.NoError(t, err)
		defer store.Close()
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_symbol_evidence`).Scan(&count))
		require.Equal(t, 1, count)
		require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/a.ts": nil}))
		assertExternalCatalogEmpty(t, ctx, store)
	})

	t.Run("delete file", func(t *testing.T) {
		store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
		require.NoError(t, err)
		defer store.Close()
		seedExternalSymbolEvidence(t, ctx, store, "src/a.ts")
		require.NoError(t, store.DeleteFile(ctx, "src/a.ts"))
		assertExternalCatalogEmpty(t, ctx, store)
	})

	t.Run("purge import only", func(t *testing.T) {
		store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
		require.NoError(t, err)
		defer store.Close()
		target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", Kind: codeanchor.ExternalTargetModule})
		require.NoError(t, err)
		require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
			"src/import-only.ts": {Imports: []codeanchor.ExternalImportEvidenceInput{{Module: "react", BindingOrdinal: 0, Target: target, Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMSideEffect, Confidence: codeanchor.ExternalConfidenceHigh}}}},
		}))
		require.NoError(t, store.PurgeIntelCodeNotInPaths(ctx, nil, true))
		assertExternalCatalogEmpty(t, ctx, store)
	})
}

func mustExternalTarget(t *testing.T, module, symbol string) codeanchor.ExternalTarget {
	t.Helper()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: module, SymbolPath: symbol, Kind: codeanchor.ExternalTargetSymbol})
	require.NoError(t, err)
	return target
}

func externalSymbolInput(owner string, target codeanchor.ExternalTarget) codeanchor.ExternalSymbolEvidenceInput {
	return codeanchor.ExternalSymbolEvidenceInput{
		OwnerFQN: owner, RefKind: codeanchor.RefKindCalls,
		Raw:      codeanchor.RawSymbolTargetKey{DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
		Target:   target,
		Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh},
	}
}

func seedExternalSymbolEvidence(t *testing.T, ctx context.Context, store *Store, path string) {
	t.Helper()
	ref := codeanchor.SymbolRefRow{SrcPath: path, OwnerFQN: "src.Component", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{path: {ref}}))
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		path: {Symbols: []codeanchor.ExternalSymbolEvidenceInput{externalSymbolInput("src.Component", mustExternalTarget(t, "react", "useState"))}},
	}))
}

func assertExternalCatalogEmpty(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	for _, table := range []string{"intel_external_symbol_evidence", "intel_external_import_evidence", "intel_external_target_map", "intel_external_targets"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count, table)
	}
}
