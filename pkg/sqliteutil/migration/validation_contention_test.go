package migration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestEnsureDomainValidationConcurrentWriter(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%v", canceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(t.TempDir(), "db.sqlite")
			db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
			require.NoError(t, err)
			defer db.Close()
			writer, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
			require.NoError(t, err)
			defer writer.Close()
			_, err = db.ExecContext(ctx, `CREATE TABLE records (value INTEGER)`)
			require.NoError(t, err)
			// The schema-init lock excludes other openers, but not an already open
			// runtime writer committing while validation reads its snapshot.
			release, err := sqliteutil.LockSchemaInit(ctx, path)
			require.NoError(t, err)
			defer release()
			committed := false
			var snapshots []int
			plan := validatedTestPlan("concurrent_writer", 0, func(ctx context.Context, tx *sql.Tx) error {
				var count int
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM records`).Scan(&count); err != nil {
					return err
				}
				snapshots = append(snapshots, count)
				if !committed {
					_, err := writer.ExecContext(ctx, `INSERT INTO records VALUES (1)`)
					if err != nil {
						return err
					}
					committed = true
					if canceled {
						cancel()
					}
				}
				return nil
			})
			err = EnsureDomain(ctx, db, plan, EnsureOptions{})
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, []int{0}, snapshots)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []int{0, 1}, snapshots, "retry must validate the new committed snapshot")
			valid, _, err := ProbeValidatedDomain(ctx, db, plan)
			require.NoError(t, err)
			require.True(t, valid)
		})
	}
}
