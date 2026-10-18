package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"testing"
)

func readinessFixture(t *testing.T) (*Store, OntologyDelta) {
	t.Helper()
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "readiness.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	metadata := NoteMetadataState{NotesHash: "notes-v1", RawNotesHash: "raw-v1", LoadedAt: 10, Ready: true}
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: metadata,
		Notes: []NoteMetadataRow{{Path: "note.md", Title: "Source", ContentHash: "source-v1", FormatID: "markdown",
			Projection: NoteProjectionState{Status: NoteProjectionStatusCurrent, ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", SourceContentHash: "source-v1", UpdatedAt: 10}}},
		PropertyValues: []NotePropertyValueRow{{NotePath: "note.md", PropertyName: "status", Source: NotePropertySourceFrontmatter,
			ValueText: "ready", ValueNorm: "ready", ValueKind: NotePropertyValueString}},
		Tags:               []NoteTagRow{{NotePath: "note.md", TagNorm: "spec"}},
		FragmentTargets:    []NoteFragmentTargetRow{{NotePath: "note.md", Kind: NoteFragmentTargetHeading, Target: "Heading", TargetNorm: "heading", Ordinal: 1}},
		LinkInputs:         []NoteLinkInputRow{{NotePath: "note.md", LookupKey: "other"}},
		ProjectionPayloads: []NoteProjectionPayloadRow{{NotePath: "note.md", JSON: `{"source":"v1"}`}},
		ProjectionDiagnostics: []NoteProjectionDiagnosticRow{{NotePath: "note.md", Code: "fixture", Category: "fixture", Message: "Fixture",
			AffectedOperation: "projection"}},
		WikilinkEdges: []GraphDocEdgeRow{{SrcPath: "note.md", DstPath: "other.md", Kind: "wikilink"}},
	}))
	state := OntologySchemaState{SchemaHash: "schema-v1", NotesHash: metadata.NotesHash, MaterializationVersion: 1, LoadedAt: 10, Ready: true, ErrorJSON: "[]"}
	delta := OntologyDelta{
		SchemaState:  &state,
		Assessments:  []OntologyNoteAssessmentRow{{NotePath: "note.md", DeclaredType: "Spec", ResolvedType: "Spec", AssessmentJSON: `{"notePath":"note.md"}`, SchemaHash: "schema-v1", UpdatedAt: 10}},
		NoteStates:   []OntologyNoteStateRow{{NotePath: "note.md", InputFingerprint: "input-v1", SchemaHash: "schema-v1", ResolvedType: "Spec", UpdatedAt: 10}},
		NoteTypes:    []OntologyNoteTypeRow{{NotePath: "note.md", TypeName: "Spec", SchemaHash: "schema-v1", UpdatedAt: 10}},
		Edges:        []OntologyEdgeRow{{SrcPath: "note.md", DstPath: "other.md", RelationName: "related", SchemaHash: "schema-v1", UpdatedAt: 10}},
		TypePolicies: []OntologyTypePolicyRow{{TypeName: "Spec", PolicyJSON: "{}", SchemaHash: "schema-v1", UpdatedAt: 10}},
		ReadModels: []readmodel.NodeReadModel{{NotePaths: []string{"note.md"},
			Nodes:            []readmodel.NodeRow{{NodeID: "node:source", NotePath: "note.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", SchemaHash: "schema-v1", UpdatedAt: 10}},
			FieldValues:      []readmodel.FieldValueRow{{NodeID: "node:source", NotePath: "note.md", TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "ready", ValueNorm: "ready", SchemaHash: "schema-v1", UpdatedAt: 10}},
			LinkDependencies: []readmodel.LinkDependencyRow{{SourceNotePath: "note.md", NodeID: "node:source", FieldName: "related", TargetInput: "other", TargetInputNorm: "other", SchemaHash: "schema-v1", UpdatedAt: 10}},
		}},
		Readiness: &OntologyReadinessPublication{SchemaState: state, MetadataState: metadata, ProviderHash: "provider-witness-v1",
			Validate: func(OntologyReadinessSnapshot) (bool, error) { return true, nil }},
	}
	return store, delta
}

