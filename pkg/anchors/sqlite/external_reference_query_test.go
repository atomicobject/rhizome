package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestExternalReferencesExactHandleReturnsBoundedStableGroups(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target := mustExternalTarget(t, "react", "useState")
	refs := []codeanchor.SymbolRefRow{
		{SrcPath: "src/z.ts", OwnerFQN: "src.Z", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
		{SrcPath: "src/a.ts", OwnerFQN: "src.A", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
		{SrcPath: "src/a.ts", OwnerFQN: "src.A", RefKind: codeanchor.RefKindTypeRef, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
		{SrcPath: "src/a.ts", OwnerFQN: "src.A", RefKind: codeanchor.RefKindMemberRef, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
	}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/z.ts": {refs[0]}, "src/a.ts": refs[1:]}))
	inputs := make([]codeanchor.ExternalSymbolEvidenceInput, 0, len(refs))
	for _, ref := range refs {
		inputs = append(inputs, codeanchor.ExternalSymbolEvidenceInput{OwnerFQN: ref.OwnerFQN, RefKind: ref.RefKind,
			Raw: codeanchor.RawSymbolTargetKey{DstLang: ref.DstLang, DstPkg: ref.DstPkg, DstName: ref.DstName, DstFQN: ref.DstFQN}, Target: target,
			Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh, ImportedName: "useState", LocalName: "useState",
				Version: &codeanchor.ExternalVersionProvenance{ManifestPath: "package.json", DeclaredRange: "^19", Scope: codeanchor.ExternalVersionDependency}}})
	}
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		"src/a.ts": {Symbols: inputs[1:], Imports: []codeanchor.ExternalImportEvidenceInput{{Module: "react", BindingOrdinal: 0, Target: target, Evidence: inputs[1].Evidence}}},
		"src/z.ts": {Symbols: inputs[:1]},
	}))

	result, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, "resolved", result.Status)
	require.NotNil(t, result.Target)
	require.True(t, result.Target.External)
	require.True(t, result.Target.Pathless)
	require.False(t, result.Target.Indexed)
	require.False(t, result.Target.SourceBacked)
	require.False(t, result.Target.SourceAvailable)
	require.Len(t, result.Calls, 1)
	require.Equal(t, "src/a.ts", result.Calls[0].Path)
	require.Len(t, result.Types, 1)
	require.Len(t, result.Members, 1)
	require.Len(t, result.Imports, 1)
	require.True(t, result.Truncated)
	require.Equal(t, "^19", result.Calls[0].DeclaredRange)
}

func TestExternalReferencesUsesOneSnapshotAcrossConcurrentDeletion(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "intel.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	defer store.Close()
	target := mustExternalTarget(t, "react", "useState")
	ref := codeanchor.SymbolRefRow{SrcPath: "src/app.ts", OwnerFQN: "src.App", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/app.ts": {ref}}))
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{"src/app.ts": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
		OwnerFQN: ref.OwnerFQN, RefKind: ref.RefKind, Raw: codeanchor.RawSymbolTargetKey{DstLang: ref.DstLang, DstPkg: ref.DstPkg, DstName: ref.DstName, DstFQN: ref.DstFQN}, Target: target,
		Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh},
	}}}}))

	writer, err := Open(dbPath)
	require.NoError(t, err)
	defer writer.Close()
	store.db.SetMaxOpenConns(1)
	conn, err := store.db.Conn(ctx)
	require.NoError(t, err)
	var writeErr error
	intercepted := false
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, name, _ string, database string) int {
			if op == sqlite3.SQLITE_READ && name == "intel_external_symbol_evidence" && database == "main" && !intercepted {
				intercepted = true
				writeErr = writer.DeleteFile(ctx, "src/app.ts")
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())
	result, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle})
	require.NoError(t, err)
	require.True(t, intercepted)
	require.NoError(t, writeErr)
	require.Equal(t, "resolved", result.Status)
	require.Len(t, result.Calls, 1, "the target and groups must come from the same pre-deletion snapshot")
	after, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle})
	require.NoError(t, err)
	require.Equal(t, "not_found", after.Status)
}

