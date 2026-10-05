package validate_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type externalPostApplyRefresher struct{}

func (externalPostApplyRefresher) Refresh(
	context.Context,
	*validate.IndexLockLease,
	[]string,
	[]validate.PathRename,
	[]string,
) (validate.PostApplyRefreshResult, error) {
	return validate.PostApplyRefreshResult{}, nil
}

var _ validate.PostApplyRefresher = externalPostApplyRefresher{}

func TestIndexLockLeaseIsBorrowOnlyOutsideValidate(t *testing.T) {
	var lease validate.IndexLockLease
	require.Error(t, lease.RequireHeld())
	leaseType := reflect.TypeOf(&lease)
	_, exposesRelease := leaseType.MethodByName("Release")
	_, exposesLockPath := leaseType.MethodByName("LockPath")
	assert.False(t, exposesRelease)
	assert.False(t, exposesLockPath)
}
