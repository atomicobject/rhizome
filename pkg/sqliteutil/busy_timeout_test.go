package sqliteutil

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDSNBusyTimeoutPolicyAppliesToEveryConnection(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts DSNOptions
		want int
	}{
		{"default", DSNOptions{}, 30000},
		{"negative retains default", DSNOptions{BusyTimeoutMs: -1}, 30000},
		{"explicit wait", DSNOptions{BusyTimeoutMs: 100}, 100},
		{"no wait", DSNOptions{NoBusyWait: true}, 0},
		{"no wait overrides duration", DSNOptions{NoBusyWait: true, BusyTimeoutMs: 100}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := OpenDSN(DSNWithOptions(filepath.Join(t.TempDir(), "busy.db"), tc.opts), Options{MaxOpenConns: 2})
			require.NoError(t, err)
			defer db.Close()
			// Hold both connections concurrently so each must have its own pragma.
			conns := make([]*sql.Conn, 2)
			for i := range conns {
				conns[i], err = db.Conn(t.Context())
				require.NoError(t, err)
				defer conns[i].Close()
				var timeout int
				require.NoError(t, conns[i].QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&timeout))
				require.Equal(t, tc.want, timeout)
			}
		})
	}
}
