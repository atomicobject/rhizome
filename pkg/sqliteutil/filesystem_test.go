package sqliteutil

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestRejectUnsafeWALFilesystem(t *testing.T) {
	t.Setenv(sqliteAllowUnsafeFilesystemEnv, "")

	for _, fsType := range []string{"virtiofs", "fuse.grpcfuse", "nfs4", "cifs", "9p"} {
		t.Run(fsType, func(t *testing.T) {
			err := rejectUnsafeWALFilesystem("/vault/.rhizome/db.sqlite", fsType)
			var unsafeErr *UnsafeWALFilesystemError
			if !errors.As(err, &unsafeErr) {
				t.Fatalf("expected UnsafeWALFilesystemError for %q, got %v", fsType, err)
			}
			if unsafeErr.Filesystem != fsType {
				t.Fatalf("expected filesystem %q, got %q", fsType, unsafeErr.Filesystem)
			}
		})
	}
}

func TestAllowUnsafeWALFilesystemRequiresExplicitOverride(t *testing.T) {
	t.Setenv(sqliteAllowUnsafeFilesystemEnv, "1")

	if err := rejectUnsafeWALFilesystem("/vault/.rhizome/db.sqlite", "virtiofs"); err != nil {
		t.Fatalf("expected explicit override to allow filesystem: %v", err)
	}
}

func TestLocalWALFilesystemAllowed(t *testing.T) {
	t.Setenv(sqliteAllowUnsafeFilesystemEnv, "")

	for _, fsType := range []string{"apfs", "ext4", "xfs", "tmpfs", ""} {
		if err := rejectUnsafeWALFilesystem("/vault/.rhizome/db.sqlite", fsType); err != nil {
			t.Fatalf("expected local filesystem %q to be allowed: %v", fsType, err)
		}
	}
}

func TestValidateWALDSNRejectsDetectedSharedFilesystem(t *testing.T) {
	t.Setenv(sqliteAllowUnsafeFilesystemEnv, "")
	dsn := DSN("/vault/.rhizome/db.sqlite")

	err := validateWALDSNWith(dsn, func(path string) (string, error) {
		if path != filepath.FromSlash("/vault/.rhizome/db.sqlite") {
			t.Fatalf("unexpected database path %q", path)
		}
		return "virtiofs", nil
	})
	var unsafeErr *UnsafeWALFilesystemError
	if !errors.As(err, &unsafeErr) {
		t.Fatalf("expected unsafe filesystem error, got %v", err)
	}
}

func TestValidateWALDSNFailsClosedWhenFilesystemCannotBeDetermined(t *testing.T) {
	t.Setenv(sqliteAllowUnsafeFilesystemEnv, "")
	dsn := DSN("/vault/.rhizome/db.sqlite")

	err := validateWALDSNWith(dsn, func(string) (string, error) {
		return "", fmt.Errorf("mount table unavailable")
	})
	if err == nil {
		t.Fatal("expected filesystem detection failure to refuse WAL open")
	}
}
