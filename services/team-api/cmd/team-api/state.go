package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotable"
	"github.com/atomicobject/rhizome/services/team-api/internal/ledger"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

func openState(ctx context.Context, cfg config.Config) (store.Store, ledger.Ledger, func(), error) {
	if cfg.State.Backend == "dynamodb" {
		// No endpoint override in deployed configuration. ECS supplies credentials
		// through the SDK default chain, normally the task role.
		aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.State.Region))
		if err != nil {
			return nil, nil, nil, err
		}
		table := &dynamotable.Table{Client: dynamodb.NewFromConfig(aws), Name: cfg.State.Table}
		l, err := ledger.OpenDynamoDB(ctx, table, time.Now)
		if err != nil {
			return nil, nil, nil, err
		}
		return store.NewDynamoDB(table), l, func() {}, nil
	}
	db, err := store.Open(filepath.Join(cfg.DataDir, "team-api.db"))
	if err != nil {
		return nil, nil, nil, err
	}
	l, err := ledger.Open(ctx, db, time.Now)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	return store.NewSQLite(db), l, func() { _ = db.Close() }, nil
}

// Heartbeat runs independently of recovery, which may walk many receipts.
// A failed heartbeat stops new HTTP work and drains requests; store checks
// still fence any settlement after expiry. Stop waits for both workers.
func maintainLedger(l ledger.Ledger, stopServing context.CancelFunc) func() {
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	for _, heartbeat := range []bool{true, false} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(ledger.LeaseDuration / 3)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				op, done := context.WithTimeout(ctx, 10*time.Second)
				var err error
				if heartbeat {
					err = l.Heartbeat(op)
				} else {
					err = l.Recover(op)
				}
				done()
				if err != nil && ctx.Err() == nil {
					slog.Error("ledger maintenance failed", "heartbeat", heartbeat, "error", err)
					if heartbeat {
						stopServing()
						return
					}
				}
			}
		}()
	}
	return func() { cancel(); workers.Wait() }
}
