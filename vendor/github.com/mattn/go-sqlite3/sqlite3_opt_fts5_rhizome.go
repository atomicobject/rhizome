//go:build cgo && !fts5 && !sqlite_fts5
// +build cgo,!fts5,!sqlite_fts5

package sqlite3

/*
#cgo CFLAGS: -DSQLITE_ENABLE_FTS5
#cgo LDFLAGS: -lm
*/
import "C"
