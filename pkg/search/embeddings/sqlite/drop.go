package sqlite

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// DropSchema drops all embeddings (semantic index) tables from the SQLite database at path.
//
// This is used when semantic and code indexes share a single unified DB file and the user
// requests a semantic-only rebuild.
func DropSchema(path string) error {
	if path == "" {
		return fmt.Errorf("empty sqlite path")
	}
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
	if err != nil {
		return err
	}
	defer db.Close()

	_, _ = db.Exec(`PRAGMA foreign_keys = OFF`)

	for _, stmt := range []string{
		`DROP TABLE IF EXISTS ` + tableChunkEmbeddings + `;`,
		`DROP TABLE IF EXISTS emb_note_embeddings;`, // Legacy table for backwards compatibility
		`DROP TABLE IF EXISTS ` + tableNotes + `;`,
		`DROP TABLE IF EXISTS ` + tableIndexMeta + `;`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
