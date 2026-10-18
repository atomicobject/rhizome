package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// PublishCurrentProjection explicitly publishes structural indexes before an
// application operation consumes them. Ordinary published-runtime reads do not
// call this method. Provider-backed derived work remains with its durable owner.
func (rt *LiveRuntime) PublishCurrentProjection(ctx context.Context) error {
	if rt == nil || rt.readOnlyCodeIndex {
		return fmt.Errorf("current projection publication requires a writable runtime")
	}
	if err := rt.WaitForCodeIndex(ctx); err != nil {
		return err
	}
	if rt.isClosed() {
		return context.Canceled
	}
	indexLane := rt.Lane()
	if indexLane == nil {
		if !rt.disableLeaderWork {
			return fmt.Errorf("current projection publication requires the runtime indexing lane")
		}
		owned := lane.New(lane.Options{LockPath: repoconfig.IndexLockPath(rt.VaultPath), Debug: rt.debug})
		indexLane = owned
		// A concurrent runtime Close must drain this one-shot lane before its
		// store. Both shutdown and this call may close it safely.
		rt.addCloser(owned.Close)
		defer owned.Close()
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(rt.ctx, cancel)
	defer stop()
	handle, _, err := indexLane.Submit(callCtx, lane.Request{
		Kind: lane.KindValidationRefresh, Trigger: "current_projection",
		Run: func(jobCtx context.Context, lease *indexlock.Lease, _ lane.Reporter) error {
			return rt.publishCurrentProjectionUnderLease(jobCtx, lease)
		},
	})
	if err != nil {
		return err
	}
	select {
	case <-handle.Done():
		return handle.Err()
	case <-callCtx.Done():
		handle.Cancel()
		<-handle.Done()
		return errors.Join(callCtx.Err(), handle.Err())
	}
}

func (rt *LiveRuntime) publishCurrentProjectionUnderLease(ctx context.Context, lease *indexlock.Lease) (err error) {
	store := rt.IntelStore()
	configuration := rt.Configuration()
	request := indexcore.Request{
		VaultDefinition: rt.currentOwnershipVaultDefinition(),
		CodeConfig:      configuration.Code,
		NoteMetadata:    rt.NoteMetadataIndexer(),
	}
	discovery, err := indexcore.Discover(ctx, request, store)
	if err != nil {
		return fmt.Errorf("discover current projection: %w", err)
	}
	service, _ := codeintel.NewCodeAnchorService(codeintel.CodeAnchorServiceConfig{
		VaultPath: rt.VaultPath, CodeCfg: configuration.Code, Store: store,
		IncludeIndexers: configuration.Code.Enabled, WriteAccess: true,
	})
	queue := indexwriter.New(ctx, indexcore.BindWriterHandlers(indexwriter.Handlers{}, service, store))
	defer func() { err = errors.Join(err, queue.Close()) }()
	if _, err = indexcore.Publish(ctx, lease, request, discovery, service, store, queue, indexcore.PublishOptions{}); err != nil {
		return fmt.Errorf("publish current projection: %w", err)
	}
	return queue.FlushAndWait(ctx)
}
