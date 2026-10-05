package perfworkload

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const ManifestVersion = 1

type Manifest struct {
	Version    int               `json:"version"`
	Seed       int64             `json:"seed"`
	Owners     OwnerManifest     `json:"owners"`
	Graph      GraphManifest     `json:"graph"`
	Embeddings EmbeddingManifest `json:"embeddings"`
	Queries    []Query           `json:"queries"`
	Protocol   Protocol          `json:"protocol"`
}

type OwnerManifest struct {
	Total          int `json:"total"`
	Code           int `json:"code"`
	Prose          int `json:"prose"`
	ChunksPerOwner int `json:"chunksPerOwner"`
}

type GraphManifest struct {
	EdgesPerCodeOwner int `json:"edgesPerCodeOwner"`
	EdgesPerNoteOwner int `json:"edgesPerNoteOwner"`
}

type EmbeddingManifest struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
}

type Query struct {
	ID           string   `json:"id"`
	Text         string   `json:"text"`
	Intent       string   `json:"intent"`
	LaneMode     string   `json:"laneMode"`
	Seeds        []string `json:"seeds,omitempty"`
	Types        []string `json:"types,omitempty"`
	PathPrefixes []string `json:"pathPrefixes,omitempty"`
	Pack         bool     `json:"pack"`
	RequireBody  bool     `json:"requireBody,omitempty"`
}

type Protocol struct {
	Warmups       int   `json:"warmups"`
	WarmSamples   int   `json:"warmSamples"`
	ColdSamples   int   `json:"coldSamples"`
	Concurrencies []int `json:"concurrencies"`
}

func LoadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(b)
}

// ParseManifest decodes and validates one captured manifest snapshot.
func ParseManifest(b []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported workload manifest version %d", m.Version)
	}
	if m.Owners.Total < 5_000 || m.Owners.Code+m.Owners.Prose != m.Owners.Total {
		return errors.New("workload requires at least 5,000 owners with code and prose totals matching total")
	}
	if m.Owners.Code == 0 || m.Owners.Prose == 0 || m.Owners.ChunksPerOwner*m.Owners.Total < 50_000 {
		return errors.New("workload requires code, prose, and at least 50,000 searchable chunks")
	}
	if m.Graph.EdgesPerCodeOwner < 1 || m.Graph.EdgesPerNoteOwner < 1 {
		return errors.New("workload graph density must be positive")
	}
	if !strings.EqualFold(strings.TrimSpace(m.Embeddings.Provider), "deterministic") || m.Embeddings.Dimensions <= 0 {
		return errors.New("frozen scale workload requires a deterministic provider with positive dimensions")
	}
	if len(m.Queries) == 0 {
		return errors.New("workload query mix is empty")
	}
	seen := map[string]struct{}{}
	for _, query := range m.Queries {
		if strings.TrimSpace(query.ID) == "" || strings.TrimSpace(query.Text) == "" {
			return errors.New("every workload query requires id and text")
		}
		if _, ok := seen[query.ID]; ok {
			return fmt.Errorf("duplicate workload query id %q", query.ID)
		}
		seen[query.ID] = struct{}{}
		if query.LaneMode != "lexical" && query.LaneMode != "hybrid" && query.LaneMode != "graph" {
			return fmt.Errorf("workload query %q requires laneMode lexical, hybrid, or graph", query.ID)
		}
		if query.LaneMode == "graph" && len(query.Seeds) == 0 {
			return fmt.Errorf("graph workload query %q requires a seed", query.ID)
		}
	}
	if m.Protocol.Warmups < 20 || m.Protocol.WarmSamples < 200 || m.Protocol.ColdSamples < 30 {
		return errors.New("protocol requires at least 20 warmups, 200 warm samples, and 30 cold samples")
	}
	if !containsInt(m.Protocol.Concurrencies, 1) || !containsInt(m.Protocol.Concurrencies, 4) {
		return errors.New("protocol requires concurrency 1 and 4")
	}
	return nil
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
