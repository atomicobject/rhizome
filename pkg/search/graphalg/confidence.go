package graphalg

import (
	"math"
	"strings"
)

// ConfidenceValue normalizes an edge confidence into a 0..1 score.
// Explicit numeric scores win; otherwise we fall back to the confidence label.
func ConfidenceValue(confidence string, score float64) float64 {
	if score > 0 && !math.IsNaN(score) && !math.IsInf(score, 0) {
		if score > 1 {
			return 1
		}
		return score
	}

	switch strings.ToLower(strings.TrimSpace(confidence)) {
	case "inferred":
		return 0.7
	case "ambiguous":
		return 0.2
	default:
		return 1
	}
}

// ConfidenceCost returns a non-negative edge cost where higher confidence is cheaper.
func ConfidenceCost(confidence string, score float64) float64 {
	return 1 - ConfidenceValue(confidence, score)
}

// ConfidenceSurpriseMultiplier returns a positive factor where lower confidence is more surprising.
func ConfidenceSurpriseMultiplier(confidence string, score float64) float64 {
	return 1 + (1-ConfidenceValue(confidence, score))*2
}
