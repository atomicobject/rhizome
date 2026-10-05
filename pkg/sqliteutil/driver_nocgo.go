//go:build !cgo

package sqliteutil

func sqliteDriverName() string {
	return "sqlite3"
}
