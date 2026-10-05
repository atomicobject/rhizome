package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
)

func insertValidationSnapshotChecks(ctx context.Context, tx *sql.Tx, snapshot ValidationSnapshot) error {
	rows := make([][]any, 0, len(snapshot.Checks))
	for index, check := range snapshot.Checks {
		notes, err := json.Marshal(check.Notes)
		if err != nil {
			return err
		}
		rows = append(rows, []any{snapshot.Generation, index, check.Check, check.Outcome, check.IssueCount, check.Summary, string(notes), check.Error, check.DurationMs})
	}
	return execValuesBatch(ctx, tx, `INSERT INTO validation_checks(generation,check_order,check_name,outcome,issue_count,summary,notes_json,error,duration_ms) VALUES `, rows, 9, "")
}

func insertValidationSnapshotDiagnostics(ctx context.Context, tx *sql.Tx, snapshot ValidationSnapshot) error {
	rows := make([][]any, 0, len(snapshot.Diagnostics))
	var paths, scopes [][]any
	for index, diagnostic := range snapshot.Diagnostics {
		location := ValidationDiagnosticLocation{}
		if diagnostic.Location != nil {
			location = *diagnostic.Location
		}
		variant := ValidationIssueVariant{}
		if diagnostic.Variant != nil {
			variant = *diagnostic.Variant
		}
		rows = append(rows, []any{snapshot.Generation, index, diagnostic.IssueKey, diagnostic.Check, diagnostic.Code, diagnostic.Message, string(diagnostic.Evidence), diagnostic.PrimaryPath, diagnostic.Type, diagnostic.Field, diagnostic.Source, diagnostic.Target, location.Unit, location.Start, location.End, location.NodeID, location.Field, location.Relation, variant.Key, variant.Label})
		for _, path := range diagnostic.AffectedPaths {
			paths = append(paths, []any{snapshot.Generation, diagnostic.IssueKey, path, "affected"})
		}
		for _, path := range diagnostic.AffectedNotePaths {
			paths = append(paths, []any{snapshot.Generation, diagnostic.IssueKey, path, "note"})
		}
		for _, membership := range []struct {
			kind string
			keys []string
		}{{ValidationScopeNode, diagnostic.AffectedNodeIDs}, {ValidationScopeType, diagnostic.AffectedTypes}, {ValidationScopeInterface, diagnostic.AffectedInterfaces}} {
			for _, key := range membership.keys {
				scopes = append(scopes, []any{snapshot.Generation, diagnostic.IssueKey, membership.kind, key})
			}
		}
	}
	if err := execValuesBatch(ctx, tx, `INSERT INTO validation_diagnostics(generation,diagnostic_order,issue_key,check_name,code,message,evidence_json,primary_path,type_name,field_name,source,target,location_unit,location_start,location_end,node_id,location_field,location_relation,variant_key,variant_label) VALUES `, rows, 20, ""); err != nil {
		return err
	}
	if err := execValuesBatch(ctx, tx, `INSERT OR IGNORE INTO validation_diagnostic_paths(generation,issue_key,path,membership_kind) VALUES `, paths, 4, ""); err != nil {
		return err
	}
	return execValuesBatch(ctx, tx, `INSERT OR IGNORE INTO validation_diagnostic_scopes(generation,issue_key,scope_kind,scope_key) VALUES `, scopes, 4, "")
}

func insertValidationSnapshotActions(ctx context.Context, tx *sql.Tx, snapshot ValidationSnapshot) error {
	rows := make([][]any, 0, len(snapshot.Actions))
	var issues, paths [][]any
	for _, action := range snapshot.Actions {
		candidates, err := json.Marshal(action.CandidatePaths)
		if err != nil {
			return err
		}
		rows = append(rows, []any{snapshot.Generation, action.ID, action.Check, action.IssueCode, action.Kind, action.Safety, action.Title, action.Summary, action.Question, action.InstanceCount, string(candidates)})
		for _, key := range action.IssueKeys {
			issues = append(issues, []any{snapshot.Generation, action.ID, key})
		}
		for _, path := range action.AffectedPaths {
			paths = append(paths, []any{snapshot.Generation, action.ID, path})
		}
	}
	if err := execValuesBatch(ctx, tx, `INSERT INTO validation_actions(generation,action_id,check_name,issue_code,kind,safety,title,summary,question,instance_count,candidate_paths_json) VALUES `, rows, 11, ""); err != nil {
		return err
	}
	if err := execValuesBatch(ctx, tx, `INSERT OR IGNORE INTO validation_action_issues(generation,action_id,issue_key) VALUES `, issues, 3, ""); err != nil {
		return err
	}
	return execValuesBatch(ctx, tx, `INSERT OR IGNORE INTO validation_action_paths(generation,action_id,path) VALUES `, paths, 3, "")
}
