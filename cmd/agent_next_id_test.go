package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const nextIDSchemaSDL = `
type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
}

type EffortNote @node(paths: ["efforts/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "EFF")
}
`

func nextIDFixtureFiles() map[string]string {
	return map[string]string{
		".rhizome/ontology/schema.graphql": nextIDSchemaSDL,
		"specs/alpha.md": `---
type: Spec
summary: alpha spec
id: SPEC-0001
aliases:
  - SPEC-0001
---
# Alpha
`,
		"specs/beta.md": `---
type: Spec
summary: beta spec
id: SPEC-0007
aliases:
  - SPEC-0007
---
# Beta
`,
		"efforts/first.md": `---
type: EffortNote
summary: first effort
id: EFF-0002
aliases:
  - EFF-0002
---
# First
`,
	}
}

func TestAgentNextIDReturnsMaxPlusOne(t *testing.T) {
	for _, tc := range []struct {
		name, typeName, count string
		files                 map[string]string
		ids                   []string
		last                  string
		currentMax            int
	}{
		{"default", "Spec", "", nextIDFixtureFiles(), []string{"SPEC-0008"}, "SPEC-0008", 7},
		{"batch", "Spec", "3", nextIDFixtureFiles(), []string{"SPEC-0008", "SPEC-0009", "SPEC-0010"}, "SPEC-0010", 7},
		{"clamped", "Spec", "1000000", nextIDFixtureFiles(), nil, "SPEC-0107", 7},
		{"empty type", "EffortNote", "", map[string]string{".rhizome/ontology/schema.graphql": nextIDSchemaSDL, "efforts/none.md": "# Placeholder\n"}, []string{"EFF-0001"}, "EFF-0001", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := setupAgentTestVault(t, tc.files)
			args := []string{"agent", "next-id", "--vault", vault.name, "--type", tc.typeName}
			if tc.count != "" {
				args = append(args, "--count", tc.count)
			}
			stdout, stderr, err := runRootCLI(t, nil, args)
			require.NoError(t, err)
			require.Empty(t, stderr)
			var resp struct {
				Type            string   `json:"type"`
				Strategy        string   `json:"strategy"`
				Prefix          string   `json:"prefix"`
				Pad             int      `json:"pad"`
				Next            string   `json:"next"`
				IDs             []string `json:"ids"`
				Count           int      `json:"count"`
				Last            string   `json:"last"`
				CurrentMax      int      `json:"currentMax"`
				CurrentMaxValue string   `json:"currentMaxValue"`
				OwnersScanned   int      `json:"ownersScanned"`
				SharedWith      []string `json:"sharedWith"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(stdout), &raw))
			require.Contains(t, raw, "pad")
			require.Contains(t, raw, "currentMax")
			require.NotContains(t, raw, "paths")
			require.NotContains(t, raw, "allocations")
			require.Equal(t, tc.typeName, resp.Type)
			require.Equal(t, "SEQUENTIAL", resp.Strategy)
			require.Equal(t, 4, resp.Pad)
			require.Equal(t, tc.last, resp.Last)
			require.Equal(t, tc.currentMax, resp.CurrentMax)
			if tc.name == "clamped" {
				require.Len(t, resp.IDs, 100)
				require.Equal(t, 100, resp.Count)
				require.Equal(t, "SPEC-0008", resp.Next)
			} else {
				require.Equal(t, tc.ids, resp.IDs)
				require.Equal(t, len(tc.ids), resp.Count)
				require.Equal(t, tc.ids[0], resp.Next)
			}
			if tc.currentMax == 7 {
				require.Equal(t, "SPEC", resp.Prefix)
				require.Equal(t, "SPEC-0007", resp.CurrentMaxValue)
				require.Equal(t, 2, resp.OwnersScanned)
			} else {
				require.Equal(t, "EFF", resp.Prefix)
				require.Zero(t, resp.OwnersScanned)
			}
			require.Empty(t, resp.SharedWith)
		})
	}
}

func TestAgentNextIDReportsTypedErrorForUnknownType(t *testing.T) {
	legacy := map[string]string{
		".rhizome/ontology/schema.graphql": `type LegacySpec @node(paths: ["legacy/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true)
}`,
		"legacy/one.md": "---\ntype: LegacySpec\nsummary: legacy\nid: SPEC-0001\naliases:\n  - SPEC-0001\n---\n# Legacy\n",
	}
	datetime := map[string]string{
		".rhizome/ontology/schema.graphql": `type Effort @node(paths: ["efforts/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}`,
	}
	for _, tc := range []struct {
		name, typeName, code, strategy, count string
		files                                 map[string]string
		paths                                 []string
	}{
		{"unknown type", "DoesNotExist", "type_not_found", "", "", nextIDFixtureFiles(), nil},
		{"legacy identifier", "LegacySpec", "unsupported_for_type", "", "", legacy, nil},
		{"sequential paths", "Spec", "invalid_input", "SEQUENTIAL", "", nextIDFixtureFiles(), []string{"specs/2026-08-05-14-32-first.md", "specs/2026-08-05-14-32-second.md"}},
		{"datetime count", "Effort", "invalid_input", "DATETIME", "2", datetime, []string{"efforts/2026-08-05-14-32-example.md"}},
		{"malformed date", "Effort", "invalid_input", "DATETIME", "", datetime, []string{"efforts/2026-02-30-14-32-impossible.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := setupAgentTestVault(t, tc.files)
			args := []string{"agent", "next-id", "--vault", vault.name, "--type", tc.typeName}
			if tc.count != "" {
				args = append(args, "--count", tc.count)
			}
			for _, path := range tc.paths {
				args = append(args, "--path", path)
			}
			stdout, stderr, err := runRootCLI(t, nil, args)
			require.Error(t, err)
			require.Empty(t, stdout)
			var payload struct {
				Code     string   `json:"code"`
				Strategy string   `json:"strategy"`
				Paths    []string `json:"paths"`
			}
			require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
			require.Equal(t, tc.code, payload.Code)
			if tc.strategy != "" {
				require.Equal(t, tc.strategy, payload.Strategy)
			}
			if tc.paths != nil {
				require.Equal(t, tc.paths, payload.Paths)
			}
		})
	}
}

func TestAgentNextIDAllocatesDateTimePathsInInputOrder(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Effort @node(paths: ["efforts/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`,
		"efforts/existing.md": `---
type: Effort
summary: existing effort
id: EFF-2026-08-05-14-32
aliases:
  - EFF-2026-08-05-14-32
---
# Existing
`,
		"efforts/existing-second.md": `---
type: Effort
summary: existing second effort
id: EFF-2026-08-05-14-32-2
aliases:
  - EFF-2026-08-05-14-32-2
---
# Existing second
`,
	})
	paths := []string{
		"efforts/2026-08-05-14-32-second.md",
		"efforts/2026-08-05-14-32-first.md",
	}

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "next-id", "--vault", vault.name, "--type", "Effort",
		"--path", paths[0], "--path", paths[1],
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload struct {
		Strategy    string   `json:"strategy"`
		Next        string   `json:"next"`
		IDs         []string `json:"ids"`
		Count       int      `json:"count"`
		Last        string   `json:"last"`
		Paths       []string `json:"paths"`
		Allocations []struct {
			Path          string `json:"path"`
			ID            string `json:"id"`
			Base          string `json:"base"`
			Disambiguator *int   `json:"disambiguator"`
		} `json:"allocations"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.Equal(t, "DATETIME", payload.Strategy)
	require.Equal(t, "EFF-2026-08-05-14-32-3", payload.Next)
	require.Equal(t, []string{"EFF-2026-08-05-14-32-3", "EFF-2026-08-05-14-32-4"}, payload.IDs)
	require.Equal(t, 2, payload.Count)
	require.Equal(t, "EFF-2026-08-05-14-32-4", payload.Last)
	require.Equal(t, paths, payload.Paths)
	require.Len(t, payload.Allocations, 2)
	require.Equal(t, paths[0], payload.Allocations[0].Path)
	require.Equal(t, payload.IDs[0], payload.Allocations[0].ID)
	require.Equal(t, "EFF-2026-08-05-14-32", payload.Allocations[0].Base)
	require.Equal(t, 3, *payload.Allocations[0].Disambiguator)
	require.Equal(t, paths[1], payload.Allocations[1].Path)
	require.Equal(t, payload.IDs[1], payload.Allocations[1].ID)
	require.Equal(t, 4, *payload.Allocations[1].Disambiguator)
}
