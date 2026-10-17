package validate_test

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"

	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"

	"github.com/stretchr/testify/require"
)

type externalPostApplyRefresher struct{}

func (externalPostApplyRefresher) Refresh(
	context.Context,
	*indexlock.Lease,
	[]string,
	[]validate.PathRename,
	[]string,
) (validate.PostApplyRefreshResult, error) {
	return validate.PostApplyRefreshResult{}, nil
}

var _ validate.PostApplyRefresher = externalPostApplyRefresher{}

func TestIndexLockLeaseRejectsForgedAuthority(t *testing.T) {
	var lease indexlock.Lease
	require.Error(t, lease.Validate(t.TempDir()))
}
