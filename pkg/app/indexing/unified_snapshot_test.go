package indexing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func indexingParitySchema(extra string) string {
	return `type Project @node(paths: ["notes/*.md"]) {
  name: String!
  progress: Float
  due: Date
  observed: DateTime
  active: Boolean
  priority: Int
  peer: Project @link
  required: String!
  ` + extra + "\n}\n"
}

func indexingParityNote(index, count int, marker string) string {
	fields := fmt.Sprintf("\nprogress: 1.25\ndue: 2026-10-17\nobserved: 2026-10-17T12:34:56Z\nactive: true\npriority: %d\npeer: notes/project-%04d.md\n---\n", index, (index+1)%count)
	return strings.Replace(indexingworkload.Note(index, count, marker), "\n---\n", fields, 1)
}

// Enrich only parity fixtures, keeping the benchmark corpus unchanged.
func writeIndexingParityWorkload(t testing.TB, root string, notes, code int) {
	t.Helper()
	indexingworkload.Write(t, root, notes, code)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology/schema.graphql"), []byte(indexingParitySchema("")), 0o600))
	for i := range notes {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("notes/project-%04d.md", i)), []byte(indexingParityNote(i, notes, "Initial")), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/workload\n\ngo 1.24\n"), 0o600))
	if code >= 2 {
		require.NoError(t, os.WriteFile(filepath.Join(root, "src/unit_0000.go"), []byte("package workload\n\nfunc Unit0(value int) int { return Unit1(value) }\n"), 0o600))
	}
}

func canonicalWorkloadSnapshot(t testing.TB, ctx context.Context, store *semdb.Store, root string) (map[string][]string, error) {
	t.Helper()
	queries := map[string]string{
		"notes":            `SELECT path, title, format_id, content_hash FROM notes WHERE indexed_at > 0`,
		"nodes":            `SELECT note_path, node_id, node_ref_json, node_kind, type_name, parent_node_id, parent_type_name, title, source_locator, fragment, block_id, display_label, locator_status, start_byte, end_byte, structural_fingerprint, schema_hash FROM ontology_nodes`,
		"fields":           `SELECT note_path, node_id, type_name, field_name, field_kind, source_kind, value_kind, value_text, value_norm, value_int, value_bool, value_real, value_date, value_datetime, target_node_id, target_ref_json, target_note_path, target_type_name, target_source_locator, list_ordinal, schema_hash FROM ontology_node_field_values`,
		"assessments":      `SELECT note_path, declared_type, resolved_type, assessment_json, has_issues, type_ambiguous, schema_hash FROM ontology_note_assessments`,
		"ontology-edges":   `SELECT src_path, src_node_id, relation_name, dst_path, dst_node_id, dst_type, provenance, structural, schema_hash FROM ontology_edges`,
		"dependencies":     `SELECT source_note_path, node_id, type_name, field_name, target_input, target_input_norm, resolved_target_note_path, resolved_target_type_name, schema_hash FROM ontology_node_field_value_dependencies`,
		"note-types":       `SELECT note_path, type_name, schema_hash FROM ontology_note_types`,
		"links":            `SELECT src_path, dst_path, kind FROM graph_doc_edges`,
		"doc-sections":     `SELECT section_id, path, source_node_id, title, level, start_byte, end_byte, content, fingerprint, ord FROM intel_doc_sections`,
		"code-anchors":     `SELECT anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint FROM intel_code_anchors`,
		"code-symbols":     `SELECT lang, kind, file, pkg, name, fqn FROM symbols`,
		"code-edges":       `SELECT e.src_type, CASE e.src_type WHEN 'anchor' THEN s.anchor_id ELSE ds.section_id END, e.dst_type, CASE e.dst_type WHEN 'anchor' THEN d.anchor_id ELSE dd.section_id END, e.kind, e.meta_json FROM intel_edges e LEFT JOIN intel_code_anchors s ON e.src_type='anchor' AND s.id=e.src_row_id LEFT JOIN intel_doc_sections ds ON e.src_type='doc_section' AND ds.id=e.src_row_id LEFT JOIN intel_code_anchors d ON e.dst_type='anchor' AND d.id=e.dst_row_id LEFT JOIN intel_doc_sections dd ON e.dst_type='doc_section' AND dd.id=e.dst_row_id`,
		"code-references":  `SELECT f.path, r.owner_fqn, r.ref_kind, t.dst_lang, t.dst_pkg, t.dst_name, t.dst_fqn, t.dst_member FROM intel_symbol_refs r JOIN intel_symbol_ref_files f ON f.file_id=r.src_file_id JOIN intel_symbol_ref_targets t ON t.target_id=r.dst_target_id`,
		"code-imports":     `SELECT src_path, module FROM intel_import_refs`,
		"go-relationships": `SELECT kind, source_path, source_fqn, target_fqn, pointer_only FROM intel_go_derived_relationships`,
		"go-state":         `SELECT import_path, directory, package_name, build_variant, membership_digest, analyzer_version, complete, valid, diagnostics_json FROM intel_go_package_relationship_state`,
		"vectors":          `SELECT s.note_path, s.type_name, s.node_kind, s.provider, s.model, s.source_content_hash, s.node_structure_fingerprint, s.chunk_text_hash, hex(v.embedding) FROM ontology_node_embedding_state s LEFT JOIN intel_embeddings e ON e.chunk_id=s.chunk_id LEFT JOIN intel_chunks c ON c.chunk_id=e.chunk_id LEFT JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id`,
		"chunk-vectors":    `SELECT c.owner_type, c.owner_id, c.granularity, c.content_hash, hex(v.embedding) FROM intel_chunks c LEFT JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id`,
	}
	snapshot := make(map[string][]string)
	for name, query := range queries {
		rows, err := store.DB().QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", name, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		snapshot[name] = []string{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return nil, err
			}
			for i, value := range values {
				if bytes, ok := value.([]byte); ok {
					value = string(bytes)
				}
				if text, ok := value.(string); ok {
					value = strings.ReplaceAll(text, root, "<vault>")
				}
				values[i] = value
			}
			encoded, err := json.Marshal(values)
			if err != nil {
				rows.Close()
				return nil, err
			}
			snapshot[name] = append(snapshot[name], string(encoded))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		sort.Strings(snapshot[name])
	}
	codePaths, err := store.IndexedFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(codePaths)
	snapshot["code"] = codePaths
	validation, err := workloadValidationSnapshot(t, ctx, store, root)
	if err != nil {
		return nil, err
	}
	encoded, err := canonicalValidationSnapshot(ctx, store, validation, root)
	if err != nil {
		return nil, err
	}
	snapshot["validation"] = []string{encoded}
	return snapshot, nil
}

