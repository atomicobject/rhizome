package graphdb

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

// DerivedScores separates graph computation from its durable publication.
// Runtime callers compute outside the vault lease; batch callers choose their
// throughput policy while using the same graph processing.
type DerivedScores struct {
	Documents []semdb.GraphDocScore
	Anchors   []semdb.AnchorScore
}

func ComputeDerivedScores(ctx context.Context, store *semdb.Store, opts DocScoresOptions) (DerivedScores, error) {
	docs, err := ComputeDocScores(ctx, store, opts)
	if err != nil {
		return DerivedScores{}, err
	}
	anchors, err := ComputeAnchorPageRank(ctx, store)
	if err != nil {
		return DerivedScores{}, err
	}
	return DerivedScores{Documents: docs, Anchors: anchors}, nil
}

type DerivedScoreWriter interface {
	ReplaceGraphDocScores(context.Context, []semdb.GraphDocScore) error
	ReplaceAnchorScores(context.Context, []semdb.AnchorScore) error
}

func PublishDerivedScores(ctx context.Context, writer DerivedScoreWriter, scores DerivedScores) error {
	if err := writer.ReplaceGraphDocScores(ctx, scores.Documents); err != nil {
		return err
	}
	return writer.ReplaceAnchorScores(ctx, scores.Anchors)
}
