package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEdgeWeight(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		count    int
		expected float64
	}{
		// Doc-domain edges (no clamp)
		{"wikilink count 1", "wikilink", 1, 1.0},
		{"wikilink count 5", "wikilink", 5, 5.0},
		{"coderef count 1", "coderef", 1, 1.0},
		{"mentions count 1", "mentions", 1, 1.0},

		// Code-domain edges (differentiated weights, varying clamps)
		{"calls count 1", "calls", 1, 0.25},
		{"calls count 5", "calls", 5, 1.25},
		{"calls count 10 (clamped)", "calls", 10, 1.25}, // clamped to 5
		{"type_ref count 1", "type_ref", 1, 0.15},
		{"type_ref count 4", "type_ref", 4, 0.60},
		{"type_ref count 10 (clamped)", "type_ref", 10, 0.60}, // clamped to 4
		{"member_ref count 1", "member_ref", 1, 0.12},
		{"member_ref count 4", "member_ref", 4, 0.48},
		{"member_ref count 10 (clamped)", "member_ref", 10, 0.48}, // clamped to 4
		{"imports count 1", "imports", 1, 0.10},
		{"imports count 3", "imports", 3, 0.30},
		{"imports count 5 (clamped)", "imports", 5, 0.30}, // clamped to 3
		{"tests count 1", "tests", 1, 0.08},
		{"tests count 3", "tests", 3, 0.24},
		{"tests count 5 (clamped)", "tests", 5, 0.24}, // clamped to 3

		// Unknown edge kind
		{"unknown kind", "unknown", 1, 0},

		// Case insensitive
		{"uppercase CALLS", "CALLS", 1, 0.25},
		{"mixed case Wikilink", "Wikilink", 1, 1.0},
		{"with spaces", "  calls  ", 1, 0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EdgeWeight(tt.kind, tt.count)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestIsCodeRefEdge(t *testing.T) {
	tests := []struct {
		kind     string
		expected bool
	}{
		{"calls", true},
		{"type_ref", true},
		{"member_ref", true},
		{"imports", true},
		{"tests", true},
		{"wikilink", false},
		{"coderef", false},
		{"mentions", false},
		{"unknown", false},
		{"CALLS", true},     // case insensitive
		{"  tests  ", true}, // trimmed
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsCodeRefEdge(tt.kind))
		})
	}
}

func TestEdgePriority(t *testing.T) {
	tests := []struct {
		kind     string
		expected int
	}{
		{"wikilink", 1},
		{"coderef", 2},
		{"mentions", 3},
		{"calls", 4},
		{"type_ref", 5},
		{"member_ref", 6},
		{"imports", 7},
		{"tests", 8},
		{"unknown", 999},
		{"WIKILINK", 1}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			assert.Equal(t, tt.expected, EdgePriority(tt.kind))
		})
	}
}

func TestIsDocDomainEdge(t *testing.T) {
	tests := []struct {
		kind     string
		expected bool
	}{
		{"wikilink", true},
		{"coderef", true},
		{"mentions", true},
		{"calls", false},
		{"imports", false},
		{"tests", false},
		{"unknown", false},
		{"MENTIONS", true}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsDocDomainEdge(tt.kind))
		})
	}
}