func BenchmarkExternalReferencesFanIn1000(b *testing.B) {
	ctx := context.Background()
	store, err := Open(filepath.Join(b.TempDir(), "intel.db"))
	require.NoError(b, err)
	defer store.Close()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: "npm", Module: "react", SymbolPath: "useState", Kind: codeanchor.ExternalTargetSymbol})
	require.NoError(b, err)
	refs := make(map[string][]codeanchor.SymbolRefRow, 1000)
	evidence := make(map[string]codeanchor.ExternalEvidenceBatch, 1000)
	for i := 0; i < 1000; i++ {
		path := fmt.Sprintf("src/component_%04d.ts", i)
		owner := fmt.Sprintf("src.Component%04d", i)
		ref := codeanchor.SymbolRefRow{SrcPath: path, OwnerFQN: owner, RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}
		refs[path] = []codeanchor.SymbolRefRow{ref}
		evidence[path] = codeanchor.ExternalEvidenceBatch{Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
			OwnerFQN: owner, RefKind: ref.RefKind, Raw: codeanchor.RawSymbolTargetKey{DstLang: ref.DstLang, DstPkg: ref.DstPkg, DstName: ref.DstName, DstFQN: ref.DstFQN}, Target: target,
			Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh},
		}}}
	}
	require.NoError(b, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, refs))
	require.NoError(b, store.ReplaceExternalEvidenceForPathsBatch(ctx, evidence))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, queryErr := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle, Limit: 1000})
		require.NoError(b, queryErr)
		require.Len(b, result.Calls, 100)
		require.True(b, result.Truncated)
	}
}

func TestExternalReferencesStructuredCandidatesAreExplicitlyAmbiguous(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	for ordinal, symbol := range []string{"useEffect", "useState"} {
		target := mustExternalTarget(t, "react", symbol)
		require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
			"src/imports" + symbol + ".ts": {Imports: []codeanchor.ExternalImportEvidenceInput{{Module: "react", BindingOrdinal: ordinal, Target: target,
				Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh, ImportedName: symbol, LocalName: symbol}}}},
		}))
	}
	result, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Ecosystem: "npm", Module: "react", SymbolPrefix: "use", Limit: 20})
	require.NoError(t, err)
	require.Equal(t, "ambiguous", result.Status)
	require.Nil(t, result.Target)
	require.Len(t, result.Candidates, 2)
	require.Less(t, result.Candidates[0].Handle, result.Candidates[1].Handle)
	require.Empty(t, result.Calls)
}

func TestExternalReferencesStructuredPrefixUsesBinaryRange(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	for ordinal, symbol := range []string{"useEffect", "useState", "usage", "éclair", "école", "être"} {
		target := mustExternalTarget(t, "react", symbol)
		require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
			fmt.Sprintf("src/import-%d.ts", ordinal): {Imports: []codeanchor.ExternalImportEvidenceInput{{Module: "react", BindingOrdinal: ordinal, Target: target,
				Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh}}}},
		}))
	}

	result, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Ecosystem: "npm", Module: "react", SymbolPrefix: "use"})
	require.NoError(t, err)
	require.Equal(t, "ambiguous", result.Status)
	require.Equal(t, []string{"useEffect", "useState"}, []string{result.Candidates[0].SymbolPath, result.Candidates[1].SymbolPath})

	result, err = store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Ecosystem: "npm", Module: "react", SymbolPrefix: "é"})
	require.NoError(t, err)
	require.Equal(t, "ambiguous", result.Status)
	require.Equal(t, []string{"éclair", "école"}, []string{result.Candidates[0].SymbolPath, result.Candidates[1].SymbolPath})

	result, err = store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Ecosystem: "npm", Module: "react"})
	require.NoError(t, err)
	require.Equal(t, "ambiguous", result.Status)
	require.Len(t, result.Candidates, 6, "an empty prefix must retain the all-symbol lookup")
}