func requireReadiness(t *testing.T, store *Store, ready bool) {
	t.Helper()
	state, err := store.GetOntologyReadiness(context.Background(), "provider-witness-v1")
	require.NoError(t, err)
	require.Equal(t, ready, state.Ready)
}

func TestOntologyReadinessPublishesAfterFinalCatalogAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	called := 0
	delta.Readiness.Validate = func(snapshot OntologyReadinessSnapshot) (bool, error) {
		called++
		if len(snapshot.Metadata) != 1 || snapshot.Metadata[0].ContentHash != "source-v1" ||
			len(snapshot.NoteStates) != 1 || snapshot.NoteStates["note.md"].InputFingerprint != "input-v1" ||
			len(snapshot.Assessments) != 1 || len(snapshot.Nodes) != 1 || len(snapshot.FieldValues) != 1 ||
			snapshot.FieldValues[0].ValueText != "ready" || snapshot.SchemaState != *delta.SchemaState ||
			snapshot.MetadataState != delta.Readiness.MetadataState {
			return false, errors.New("validator did not receive the complete final transaction")
		}
		return true, nil
	}
	requireReadiness(t, store, false)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	require.Equal(t, 1, called)
	requireReadiness(t, store, true)
	otherProvider, err := store.GetOntologyReadiness(ctx, "provider-witness-v2")
	require.NoError(t, err)
	require.False(t, otherProvider.Ready)
	require.Equal(t, *delta.SchemaState, otherProvider.SchemaState)

	var dbPath string
	var sequence int
	var database string
	require.NoError(t, store.db.QueryRowContext(ctx, `PRAGMA database_list`).Scan(&sequence, &database, &dbPath))
	require.NoError(t, store.Close())
	reopened, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	requireReadiness(t, reopened, true)
}

func TestOntologyReadinessFailedValidationRollsBackRowsAndProof(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	for _, validationErr := range []error{nil, errors.New("validation failed")} {
		delta.ReadModels[0].FieldValues[0].ValueText = "bad"
		delta.ReadModels[0].FieldValues[0].ValueNorm = "bad"
		delta.Readiness.Validate = func(snapshot OntologyReadinessSnapshot) (bool, error) {
			return snapshot.FieldValues[0].ValueText == "ready", validationErr
		}
		require.Error(t, store.ApplyOntologyDelta(ctx, delta))
		requireReadiness(t, store, true)
		fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, []string{"node:source"}, nil)
		require.NoError(t, err)
		require.Len(t, fields, 1)
		require.Equal(t, "ready", fields[0].ValueText)
	}
}

func TestOntologyReadinessRequiresExactPublicationWitnesses(t *testing.T) {
	for _, mismatch := range []string{"schema", "metadata", "raw metadata", "missing validator", "missing provider"} {
		t.Run(mismatch, func(t *testing.T) {
			store, delta := readinessFixture(t)
			switch mismatch {
			case "schema":
				delta.Readiness.SchemaState.SchemaHash = "wrong-schema"
			case "metadata":
				delta.Readiness.MetadataState.LoadedAt++
			case "raw metadata":
				delta.Readiness.MetadataState.RawNotesHash = "wrong-source"
			case "missing validator":
				delta.Readiness.Validate = nil
			case "missing provider":
				delta.Readiness.ProviderHash = ""
			}
			require.Error(t, store.ApplyOntologyDelta(context.Background(), delta))
			requireReadiness(t, store, false)
			nodes, err := store.OntologyNodeCount(context.Background())
			require.NoError(t, err)
			require.Zero(t, nodes)
		})
	}
}

func TestOntologyReadinessPartialWritesAndLegacyReadyDoNotCertify(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	require.NoError(t, store.UpsertOntologyAssessments(ctx, delta.Assessments))
	requireReadiness(t, store, false)
	require.NoError(t, store.UpsertOntologySchemaState(ctx, *delta.SchemaState))
	requireReadiness(t, store, false)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{SchemaState: *delta.SchemaState}))
	requireReadiness(t, store, false)
}

