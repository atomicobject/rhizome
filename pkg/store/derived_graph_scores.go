package store

// DerivedGraphScores publishes both graph score families at one writer barrier.
type DerivedGraphScores struct {
	Documents []GraphDocScore
	Anchors   []AnchorScore
}