func TestExternalReferencePrefixQueryUsesIdentityIndex(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	recording := &recordingExternalQueryer{db: store.db}
	_, _, err = externalReferenceCandidates(ctx, recording, codeanchor.ExternalReferenceQuery{
		Ecosystem: "npm", Module: "react", SymbolPrefix: "use",
	}, 20)
	require.NoError(t, err)
	rows, err := store.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+recording.query, recording.args...)
	require.NoError(t, err)
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		details = append(details, detail)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(details, "\n"), "symbol_path>? AND symbol_path<?")
}

type recordingExternalQueryer struct {
	db    *sql.DB
	query string
	args  []any
}

func (r *recordingExternalQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.query, r.args = query, args
	return r.db.QueryContext(ctx, query, args...)
}

func TestExternalReferencesRejectsMixedOrIncompleteLookup(t *testing.T) {
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	_, err = store.ExternalReferences(context.Background(), codeanchor.ExternalReferenceQuery{Handle: "h", Ecosystem: "npm", Module: "react"})
	require.ErrorContains(t, err, "exactly one lookup mode")
	_, err = store.ExternalReferences(context.Background(), codeanchor.ExternalReferenceQuery{Ecosystem: "npm"})
	require.ErrorContains(t, err, "ecosystem and module")
}

func TestExternalReferencesSuppressesStaleEvidenceAfterLocalDefinitionsArrive(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target := mustExternalTarget(t, "react", "useState")
	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{FilePath: "src/app.ts", Lang: codeanchor.LangTS, Hash: "source", ParseStatus: codeanchor.ParseOK}))
	ref := codeanchor.SymbolRefRow{SrcPath: "src/app.ts", OwnerFQN: "src.App", RefKind: codeanchor.RefKindCalls, DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"src/app.ts": {ref}}))
	evidence := codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh}
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{"src/app.ts": {
		Symbols: []codeanchor.ExternalSymbolEvidenceInput{{OwnerFQN: ref.OwnerFQN, RefKind: ref.RefKind,
			Raw: codeanchor.RawSymbolTargetKey{DstLang: ref.DstLang, DstPkg: ref.DstPkg, DstName: ref.DstName, DstFQN: ref.DstFQN}, Target: target, Evidence: evidence}},
		Imports: []codeanchor.ExternalImportEvidenceInput{{Module: "react", BindingOrdinal: 0, Target: target, Evidence: evidence}},
	}}))

	before, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle})
	require.NoError(t, err)
	require.Len(t, before.Calls, 1)
	require.Len(t, before.Imports, 1)
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{"vendor/react.py": {{SrcPath: "vendor/react.py", Lang: codeanchor.LangPy, Module: "react"}}}))
	crossLanguage, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle})
	require.NoError(t, err)
	require.Len(t, crossLanguage.Imports, 1, "a same-named module in another language must not suppress the external import")
	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{FilePath: "vendor/react.ts", Lang: codeanchor.LangTS, Hash: "local", ParseStatus: codeanchor.ParseOK,
		Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangTS, Kind: codeanchor.SymFunc, File: "vendor/react.ts", Pkg: "react", Name: "useState", FQN: "react.useState"}}}))
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{"vendor/react.ts": {{SrcPath: "vendor/react.ts", Lang: codeanchor.LangTS, Module: "react"}}}))

	after, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle})
	require.NoError(t, err)
	require.Empty(t, after.Calls)
	require.Empty(t, after.Imports)
}

func TestExternalReferencesCanonicalizesStructuredNodeModuleAlias(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	target := mustExternalTarget(t, "node:path", "join")
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{"src/app.ts": {Imports: []codeanchor.ExternalImportEvidenceInput{{
		Module: "node:path", BindingOrdinal: 0, Target: target, Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh},
	}}}}))
	result, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Ecosystem: "npm", Module: "path", SymbolPrefix: "join"})
	require.NoError(t, err)
	require.Equal(t, "resolved", result.Status)
	require.Equal(t, "node:path", result.Target.Module)
}
