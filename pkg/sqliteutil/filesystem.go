package sqliteutil

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const sqliteAllowUnsafeFilesystemEnv = "RHIZOME_SQLITE_ALLOW_UNSAFE_FS"

// UnsafeWALFilesystemError reports a filesystem whose locking and shared-memory
// semantics are not supported by SQLite WAL mode.
type UnsafeWALFilesystemError struct {
	Path       string
	Filesystem string
}

func (e *UnsafeWALFilesystemError) Error() string {
	return fmt.Sprintf(
		"refusing SQLite WAL database %q on %s filesystem: keep all Rhizome processes and the database in one host/VM on a local filesystem; set %s=1 only if the filesystem provides coherent POSIX locks and shared memory",
		e.Path,
		e.Filesystem,
		sqliteAllowUnsafeFilesystemEnv,
	)
}

func rejectUnsafeWALFilesystem(path, fsType string) error {
	if strings.TrimSpace(os.Getenv(sqliteAllowUnsafeFilesystemEnv)) == "1" {
		return nil
	}
	fsType = strings.ToLower(strings.TrimSpace(fsType))
	if fsType == "" || !unsafeWALFilesystem(fsType) {
		return nil
	}
	return &UnsafeWALFilesystemError{Path: path, Filesystem: fsType}
}

func unsafeWALFilesystem(fsType string) bool {
	if strings.HasPrefix(fsType, "fuse") {
		return true
	}
	switch fsType {
	case "9p", "afpfs", "cifs", "nfs", "nfs4", "osxfs", "smbfs", "sshfs", "virtiofs":
		return true
	default:
		return false
	}
}

func validateWALDSN(dsn string) error {
	return validateWALDSNWith(dsn, filesystemType)
}

func validateWALDSNWith(dsn string, detect func(string) (string, error)) error {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "file" || u.Path == "" || u.Path == ":memory:" {
		return nil
	}
	path := filepath.FromSlash(u.Path)
	fsType, err := detect(path)
	if err != nil {
		if strings.TrimSpace(os.Getenv(sqliteAllowUnsafeFilesystemEnv)) == "1" {
			return nil
		}
		return fmt.Errorf("determine filesystem for SQLite WAL database %q: %w (set %s=1 only after verifying coherent POSIX locks and shared memory)", path, err, sqliteAllowUnsafeFilesystemEnv)
	}
	return rejectUnsafeWALFilesystem(path, fsType)
}