func workloadValidationSnapshot(t testing.TB, ctx context.Context, store *semdb.Store, root string) (semdb.ValidationSnapshot, error) {
	t.Helper()
	definition, err := repoconfig.LoadDefinitionFromPath(root)
	if err != nil {
		return semdb.ValidationSnapshot{}, err
	}
	indexer := testkit.NoteMetadataIndexer(t)
	runtime, runtimeErr := ontology.PublishedRuntimeWithStore(ctx, indexer, definition, store)
	snapshot, _, err := validationrun.IndexValidationSnapshot(ctx, indexer, definition, runtime, runtimeErr, 500)
	return snapshot, err
}

func canonicalValidationSnapshot(ctx context.Context, store *semdb.Store, snapshot semdb.ValidationSnapshot, root string) (string, error) {
	metadata, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return "", err
	}
	// NotesHash includes the absolute root. Prove the publication matches that
	// root's current metadata, then compare its content digest across fixtures.
	if snapshot.InputRevision != metadata.NotesHash {
		return "", fmt.Errorf("validation input revision does not match published metadata")
	}
	snapshot.InputRevision = metadata.RawNotesHash
	// Repair authority hashes the absolute root plus the source inventory and
	// schema. The published value is checked against a fresh run in this vault
	// below; cross-vault parity compares the public actions and diagnostics.
	if len(snapshot.Actions) > 0 && !strings.HasPrefix(snapshot.RepairPlanFingerprint, "plan:v3:") {
		return "", fmt.Errorf("validation actions are missing their repair-plan authority")
	}
	snapshot.RepairPlanFingerprint = ""
	// These identify a publication attempt, not its observable validation facts.
	snapshot.VaultIdentity, snapshot.Generation = "", 0
	snapshot.StartedAt, snapshot.FinishedAt, snapshot.DurationMs = 0, 0, 0
	for i := range snapshot.Checks {
		snapshot.Checks[i].DurationMs = 0
	}
	encoded, err := json.Marshal(snapshot)
	return strings.ReplaceAll(string(encoded), root, "<vault>"), err
}

func addPublishedValidationSnapshot(t testing.TB, ctx context.Context, store *semdb.Store, root string, snapshot map[string][]string) error {
	state, err := store.GetValidationStateSnapshot(ctx)
	if err != nil {
		return err
	}
	if !state.HasSnapshot {
		return fmt.Errorf("completed batch did not publish validation")
	}
	// The public snapshot API pages diagnostics separately.
	for cursor := ""; ; {
		page, err := store.GetValidationDiagnosticsPage(ctx, semdb.ValidationDiagnosticPageRequest{Generation: state.Snapshot.Generation, Limit: 200, Cursor: cursor})
		if err != nil {
			return err
		}
		state.Snapshot.Diagnostics = append(state.Snapshot.Diagnostics, page.Diagnostics...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	fresh, err := workloadValidationSnapshot(t, ctx, store, root)
	if err != nil {
		return err
	}
	if state.Snapshot.RepairPlanFingerprint != fresh.RepairPlanFingerprint {
		return fmt.Errorf("published repair-plan authority does not match fresh validation in this vault")
	}
	encoded, err := canonicalValidationSnapshot(ctx, store, state.Snapshot, root)
	if err != nil {
		return err
	}
	snapshot["published-validation"] = []string{state.State.Status, strings.ReplaceAll(state.State.Error, root, "<vault>"), encoded}
	return nil
}

func requireParitySnapshotCoverage(t testing.TB, snapshot map[string][]string) {
	t.Helper()
	fields := strings.Join(snapshot["fields"], "\n")
	require.Contains(t, fields, "1.25", "real scalar values must be materialized")
	require.Contains(t, fields, `"2026-10-17"`, "date scalar values must be materialized")
	require.Contains(t, fields, `"2026-10-17T12:34:56.000000000Z"`, "datetime scalar values must be materialized")
	var validation semdb.ValidationSnapshot
	require.NoError(t, json.Unmarshal([]byte(snapshot["validation"][0]), &validation))
	require.NotEmpty(t, validation.Checks)
	require.Positive(t, validation.IssueCount, "missing required fields must exercise validation diagnostics")
	require.NotEmpty(t, validation.Diagnostics)
}
