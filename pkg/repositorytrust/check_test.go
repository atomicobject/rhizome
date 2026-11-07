package repositorytrust

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDenialReasonReportsLookupFailure(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, "repository trust could not be read: unavailable", DenialReason(dir, "actions", func(string) (bool, error) {
		return false, errors.New("unavailable")
	}))
	store := Store{Dir: t.TempDir()}
	canonical, err := CanonicalCheckout(dir)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(store.recordPath(canonical), 0700))
	require.Contains(t, DenialReason(dir, "actions", store.Trusted), "repository trust could not be read:")
}
