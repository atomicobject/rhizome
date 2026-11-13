package serve

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type liveValidationProjectionRefresher struct {
	live *bootstrap.LiveRuntime
}

func (r liveValidationProjectionRefresher) projection() validationproduct.ValidationProjectionPostApplyRefresher {
	return validationproduct.ValidationProjectionPostApplyRefresher{
		VaultPath: r.live.VaultPath, VaultDef: r.live.Snapshot().VaultDefinition,
		NoteMetadata: r.live.NoteMetadataIndexer(), NoteReader: &obsidian.Note{},
	}
}

func (r liveValidationProjectionRefresher) Refresh(ctx context.Context, lease *indexlock.Lease, changed []string, renamed []validate.PathRename, deleted []string) (validate.PostApplyRefreshResult, error) {
	return r.projection().Refresh(ctx, lease, changed, renamed, deleted)
}

func (r liveValidationProjectionRefresher) RefreshScoped(ctx context.Context, lease *indexlock.Lease, scope validate.PostApplyRefreshScope) (validate.PostApplyRefreshResult, error) {
	return r.projection().RefreshScoped(ctx, lease, scope)
}
