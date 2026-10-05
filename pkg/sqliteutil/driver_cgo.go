//go:build cgo

package sqliteutil

import (
	"database/sql"
	"strconv"
	"sync"

	"github.com/mattn/go-sqlite3"
)

const rhizomeSQLiteDriverName = "sqlite3_rhizome"

var registerRhizomeSQLiteDriver sync.Once

func sqliteDriverName() string {
	registerRhizomeSQLiteDriver.Do(func() {
		sql.Register(rhizomeSQLiteDriverName, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				_, err := conn.Exec("PRAGMA mmap_size = "+strconv.FormatInt(mmapSizeFromEnv(), 10), nil)
				return err
			},
		})
	})
	return rhizomeSQLiteDriverName
}
