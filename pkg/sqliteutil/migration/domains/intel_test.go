package domains

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelPlanFromNumbersOnlyPostBaselineSteps(t *testing.T) {
	plan := IntelPlanFrom(56, 58, []TxStep{
		func(context.Context, *sql.Tx) error { return nil },
		func(context.Context, *sql.Tx) error { return nil },
	}, nil)
	require.Len(t, plan.Steps, 2)
	require.Equal(t, 57, plan.Steps[0].To)
	require.Equal(t, "intel_v56_to_v57", plan.Steps[0].Name)
	require.Equal(t, 58, plan.Steps[1].To)
	require.Equal(t, "intel_v57_to_v58", plan.Steps[1].Name)
}
