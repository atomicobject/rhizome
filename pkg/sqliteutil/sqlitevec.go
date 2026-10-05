package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

var sqliteVecInitOnce sync.Once
var sqliteVecInitErr error

// EnsureSQLiteVec registers sqlite-vec as an auto-extension for future SQLite connections.
func EnsureSQLiteVec() error {
	sqliteVecInitOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				sqliteVecInitErr = fmt.Errorf("register sqlite-vec auto-extension: %v", r)
			}
		}()
		vec.Auto()
	})
	return sqliteVecInitErr
}

func verifySQLiteVec(ctx context.Context, db *sql.DB) error {
	// A non-zero 1D vector serialized as float32 little-endian.
	probe := []byte{0x00, 0x00, 0x80, 0x3f}
	var dist float64
	if err := db.QueryRowContext(ctx, `SELECT vec_distance_cosine(?, ?)`, probe, probe).Scan(&dist); err != nil {
		return fmt.Errorf("sqlite-vec unavailable: %w", err)
	}
	if math.Abs(dist) > 1e-6 {
		return fmt.Errorf("sqlite-vec probe returned unexpected distance: %f", dist)
	}
	return nil
}
