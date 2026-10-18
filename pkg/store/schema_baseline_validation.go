package store

import (
	"context"
)

func intelBaselineStep52(ctx context.Context, db execer) error { // v52 -> v53
	if err := ensureValidationAndRationaleSchema(ctx, db); err != nil {
		return err
	}
	return nil
}

func intelBaselineStep53(ctx context.Context, db execer) error { // v53 -> v54
	return ensureValidationAndRationaleSchema(ctx, db)
}