func TestMergeOntologyReadinessCannotCertifyLaterPartialOrDifferentState(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	var merged OntologyDelta
	MergeOntologyDelta(&merged, delta)
	MergeOntologyDelta(&merged, OntologyDelta{Assessments: delta.Assessments})
	require.NoError(t, store.ApplyOntologyDelta(ctx, merged))
	requireReadiness(t, store, false)

	merged = OntologyDelta{}
	MergeOntologyDelta(&merged, delta)
	MergeOntologyDelta(&merged, OntologyDelta{ReadModels: []readmodel.NodeReadModel{{NotePaths: []string{"note.md"}}}})
	require.NoError(t, store.ApplyOntologyDelta(ctx, merged))
	requireReadiness(t, store, false)
	nodes, err := store.OntologyNodeCount(ctx)
	require.NoError(t, err)
	require.Zero(t, nodes)

	merged = OntologyDelta{}
	MergeOntologyDelta(&merged, delta)
	newState := *delta.SchemaState
	newState.SchemaHash = "schema-v2"
	// A stale callback carried beside a new final schema cannot certify it.
	MergeOntologyDelta(&merged, OntologyDelta{SchemaState: &newState, Readiness: delta.Readiness})
	require.ErrorContains(t, store.ApplyOntologyDelta(ctx, merged), "does not match")
	requireReadiness(t, store, false)
}

func TestOntologyReadinessReadDoesNotHydrateSourceOrCatalog(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	store.db.SetMaxOpenConns(1)
	conn, err := store.db.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, table, _ string, _ string) int {
			if op == sqlite3.SQLITE_READ && table != "ontology_readiness" && table != "ontology_schema_state" && table != "note_metadata_state" {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())
	t.Cleanup(func() {
		conn, err := store.db.Conn(ctx)
		if err == nil {
			_ = conn.Raw(func(raw any) error { raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(nil); return nil })
			_ = conn.Close()
		}
	})
	requireReadiness(t, store, true)
}

func TestOntologyReadinessDirectSQLMutationInvalidatesProof(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	// These are the independently enumerated source/catalog boundaries. Cover
	// each SQL event because tests and repair tools also write tables directly.
	columns := map[string]string{
		"notes": "title", "note_metadata_state": "notes_hash", "note_projection_state": "provider_version",
		"note_projection_payloads": "payload_json", "property_keys": "property_name", "note_property_values": "value_text",
		"note_tags": "tag_norm", "note_link_inputs": "lookup_key", "note_fragment_targets": "target_text",
		"note_projection_diagnostics": "message", "graph_doc_edges": "kind", "ontology_schema_state": "schema_hash",
		"ontology_note_assessments": "assessment_json", "ontology_note_state": "input_fingerprint",
		"ontology_note_types": "type_name", "ontology_edges": "relation_name", "ontology_type_policies": "policy_json",
		"ontology_nodes": "title", "ontology_node_field_values": "value_text", "ontology_node_field_value_dependencies": "target_input",
	}
	for table, column := range columns {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			t.Run(table+"/"+event, func(t *testing.T) {
				tx, err := store.db.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				query := fmt.Sprintf("UPDATE %s SET %s = %s", table, column, column)
				if event == "INSERT" {
					query = fmt.Sprintf("INSERT OR REPLACE INTO %s SELECT * FROM %s", table, table)
				} else if event == "DELETE" {
					query = "DELETE FROM " + table
				}
				result, err := tx.ExecContext(ctx, query)
				require.NoError(t, err)
				changed, err := result.RowsAffected()
				require.NoError(t, err)
				require.Positive(t, changed, "fixture must exercise the trigger")
				var proofs int
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_readiness`).Scan(&proofs))
				require.Zero(t, proofs)
			})
		}
	}
	requireReadiness(t, store, true)
	require.NoError(t, store.SetBrokenLinksJSON(ctx, "[]"))
	requireReadiness(t, store, true)
}

func TestOntologyReadinessSchemaIdempotencePreservesProof(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error { return createOntologyReadinessSchema(ctx, tx) }))
	requireReadiness(t, store, true)
}
