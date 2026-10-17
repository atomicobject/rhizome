package sourcetxn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostRenameSyncFailureRestoresEveryPublishedEntry(t *testing.T) {
	for _, faultIndex := range []int{0, 1} {
		t.Run(fmt.Sprint(faultIndex), func(t *testing.T) {
			root, options, intent := sourceFixture(t)
			fault := errors.New("directory sync failed after rename")
			err := apply(context.Background(), root, intent, options, &Hooks{BeforeInstallDirectorySync: func(index int, _ string) error {
				if index == faultIndex {
					return fault
				}
				return nil
			}})
			require.ErrorIs(t, err, fault)
			for _, file := range intent.Files {
				content, err := os.ReadFile(filepath.Join(root, file.Path))
				require.NoError(t, err)
				require.Equal(t, "old", string(content), file.Path)
			}
			require.NoError(t, CheckPending(root))
			artifacts, err := filepath.Glob(filepath.Join(root, "*.rzm-repair-*"))
			require.NoError(t, err)
			require.Empty(t, artifacts)
			require.NoError(t, recoverSourceFixture(root, options))
		})
	}
}
