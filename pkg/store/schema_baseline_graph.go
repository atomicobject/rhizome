package store

import (
	"context"
)

func intelBaselineStep09(ctx context.Context, db execer) error { // v9 -> v10
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS graph_doc_scores (
				doc_path TEXT PRIMARY KEY CHECK (doc_path != ''),
				doc_type TEXT NOT NULL CHECK (doc_type IN ('note', 'code')),
				hub REAL NOT NULL,
				authority REAL NOT NULL,
				community TEXT NOT NULL,
				inbound INTEGER NOT NULL,
				outbound INTEGER NOT NULL,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_graph_doc_scores_authority ON graph_doc_scores(authority DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_graph_doc_scores_community ON graph_doc_scores(community);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep10(ctx context.Context, db execer) error { // v10 -> v11
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS graph_doc_edges (
				src_path TEXT NOT NULL CHECK (src_path != ''),
				dst_path TEXT NOT NULL CHECK (dst_path != ''),
				kind TEXT NOT NULL CHECK (kind != ''),
				PRIMARY KEY (src_path, dst_path, kind)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_src ON graph_doc_edges(src_path);`,
		`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_dst ON graph_doc_edges(dst_path);`,
		`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_kind_src ON graph_doc_edges(kind, src_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep11(ctx context.Context, db execer) error { // v11 -> v12
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS graph_anchor_scores (
				anchor_id TEXT PRIMARY KEY CHECK (anchor_id != ''),
				pagerank REAL NOT NULL,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_graph_anchor_scores_rank ON graph_anchor_scores(pagerank DESC);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep14(ctx context.Context, db execer) error { // v14 -> v15
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_kind_dst ON graph_doc_edges(kind, dst_path);`,
		`CREATE INDEX IF NOT EXISTS idx_doc_links_dst_path ON doc_links(dst_kind, dst_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep34(ctx context.Context, db execer) error { // v34 -> v35
	// Add confidence columns to graph_doc_edges. Guard each ALTER with a column-existence
	// check so this migration is idempotent (safe if re-run after manual state resets).
	type colAdd struct {
		col  string
		stmt string
	}
	cols := []colAdd{
		{"confidence", `ALTER TABLE graph_doc_edges ADD COLUMN confidence TEXT NOT NULL DEFAULT 'extracted'`},
		{"confidence_score", `ALTER TABLE graph_doc_edges ADD COLUMN confidence_score REAL NOT NULL DEFAULT 1.0`},
		{"source_location", `ALTER TABLE graph_doc_edges ADD COLUMN source_location TEXT NOT NULL DEFAULT ''`},
	}
	if q, ok := db.(queryRower); ok {
		for _, c := range cols {
			has, err := tableHasColumnQuery(ctx, q, "graph_doc_edges", c.col)
			if err != nil {
				return err
			}
			if has {
				continue
			}
			if _, err := db.ExecContext(ctx, c.stmt); err != nil {
				return err
			}
		}
	} else {
		for _, c := range cols {
			if _, err := db.ExecContext(ctx, c.stmt); err != nil {
				return err
			}
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_confidence ON graph_doc_edges(confidence)`); err != nil {
		return err
	}
	return nil
}
