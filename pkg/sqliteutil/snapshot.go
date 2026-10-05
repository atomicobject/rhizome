package sqliteutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SnapshotInto writes a transactionally consistent copy of the database at
// sourcePath to targetPath with VACUUM INTO. Copying the file and its WAL and
// SHM sidecars one at a time can interleave a concurrent writer's states; a
// snapshot reads one WAL-mode transaction, includes committed WAL content, and
// leaves a checkpointed single file. targetPath must not exist. The source
// opens in existing-write mode because SQLite refuses VACUUM INTO under
// query_only; nothing in the source changes.
func SnapshotInto(ctx context.Context, sourcePath, targetPath string) error {
	db, err := OpenDSN(DSNWithOptions(sourcePath, DSNOptions{ExistingWrite: true}), Options{MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		return fmt.Errorf("open source database %s: %w", sourcePath, err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, targetPath); err != nil {
		return fmt.Errorf("snapshot %s into %s: %w", sourcePath, targetPath, err)
	}
	return nil
}

// cloneLockTimeoutMs bounds how long CloneInto waits for the source's write
// lock before the caller falls back to SnapshotInto.
const cloneLockTimeoutMs = 2000

var errCloneUnsupported = fmt.Errorf("copy-on-write clone: %w", errors.ErrUnsupported)

// CloneInto copies the database at sourcePath to targetPath with copy-on-write
// filesystem clones of the database and its WAL. It holds the source's write
// lock while cloning, which freezes the WAL: no frame is appended and no
// checkpoint can reset or truncate it. A concurrent passive checkpoint can
// still backfill pages into the database file, but every such page has a WAL
// frame in the cloned WAL that supersedes it when the target first opens. The
// SHM index is never cloned; SQLite rebuilds it from the WAL. targetPath and
// its sidecars must not exist. Any error leaves no target files behind.
func CloneInto(ctx context.Context, sourcePath, targetPath string) (err error) {
	// Clone the files SQLite opens, never a symlink to them: a cloned link
	// would make the target write into the source.
	sourcePath, err = filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source database: %w", err)
	}
	db, err := OpenDSN(DSNWithOptions(sourcePath, DSNOptions{ExistingWrite: true, BusyTimeoutMs: cloneLockTimeoutMs}), Options{MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		return fmt.Errorf("open source database %s: %w", sourcePath, err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open source database %s: %w", sourcePath, err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("lock source database %s: %w", sourcePath, err)
	}
	defer func() {
		if err != nil {
			for _, path := range []string{targetPath, targetPath + "-wal"} {
				_ = os.Remove(path)
			}
		}
	}()
	defer func() {
		_, rollbackErr := conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		err = errors.Join(err, rollbackErr)
	}()
	if err := cloneFile(sourcePath, targetPath); err != nil {
		return fmt.Errorf("clone %s: %w", sourcePath, err)
	}
	if _, statErr := os.Stat(sourcePath + "-wal"); statErr == nil {
		if err := cloneFile(sourcePath+"-wal", targetPath+"-wal"); err != nil {
			return fmt.Errorf("clone %s-wal: %w", sourcePath, err)
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return nil
}
