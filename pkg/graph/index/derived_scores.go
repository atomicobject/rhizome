package graphindex

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// ComputeDerivedScores separates graph computation from durable publication.
// Runtime callers compute outside the vault lease; batch callers choose their
// throughput policy while using the same graph processing.

func ComputeDerivedScores(ctx context.Context, store *semdb.Store, opts DocScoresOptions) (semdb.DerivedGraphScores, error) {
	docs, err := ComputeDocScores(ctx, store, opts)
	if err != nil {
		return semdb.DerivedGraphScores{}, err
	}
	anchors, err := ComputeAnchorPageRank(ctx, store)
	if err != nil {
		return semdb.DerivedGraphScores{}, err
	}
	return semdb.DerivedGraphScores{Documents: docs, Anchors: anchors}, nil
}

type DerivedScoreWriter interface {
	ReplaceGraphDocScores(context.Context, []semdb.GraphDocScore) error
	ReplaceAnchorScores(context.Context, []semdb.AnchorScore) error
}

func PublishDerivedScores(ctx context.Context, writer DerivedScoreWriter, scores semdb.DerivedGraphScores) error {
	if err := writer.ReplaceGraphDocScores(ctx, scores.Documents); err != nil {
		return err
	}
	return writer.ReplaceAnchorScores(ctx, scores.Anchors)
}
