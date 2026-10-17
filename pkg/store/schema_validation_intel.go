package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) validateIntelSchemaTx(ctx context.Context, tx *sql.Tx) error {
	required := map[string][]string{
		"derived_work":                           {"kind", "path", "generation", "attempt", "retry_at", "ready"},
		"derived_work_epoch":                     {"kind", "generation", "revision"},
		"derived_work_sequence":                  {"kind", "path", "generation"},
		"files":                                  {"path", "lang", "hash", "indexer_version", "parse_status", "call_edges_stale", "mtime"},
		"symbols":                                {"id", "lang", "kind", "file", "pkg", "name", "fqn", "fqn_reversed"},
		"anchors":                                {"id", "label", "kind", "path_prefix"},
		"anchor_globs":                           {"anchor_id", "pattern"},
		"notes":                                  {"id", "path", "title", "content_hash", "indexer_version", "mtime", "size", "indexed_at", "note_key_full", "note_key_base", "path_len", "first_segment_norm", "format_id"},
		"property_keys":                          {"property_id", "property_name", "updated_at"},
		"note_property_values":                   {"note_id", "property_id", "source", "value_text", "value_norm", "value_kind", "is_list", "list_ordinal"},
		"note_tags":                              {"note_id", "tag_norm"},
		"note_search_terms":                      {"note_id", "scope", "segment_ordinal", "token_ordinal", "token_text"},
		"note_metadata_state":                    {"notes_hash", "raw_notes_hash", "loaded_at", "ready"},
		"note_projection_state":                  {"note_id", "provider_version", "projection_version", "source_content_hash", "status", "diagnostic_code", "diagnostic_detail", "updated_at"},
		"note_fragment_targets":                  {"note_id", "target_kind", "target_text", "target_norm", "ordinal"},
		"note_projection_diagnostics":            {"note_id", "ordinal", "code", "category", "message", "range_present", "start_byte", "end_byte", "blocking", "affected_operation"},
		"note_search_regions":                    {"note_id", "ordinal", "origin", "region_kind", "region_text", "media_type", "range_present", "start_byte", "end_byte"},
		"note_link_inputs":                       {"note_id", "lookup_key"},
		"note_projection_payloads":               {"note_id", "payload_json"},
		"ontology_readiness":                     {"id", "schema_hash", "notes_hash", "raw_notes_hash", "metadata_loaded_at", "materialization_version", "ontology_loaded_at", "provider_hash", "error_json"},
		"intel_code_anchors":                     {"id", "anchor_id", "path", "kind", "fqn"},
		"intel_doc_sections":                     {"id", "section_id", "path", "level", "content", "updated_at", "source_node_id", "ord"},
		"ontology_nodes":                         {"id", "node_id", "note_path", "node_ref_json", "node_kind", "type_name", "parent_node_id", "source_locator", "fragment", "block_id", "display_label", "locator_status", "structural_fingerprint", "relocation_fingerprint", "schema_hash", "updated_at"},
		"ontology_node_field_values":             {"id", "node_id", "note_path", "type_name", "field_name", "field_kind", "source_kind", "value_kind", "value_text", "value_norm", "value_bool", "value_int", "value_real", "value_date", "value_datetime", "target_node_id", "target_ref_json", "target_note_path", "target_type_name", "target_source_locator", "list_ordinal", "schema_hash", "updated_at"},
		"ontology_node_field_value_dependencies": {"id", "source_note_path", "node_id", "type_name", "field_name", "target_input", "target_input_norm", "resolved_target_note_path", "resolved_target_type_name", "schema_hash", "updated_at"},
		"intel_edges":                            {"src_type", "src_row_id", "dst_type", "dst_row_id", "kind"},
		"intel_chunks":                           {"id", "chunk_id", "owner_id", "owner_row_id", "owner_type", "chunk_family", "ord", "content_hash", "updated_at"},
		"intel_embeddings":                       {"chunk_row_id", "chunk_id", "norm", "dimensions", "created_at"},
		"ontology_node_embedding_state":          {"chunk_id", "node_id", "note_path", "type_name", "node_kind", "embedding_schema_signature", "chunk_text_hash", "chunk_granularity", "updated_at"},
		"index_metadata":                         {"key", "value"},
		"intel_symbol_ref_files":                 {"file_id", "path"},
		"intel_symbol_ref_targets":               {"target_id", "dst_lang", "dst_pkg", "dst_name", "dst_fqn", "dst_member"},
		"intel_symbol_refs":                      {"src_file_id", "owner_symbol_id", "owner_fqn", "ref_kind", "dst_target_id"},
		"intel_external_targets":                 {"external_id", "handle", "ecosystem", "module", "symbol_path", "target_kind"},
		"intel_external_target_map":              {"raw_target_id", "external_id"},
		"intel_external_symbol_evidence":         {"src_file_id", "owner_fqn", "ref_kind", "raw_target_id", "external_id", "evidence_kind", "confidence", "imported_name", "local_name", "manifest_path", "declared_range", "version_scope"},
		"intel_external_import_evidence":         {"src_path", "module", "binding_ordinal", "external_id", "evidence_kind", "confidence", "imported_name", "local_name", "manifest_path", "declared_range", "version_scope"},
		"intel_import_refs":                      {"src_path", "module"},
		"intel_module_defs":                      {"src_path", "lang", "module"},
		"intel_go_package_relationship_state":    {"package_key", "import_path", "directory", "package_name", "build_variant", "membership_digest", "analyzer_version", "complete", "valid", "diagnostics_json", "updated_at"},
		"intel_go_package_files":                 {"source_path", "package_key", "import_path", "directory", "package_name", "build_variant"},
		"intel_go_derived_relationships":         {"package_key", "kind", "source_path", "source_fqn", "target_fqn", "pointer_only"},
		"intel_fts_rowid":                        {"item_type", "item_id", "fts_rowid"},
		"intel_rationale":                        {"rationale_id", "path", "symbol_fqn", "kind", "content", "start_line", "end_line", "fingerprint", "updated_at"},
		"intel_rationale_fts_rowid":              {"rationale_id", "fts_rowid"},
		"graph_doc_scores":                       {"doc_path", "doc_type", "hub", "authority", "community", "inbound", "outbound", "updated_at"},
		"graph_doc_edges":                        {"src_path", "dst_path", "confidence", "confidence_score", "source_location", "link_text"},
		"graph_anchor_scores":                    {"anchor_id", "pagerank", "updated_at"},
		"graph_web_revision":                     {"id", "incarnation", "revision"},
		"ontology_note_assessments":              {"note_path", "declared_type", "resolved_type", "assessment_json", "has_issues", "type_ambiguous", "schema_hash", "updated_at"},
		"ontology_note_state":                    {"note_path", "input_fingerprint", "schema_hash", "resolved_type", "updated_at"},
		"ontology_note_types":                    {"note_path", "type_name", "schema_hash", "updated_at"},
		"ontology_edges":                         {"src_path", "src_node_id", "relation_name", "dst_path", "dst_node_id", "dst_type", "provenance", "structural", "schema_hash", "updated_at"},
		"ontology_type_policies":                 {"type_name", "policy_json", "schema_hash", "updated_at"},
		"ontology_schema_state":                  {"schema_hash", "notes_hash", "materialization_version", "loaded_at", "ready", "error_json", "broken_links_json"},
		"validation_state":                       {"id", "status", "result_json", "error", "generation", "published_generation", "started_at", "finished_at", "duration_ms"},
		"validation_generations":                 {"generation", "vault_identity", "scope", "selected_checks_json", "schema_identity", "config_identity", "input_revision", "started_at", "finished_at", "duration_ms", "completion", "stale_reason", "issue_count", "error_count", "affected_file_count", "affected_note_count", "repair_action_count", "repair_plan_fingerprint"},
		"validation_checks":                      {"generation", "check_order", "check_name", "outcome", "issue_count", "summary", "notes_json", "error", "duration_ms"},
		"validation_diagnostics":                 {"generation", "diagnostic_order", "issue_key", "check_name", "code", "message", "evidence_json", "primary_path", "type_name", "field_name", "source", "target", "location_unit", "location_start", "location_end", "node_id", "location_field", "location_relation", "variant_key", "variant_label"},
		"validation_diagnostic_paths":            {"generation", "issue_key", "path", "membership_kind"},
		"validation_diagnostic_scopes":           {"generation", "issue_key", "scope_kind", "scope_key"},
		"validation_actions":                     {"generation", "action_id", "check_name", "issue_code", "kind", "safety", "title", "summary", "question", "instance_count", "candidate_paths_json"},
		"validation_action_issues":               {"generation", "action_id", "issue_key"},
		"validation_action_paths":                {"generation", "action_id", "path"},
		"mcp_sessions":                           {"session_id", "created_at", "last_seen_at"},
		"mcp_session_items":                      {"session_id", "item_key", "fingerprint", "sent_at"},
		"mcp_session_item_reservations":          {"session_id", "item_key", "fingerprint", "reservation_id", "reserved_at"},
		"mcp_session_maintenance":                {"maintenance_key", "next_due_at"},
	}
	for table, cols := range required {
		exists, err := tableExistsQuery(ctx, tx, table)
		if err != nil {
			return fmt.Errorf("validate table %s: %w", table, err)
		}
		if !exists {
			return intelSchemaDrift("missing required table %s", table)
		}
		for _, col := range cols {
			has, err := tableHasColumnQuery(ctx, tx, table, col)
			if err != nil {
				return fmt.Errorf("validate column %s.%s: %w", table, col, err)
			}
			if !has {
				return intelSchemaDrift("missing required column %s.%s", table, col)
			}
		}
	}
	if err := validateIntelVecOwnerPartitionsTx(ctx, tx); err != nil {
		return err
	}
	if err := validateGoRelationshipSchema(ctx, tx); err != nil {
		return err
	}
	if err := validateNoteProjectionStateSchemaTx(ctx, tx); err != nil {
		return err
	}
	if err := validateNoteFormatIDSchemaTx(ctx, tx); err != nil {
		return err
	}
	if err := validateGraphRevisionSchema(ctx, tx); err != nil {
		return err
	}
	if err := validateRationaleFTSSchema(ctx, tx); err != nil {
		return err
	}
	requiredIndexes := []string{
		"idx_anchors_path_prefix",
		"idx_anchor_globs_anchor",
		"idx_property_keys_name",
		"idx_note_property_values_lookup",
		"idx_note_property_values_note",
		"idx_note_tags_tag",
		"idx_note_search_terms_lookup",
		"idx_note_search_terms_note",
		"idx_note_metadata_state_loaded",
		"idx_note_projection_state_status",
		"idx_note_fragment_targets_note",
		"idx_note_fragment_targets_lookup",
		"idx_note_projection_diagnostics_operation",
		"idx_note_search_regions_kind",
		"idx_note_link_inputs_lookup",
		"idx_notes_indexed_at",
		"idx_notes_note_key_full",
		"idx_notes_note_key_base",
		"idx_notes_first_segment_norm",
		"idx_intel_edges_mentions_dst",
		"idx_intel_chunks_owner",
		"idx_intel_chunks_owner_id",
		"idx_files_call_edges_stale",
		"idx_intel_symbol_ref_files_path",
		"idx_intel_symbol_ref_targets_name",
		"idx_intel_symbol_ref_targets_fqn",
		"idx_intel_symbol_refs_dst_target",
		"idx_intel_symbol_refs_owner",
		"idx_intel_external_target_map_external",
		"idx_intel_external_symbol_evidence_external",
		"idx_intel_external_import_evidence_external",
		"idx_intel_rationale_fts_rowid",
		"idx_intel_go_package_files_package",
		"idx_intel_go_package_files_directory",
		"idx_intel_go_derived_relationship_target",
		"idx_intel_go_derived_relationship_source",
		"idx_ontology_note_assessments_resolved",
		"idx_ontology_note_state_schema",
		"idx_ontology_note_types_type",
		"idx_ontology_edges_src",
		"idx_ontology_edges_dst",
		"idx_ontology_edges_src_query",
		"idx_ontology_edges_dst_query",
		"idx_ontology_type_policies_schema",
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
		"idx_validation_checks_order",
		"idx_validation_diagnostics_page",
		"idx_validation_diagnostics_check_code",
		"idx_validation_diagnostics_variant",
		"idx_validation_diagnostic_paths_scope",
		"idx_validation_diagnostic_scopes_scope",
		"idx_validation_action_issues_issue",
		"idx_validation_action_paths_scope",
	}
	for _, idx := range requiredIndexes {
		exists, err := indexExistsQuery(ctx, tx, idx)
		if err != nil {
			return fmt.Errorf("validate index %s: %w", idx, err)
		}
		if !exists {
			return intelSchemaDrift("missing required index %s", idx)
		}
	}
	for _, table := range ontologyReadinessSourceTables {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			name := "ontology_readiness_" + table + "_" + event
			var definition string
			if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&definition); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return intelSchemaDrift("missing required trigger %s", name)
				}
				return fmt.Errorf("validate trigger %s: %w", name, err)
			}
			if normalizeSchemaTriggerSQL(definition) != normalizeSchemaTriggerSQL(ontologyReadinessTriggerSQL(table, event)) {
				return intelSchemaDrift("invalid ontology readiness trigger %s", name)
			}
		}
	}
	return nil
}
