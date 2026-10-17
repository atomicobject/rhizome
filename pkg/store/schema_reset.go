package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"strings"
	"sync"
)

// ResetDomain drops and recreates the code-index domain tables (code anchors + intel + doc_links).
// It does NOT touch semantic embeddings tables (`emb_*`) nor code embeddings tables (`code_*`).
func (s *Store) ResetDomain(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.reset_domain")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		defer func() {
			_, _ = db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
		}()

		// Drop views first (a corrupted DB can have a view shadowing a table name).
		dropViews := []string{
			"schema_version",
			"files",
			"symbols",
			"super_edges",
			"annotations",
			"calls",
			"anchors",
			"anchor_globs",
			"notes",
			"property_keys",
			"note_property_values",
			"note_tags",
			"note_metadata_state",
			"note_projection_state",
			"note_fragment_targets",
			"note_projection_diagnostics",
			"note_search_regions",
			"note_link_inputs",
			"note_projection_payloads",
			"ontology_readiness",
			"note_anchors",
			"anchor_scopes",
			"index_metadata",
			"doc_links",
			"intel_code_anchors",
			"intel_doc_sections",
			"intel_edges",
			"intel_symbol_ref_files",
			"intel_symbol_ref_targets",
			"intel_symbol_refs",
			"intel_external_targets",
			"intel_external_target_map",
			"intel_external_symbol_evidence",
			"intel_external_import_evidence",
			"intel_import_refs",
			"intel_module_defs",
			"intel_go_package_relationship_state",
			"intel_go_package_files",
			"intel_go_derived_relationships",
			"intel_chunks",
			"intel_fts",
			"intel_fts_rowid",
			"intel_rationale",
			"intel_rationale_fts",
			"intel_rationale_fts_rowid",
			"graph_doc_scores",
			"graph_doc_edges",
			"graph_anchor_scores",
			"ontology_note_assessments",
			"ontology_note_state",
			"ontology_note_types",
			"ontology_edges",
			"ontology_type_policies",
			"ontology_schema_state",
			"ontology_nodes",
			"ontology_node_field_values",
			"ontology_node_field_value_dependencies",
			"ontology_node_embedding_state",
			"mcp_session_item_reservations",
			"mcp_session_maintenance",
		}
		for _, name := range dropViews {
			if err := dropViewIfExists(ctx, db, name); err != nil {
				return err
			}
		}

		// Indices (best-effort; dropping tables will remove their indices, but keep this resilient).
		dropIndexes := []string{
			"idx_symbols_file",
			"idx_symbols_lang_pkg_name",
			"idx_symbols_lang_fqn_reversed",
			"idx_super_child",
			"idx_super_parent",
			"idx_annotations_owner",
			"idx_annotations_type",
			"idx_annotations_type_owner",
			"idx_calls_file",
			"idx_calls_callee",
			"idx_calls_owner",
			"idx_anchors_callee",
			"idx_anchors_path_prefix",
			"idx_property_keys_name",
			"idx_note_property_values_lookup",
			"idx_note_property_values_note",
			"idx_note_tags_tag",
			"idx_note_metadata_state_loaded",
			"idx_note_projection_state_status",
			"idx_note_fragment_targets_note",
			"idx_note_fragment_targets_lookup",
			"idx_note_projection_diagnostics_operation",
			"idx_note_search_regions_kind",
			"idx_note_link_inputs_lookup",
			"idx_notes_indexed_at",
			"idx_anchor_globs_anchor",
			"idx_anchor_scopes_anchor",
			"idx_anchor_scopes_symbol",
			"idx_anchor_scopes_call_file",
			"idx_intel_code_anchors_path",
			"idx_intel_code_anchors_fqn",
			"idx_intel_code_anchors_lang_fqn",
			"idx_intel_edges_dst_kind",
			"idx_intel_edges_src_kind",
			"idx_intel_edges_kind_dst",
			"idx_intel_edges_kind_src",
			"idx_intel_edges_mentions_dst",
			"idx_intel_symbol_refs_dst_target",
			"idx_intel_symbol_refs_owner",
			"idx_intel_symbol_ref_files_path",
			"idx_intel_symbol_ref_targets_name",
			"idx_intel_symbol_ref_targets_fqn",
			"idx_intel_external_target_map_external",
			"idx_intel_external_symbol_evidence_external",
			"idx_intel_external_import_evidence_external",
			"idx_intel_import_refs_module",
			"idx_intel_module_defs_src",
			"idx_intel_module_defs_module",
			"idx_intel_go_package_files_package",
			"idx_intel_go_package_files_directory",
			"idx_intel_go_derived_relationship_target",
			"idx_intel_go_derived_relationship_source",
			"idx_intel_doc_sections_path",
			"idx_intel_fts_rowid_item",
			"idx_intel_rationale_path",
			"idx_intel_rationale_symbol",
			"idx_intel_rationale_kind",
			"idx_intel_rationale_fts_rowid",
			"idx_intel_chunks_owner",
			"idx_intel_chunks_owner_id",
			"idx_intent_embeddings_intent",
			"idx_doc_links_dst",
			"idx_doc_links_src",
			"idx_graph_doc_scores_authority",
			"idx_graph_doc_scores_community",
			"idx_graph_doc_edges_src",
			"idx_graph_doc_edges_dst",
			"idx_graph_doc_edges_kind_src",
			"idx_graph_anchor_scores_rank",
			"idx_files_call_edges_stale",
			"idx_ontology_note_assessments_resolved",
			"idx_ontology_note_state_schema",
			"idx_ontology_note_types_type",
			"idx_ontology_edges_src",
			"idx_ontology_edges_dst",
			"idx_ontology_edges_src_query",
			"idx_ontology_edges_dst_query",
			"idx_ontology_type_policies_schema",
			"idx_ontology_schema_state_loaded",
			"idx_ontology_nodes_note_path",
			"idx_ontology_nodes_type",
			"idx_ontology_nodes_source_locator",
			"idx_ontology_nodes_note_fragment",
			"idx_ontology_nodes_note_block",
			"idx_ontology_node_field_values_unique",
			"idx_ontology_node_field_values_node",
			"idx_ontology_node_field_values_norm",
			"idx_ontology_node_field_values_bool",
			"idx_ontology_node_field_values_date",
			"idx_ontology_node_field_values_datetime",
			"idx_ontology_node_field_values_real",
			"idx_ontology_node_field_values_int",
			"idx_ontology_node_field_values_target",
			"idx_ontology_node_field_values_target_note",
			"idx_ontology_node_field_values_note",
			"idx_ontology_node_field_value_dependencies_unique",
			"idx_ontology_node_field_value_dependencies_source",
			"idx_ontology_node_field_value_dependencies_target_input",
			"idx_ontology_node_field_value_dependencies_resolved_target",
			"idx_ontology_node_embedding_state_node",
			"idx_ontology_node_embedding_state_note",
			"idx_ontology_node_embedding_state_type",
			"idx_mcp_session_item_reservations_owner",
		}
		for _, name := range dropIndexes {
			if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
				return err
			}
		}

		// Tables (includes virtual tables).
		dropTables := []string{
			"graph_doc_scores",
			"graph_doc_edges",
			"graph_anchor_scores",
			"graph_web_revision",
			"ontology_note_assessments",
			"ontology_note_state",
			"ontology_note_types",
			"ontology_edges",
			"ontology_type_policies",
			"ontology_schema_state",
			"ontology_node_embedding_state",
			"ontology_node_field_value_dependencies",
			"ontology_node_field_values",
			"ontology_nodes",
			"note_fragment_targets",
			"note_projection_diagnostics",
			"note_search_regions",
			"note_link_inputs",
			"note_projection_payloads",
			"ontology_readiness",
			"note_projection_state",
			"note_metadata_state",
			"note_tags",
			"note_property_values",
			"property_keys",
			"intel_fts",
			"intel_fts_rowid",
			"intel_rationale",
			"intel_rationale_fts",
			"intel_rationale_fts_rowid",
			"intel_embeddings",
			"intel_chunks",
			"intel_edges",
			"intel_doc_sections",
			"intel_code_anchors",
			"intel_symbol_ref_files",
			"intel_external_symbol_evidence",
			"intel_external_import_evidence",
			"intel_external_target_map",
			"intel_external_targets",
			"intel_symbol_ref_targets",
			"intel_symbol_refs",
			"intel_import_refs",
			"intel_module_defs",
			"intel_go_derived_relationships",
			"intel_go_package_files",
			"intel_go_package_relationship_state",
			"doc_links",
			"intent_embeddings",
			"anchor_scopes",
			"note_anchors",
			"notes",
			"anchor_globs",
			"anchors",
			"calls",
			"annotations",
			"super_edges",
			"symbols",
			"files",
			"index_metadata",
			"mcp_session_item_reservations",
			"mcp_session_maintenance",
			"schema_version",
		}
		for _, name := range dropTables {
			if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
				return err
			}
		}
		if err := dropVecTablesByPrefix(ctx, db, intelVecTablePrefix); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainIntel)); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
				return err
			}
		}
		s.vecReady = sync.Map{}
		return nil
	})
}
