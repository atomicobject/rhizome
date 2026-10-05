// Docs: [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md)
package graphalg

import "math"

// HITSResult contains the hub and authority scores from the HITS algorithm.
type HITSResult struct {
	Hubs        map[string]float64
	Authorities map[string]float64
}

// ComputeHITS computes HITS (Hyperlink-Induced Topic Search) scores.
//
// HITS assigns two scores per node:
// - Hub score: measures how well a node curates/aggregates links to good authorities
// - Authority score: measures how often a node is referenced by good hubs
//
// Algorithm:
// - Runs 30 iterations of alternating updates (authority then hub)
// - Normalizes scores via L2 norm after each iteration to prevent explosion
// - Initializes all scores to 1.0
//
// Hub and authority scores are mutually reinforcing: good hubs link to good authorities,
// and good authorities are linked to by good hubs. This makes HITS effective for finding
// "important" nodes in directed graphs (e.g., notes that link to many important notes).
//
// Used by ComputeDocScores to assign hub/authority scores to documents in the unified
// doc graph (notes + code files).
func ComputeHITS(adjacency map[string]map[string]struct{}) HITSResult {
	const iterations = 30

	n := len(adjacency)
	if n == 0 {
		return HITSResult{
			Hubs:        map[string]float64{},
			Authorities: map[string]float64{},
		}
	}

	// Build reverse adjacency for efficient authority computation:
	// reverse[dst] = set of nodes that link TO dst.
	reverse := make(map[string]map[string]struct{}, n)
	for node := range adjacency {
		reverse[node] = make(map[string]struct{})
	}
	for src, targets := range adjacency {
		for dst := range targets {
			if _, ok := reverse[dst]; !ok {
				reverse[dst] = make(map[string]struct{})
			}
			reverse[dst][src] = struct{}{}
		}
	}

	// Initialize scores.
	hub := make(map[string]float64, n)
	auth := make(map[string]float64, n)
	for node := range adjacency {
		hub[node] = 1.0
		auth[node] = 1.0
	}

	// Iterative refinement.
	for i := 0; i < iterations; i++ {
		// Update authority scores: auth(p) = sum of hub(q) for all q that link to p.
		newAuth := make(map[string]float64, n)
		for node := range adjacency {
			sum := 0.0
			for src := range reverse[node] {
				sum += hub[src]
			}
			newAuth[node] = sum
		}

		// Update hub scores: hub(p) = sum of auth(q) for all q that p links to.
		newHub := make(map[string]float64, n)
		for node, targets := range adjacency {
			sum := 0.0
			for dst := range targets {
				sum += newAuth[dst] // Use updated authority scores.
			}
			newHub[node] = sum
		}

		// Normalize to prevent score explosion.
		authNorm := 0.0
		hubNorm := 0.0
		for node := range adjacency {
			authNorm += newAuth[node] * newAuth[node]
			hubNorm += newHub[node] * newHub[node]
		}
		authNorm = math.Sqrt(authNorm)
		hubNorm = math.Sqrt(hubNorm)

		if authNorm > 0 {
			for node := range adjacency {
				newAuth[node] /= authNorm
			}
		}
		if hubNorm > 0 {
			for node := range adjacency {
				newHub[node] /= hubNorm
			}
		}

		auth = newAuth
		hub = newHub
	}

	return HITSResult{
		Hubs:        hub,
		Authorities: auth,
	}
}
