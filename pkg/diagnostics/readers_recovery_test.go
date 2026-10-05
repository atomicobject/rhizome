package diagnostics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadReportRecoversProtectedCopyFromDamagedHistory(t *testing.T) {
	for _, damage := range []string{"malformed", "wrong identity"} {
		t.Run(damage, func(t *testing.T) {
			root := t.TempDir()
			r := openTestRecorder(t, root, Options{})
			ctx := operationContext("index", time.Now())
			require.NoError(t, r.PublishReport(ctx, Report{Status: "error", ReasonCode: "synthetic_failure"}))
			latest, err := ReadLatest(root, "index")
			require.NoError(t, err)
			entries, err := os.ReadDir(filepath.Join(r.dir, "reports"))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			path := filepath.Join(r.dir, "reports", entries[0].Name())
			data := []byte("{broken")
			if damage == "wrong identity" {
				other, bytes, err := r.prepareReport(operationContext("index", time.Now()), Report{Status: "success"})
				require.NoError(t, err)
				require.NotEqual(t, latest.OperationID, other.OperationID)
				data = bytes
			}
			require.NoError(t, os.WriteFile(path, data, 0o600))
			recovered, err := ReadReport(root, latest.OperationID)
			require.NoError(t, err)
			require.Equal(t, latest, recovered)
			// A protected copy for another operation cannot conceal damaged history.
			newer := operationContext("index", time.Now())
			require.NoError(t, r.PublishReport(newer, Report{Status: "success"}))
			_, err = ReadReport(root, latest.OperationID)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrNotFound)
		})
	}
}
