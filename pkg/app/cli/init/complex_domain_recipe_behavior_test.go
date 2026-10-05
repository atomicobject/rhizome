package init

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestComplexDomainRecipesExecutePopulatedTraceabilityFixture(t *testing.T) {
	root := installComplexDomainRecipeFixture(t)
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "recipe-behavior.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	vaultDef := obsidian.VaultDefinition{Name: "complex-domain-recipes", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, runtime.Ready)
	for _, path := range []string{
		"docs/reference/domain/contexts/README.md",
		"docs/reference/domain/types/README.md",
		"docs/reference/domain/processes/README.md",
		"docs/reference/domain/workflows/README.md",
		"docs/reference/requirements/sources/README.md",
		"docs/reference/requirements/requirements/README.md",
		"docs/reference/requirements/sources/shipping-policy.md",
		"docs/reference/requirements/sources/returns-policy.md",
		"docs/reference/domain/contexts/fulfillment.md",
		"docs/reference/domain/types/shipment.md",
		"docs/reference/domain/processes/ship-order.md",
		"docs/reference/domain/workflows/track-shipment.md",
		"docs/specs/product/shipping.md",
		"docs/specs/product/unlinked.md",
		"docs/reference/requirements/requirements/shipment-window.md",
		"docs/reference/requirements/requirements/shipment-alert.md",
	} {
		require.Emptyf(t, starterIssuesForPath(runtime.Issues, path, ""), "fixture note %s should satisfy the installed ontology", path)
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	recipes, loadIssues := queryrecipe.LoadDefaultSources(root)
	require.Empty(t, loadIssues)
	byID := make(map[string]queryrecipe.Recipe, len(recipes))
	for _, recipe := range recipes {
		byID[recipe.ID] = recipe
	}
	deps := ontologyquery.Deps{
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Service:    ontology.NewService(vaultDef, &obsidian.Note{}, store, runtime.Schema),
	}
	run := func(id string, inputs map[string]string) map[string]any {
		t.Helper()
		recipe, ok := byID[id]
		require.Truef(t, ok, "missing recipe %s", id)
		compiled, issues := queryrecipe.Compile(recipe, execSchema, inputs)
		require.Empty(t, issues)
		result := ontologyquery.ExecutePrepared(ctx, deps, runtime.Schema, execSchema, compiled.Prepared)
		require.Empty(t, result.Errors)
		return result.Data
	}

	t.Run("bounded inventory binds strings and integers and reports truncation", func(t *testing.T) {
		recipe := byID["domain-inventory-pack"]
		compiled, issues := queryrecipe.Compile(recipe, execSchema, map[string]string{
			"type":  "Requirement",
			"find":  "Shipment",
			"first": "1",
		})
		require.Empty(t, issues)
		require.Equal(t, "Requirement", compiled.Variables["type"])
		require.Equal(t, "Shipment", compiled.Variables["find"])
		require.EqualValues(t, 1, compiled.Variables["first"])

		result := ontologyquery.ExecutePrepared(ctx, deps, runtime.Schema, execSchema, compiled.Prepared)
		require.Empty(t, result.Errors)
		notes := result.Data["notes"].(map[string]any)
		require.Len(t, notes["nodes"].([]any), 1)
		pageInfo := notes["pageInfo"].(map[string]any)
		require.EqualValues(t, 1, pageInfo["requestedFirst"])
		require.EqualValues(t, 1, pageInfo["returnedCount"])
		require.Equal(t, true, pageInfo["truncated"])
		require.Empty(t, notes["warnings"].([]any))
	})

	t.Run("source context exposes identity and typed requirements", func(t *testing.T) {
		note := run("domain-context-pack", map[string]string{"path": "docs/reference/requirements/sources/shipping-policy.md"})["note"].(map[string]any)
		require.Equal(t, "SRC-0001", note["id"])
		require.Equal(t, "v2", note["sourceVersion"])
		require.Equal(t, "reviewed", note["reviewStatus"])
		require.Equal(t, "2026-09-01", note["lastExtracted"])
		require.ElementsMatch(t, []string{"REQ-0001", "REQ-0002"}, recipeNodeIDs(note["requirements"]))
	})

	t.Run("requirement trace returns usable spec identities", func(t *testing.T) {
		note := run("requirement-trace-pack", map[string]string{"path": "docs/reference/requirements/requirements/shipment-window.md"})["note"].(map[string]any)
		specs := note["specs"].([]any)
		require.Len(t, specs, 1)
		require.Equal(t, "docs/specs/product/shipping.md", specs[0].(map[string]any)["path"])
		require.Equal(t, "Shipping Product Spec", specs[0].(map[string]any)["title"])
	})

	t.Run("spec context separates incoming candidates and confirms typed specs", func(t *testing.T) {
		note := run("spec-domain-context-pack", map[string]string{"path": "docs/specs/product/shipping.md"})["note"].(map[string]any)
		require.Empty(t, note["linked"].([]any))
		candidates := note["incomingRequirementCandidates"].([]any)
		require.Len(t, candidates, 2)
		byRequirementID := recipeNodesByID(candidates)
		confirmedSpecs := byRequirementID["REQ-0001"]["specs"].([]any)
		require.Len(t, confirmedSpecs, 1)
		require.Equal(t, "docs/specs/product/shipping.md", confirmedSpecs[0].(map[string]any)["path"])
		require.Empty(t, byRequirementID["REQ-0002"]["specs"].([]any))

		unlinked := run("spec-domain-context-pack", map[string]string{"path": "docs/specs/product/unlinked.md"})["note"]
		require.NotNil(t, unlinked)
		require.Empty(t, unlinked.(map[string]any)["linked"].([]any))
		require.Empty(t, unlinked.(map[string]any)["incomingRequirementCandidates"].([]any))
		require.Nil(t, run("spec-domain-context-pack", map[string]string{"path": "docs/specs/product/missing.md"})["note"])
	})

	t.Run("authored story and criterion locators resolve", func(t *testing.T) {
		prepared, errors := ontologyquery.Prepare(execSchema, `{
  story: resolve(ref: "docs/specs/product/shipping.md#^SPEC-1001-US1") { found kind path resolvedType }
  criterion: resolve(ref: "docs/specs/product/shipping.md#^SPEC-1001-US1-AC1") { found kind path resolvedType }
}`)
		require.Empty(t, errors)
		result := ontologyquery.ExecutePrepared(ctx, deps, runtime.Schema, execSchema, prepared)
		require.Empty(t, result.Errors)
		require.Equal(t, true, result.Data["story"].(map[string]any)["found"])
		require.Equal(t, "UserStory", result.Data["story"].(map[string]any)["resolvedType"])
		require.Equal(t, true, result.Data["criterion"].(map[string]any)["found"])
		require.Equal(t, "AcceptanceCriterion", result.Data["criterion"].(map[string]any)["resolvedType"])
	})

	t.Run("requirement and source impact preserve evidence and conflicts", func(t *testing.T) {
		requirement := run("changed-domain-impact-pack", map[string]string{"path": "docs/reference/requirements/requirements/shipment-window.md"})["note"].(map[string]any)
		require.Equal(t, []any{"shipping-policy.md#window"}, requirement["sourceLocations"])
		require.Equal(t, "policy://shipping", requirement["sources"].([]any)[0].(map[string]any)["provenance"])
		require.Equal(t, []string{"REQ-0002"}, recipeNodeIDs(requirement["conflicts"]))

		source := run("changed-domain-impact-pack", map[string]string{"path": "docs/reference/requirements/sources/shipping-policy.md"})["note"].(map[string]any)
		impacted := recipeNodesByID(source["requirements"].([]any))["REQ-0001"]
		require.Equal(t, []any{"shipping-policy.md#window"}, impacted["sourceLocations"])
		require.Equal(t, "policy://shipping", impacted["sources"].([]any)[0].(map[string]any)["provenance"])
		require.Equal(t, []string{"REQ-0002"}, recipeNodeIDs(impacted["conflicts"]))
	})

	t.Run("changed domain families expose bounded requirement delivery context", func(t *testing.T) {
		for name, path := range map[string]string{
			"source":   "docs/reference/requirements/sources/shipping-policy.md",
			"context":  "docs/reference/domain/contexts/fulfillment.md",
			"type":     "docs/reference/domain/types/shipment.md",
			"process":  "docs/reference/domain/processes/ship-order.md",
			"workflow": "docs/reference/domain/workflows/track-shipment.md",
		} {
			t.Run(name, func(t *testing.T) {
				note := run("changed-domain-impact-pack", map[string]string{"path": path})["note"].(map[string]any)
				require.Contains(t, recipeNodeIDs(note["requirements"]), "REQ-0001")
				requirement := recipeNodesByID(note["requirements"].([]any))["REQ-0001"]
				require.Equal(t, []any{"docs/specs/product/shipping.md#^SPEC-1001-US1"}, requirement["storyRefs"])
				require.Equal(t, []any{"docs/specs/product/shipping.md#^SPEC-1001-US1-AC1"}, requirement["acceptanceCriterionRefs"])
				specs := requirement["specs"].([]any)
				require.Len(t, specs, 1)
				require.Equal(t, "docs/specs/product/shipping.md", specs[0].(map[string]any)["path"])
			})
		}
	})

	t.Run("row recipes honor bounds and expose completeness", func(t *testing.T) {
		for _, id := range []string{"feature-area-backlog-pack", "coverage-gap-pack"} {
			notes := run(id, map[string]string{"find": "Shipment", "first": "1"})["notes"].(map[string]any)
			require.Len(t, notes["nodes"].([]any), 1)
			require.Equal(t, true, notes["pageInfo"].(map[string]any)["truncated"])
			require.Empty(t, notes["warnings"].([]any))
		}
		notes := run("sources-needing-review-pack", map[string]string{"find": "Policy", "first": "1"})["notes"].(map[string]any)
		require.Len(t, notes["nodes"].([]any), 1)
		require.Equal(t, true, notes["pageInfo"].(map[string]any)["truncated"])
		require.Empty(t, notes["warnings"].([]any))

		noMatch := run("coverage-gap-pack", map[string]string{"find": "does-not-exist"})["notes"].(map[string]any)
		require.Empty(t, noMatch["nodes"].([]any))
		inventory := run("coverage-gap-pack", nil)["notes"].(map[string]any)
		require.Len(t, inventory["nodes"].([]any), 2)
	})
}

func installComplexDomainRecipeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Recipe fixture\n"), 0o644))
	require.NoError(t, Run(RunOptions{Dir: root, Workflow: templateComplexDomain, Stdout: io.Discard, Stderr: io.Discard}))

	files := map[string]string{
		"docs/reference/requirements/sources/shipping-policy.md": `---
id: SRC-0001
summary: Shipping Policy
source-kind: document
source-date: 2026-08-15
source-version: v2
provenance: policy://shipping
confidence: verified
review-status: reviewed
last-extracted: 2026-09-01
---
# Shipping Policy
`,
		"docs/reference/requirements/sources/returns-policy.md": `---
id: SRC-0002
summary: Returns Policy
source-kind: document
source-version: v1
review-status: needs_review
---
# Returns Policy
`,
		"docs/reference/domain/contexts/fulfillment.md": `---
id: CTX-0001
summary: Fulfillment
status: active
review-status: reviewed
---
# Fulfillment
`,
		"docs/reference/domain/types/shipment.md": `---
id: DT-0001
summary: Shipment
status: active
confidence: verified
review-status: reviewed
contexts:
  - docs/reference/domain/contexts/fulfillment.md
---
# Shipment
`,
		"docs/reference/domain/processes/ship-order.md": `---
id: PROC-0001
summary: Ship order
status: active
review-status: reviewed
contexts:
  - docs/reference/domain/contexts/fulfillment.md
---
# Ship Order
`,
		"docs/reference/domain/workflows/track-shipment.md": `---
id: WF-0001
summary: Track shipment
status: active
actors: [customer]
review-status: reviewed
processes:
  - docs/reference/domain/processes/ship-order.md
---
# Track Shipment
`,
		"docs/specs/product/shipping.md": `---
type: ProductSpec
id: SPEC-1001
summary: Shipping Product Spec
spec-status: active
last-updated: 2026-09-01
---
# Shipping Product Spec

## Summary

Show customers a promised shipment window.

## Goals

- Show the promised shipment window.

## Non-Goals

- Change carrier fulfillment.

## Requirements

The experience must show the promised window.

## User Stories

### US1 - See shipment window

- id:: ^SPEC-1001-US1
- summary:: Customers see the promised shipment window.
- status:: ready

As a customer, I want to see the promised shipment window.

#### Acceptance Criteria

- The shipment window is visible. ^SPEC-1001-US1-AC1
`,
		"docs/specs/product/unlinked.md": `---
type: ProductSpec
id: SPEC-1002
summary: Unlinked Product Spec
spec-status: active
last-updated: 2026-09-01
---
# Unlinked Product Spec

## Summary

An intentionally uncovered spec fixture.

## Goals

- Exercise resolved notes without upstream coverage.

## Non-Goals

- Define shipping behavior.

## Requirements

No upstream requirement is authored.

## User Stories

### US1 - Observe an unlinked spec

- id:: ^SPEC-1002-US1
- summary:: Readers observe an unlinked spec.
- status:: ready

#### Acceptance Criteria

- The spec resolves without upstream coverage. ^SPEC-1002-US1-AC1
`,
		"docs/reference/requirements/requirements/shipment-window.md": `---
id: REQ-0001
summary: Shipment window
status: accepted
kind: capability
priority: high
confidence: verified
review-status: reviewed
source-locations: [shipping-policy.md#window]
sources: [docs/reference/requirements/sources/shipping-policy.md]
contexts: [docs/reference/domain/contexts/fulfillment.md]
domain-types: [docs/reference/domain/types/shipment.md]
processes: [docs/reference/domain/processes/ship-order.md]
workflows: [docs/reference/domain/workflows/track-shipment.md]
specs: [docs/specs/product/shipping.md]
story-refs: [docs/specs/product/shipping.md#^SPEC-1001-US1]
acceptance-criterion-refs: [docs/specs/product/shipping.md#^SPEC-1001-US1-AC1]
conflicts: [docs/reference/requirements/requirements/shipment-alert.md]
---
# Shipment Window

Evidence: [[docs/reference/requirements/sources/shipping-policy]].
Context: [[docs/reference/domain/contexts/fulfillment]].
Type: [[docs/reference/domain/types/shipment]].
Process: [[docs/reference/domain/processes/ship-order]].
Workflow: [[docs/reference/domain/workflows/track-shipment]].
Delivery: [[docs/specs/product/shipping]].
`,
		"docs/reference/requirements/requirements/shipment-alert.md": `---
id: REQ-0002
summary: Shipment alert
status: candidate
kind: capability
priority: medium
confidence: source_backed
review-status: unreviewed
sources: [docs/reference/requirements/sources/shipping-policy.md]
---
# Shipment Alert

Evidence: [[docs/reference/requirements/sources/shipping-policy]].
Candidate discussion mentions [[docs/specs/product/shipping]].
`,
	}
	for path, content := range files {
		target := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, []byte(content), 0o644))
	}
	return root
}

func recipeNodeIDs(value any) []string {
	nodes, _ := value.([]any)
	ids := make([]string, 0, len(nodes))
	for _, value := range nodes {
		node, _ := value.(map[string]any)
		if id, _ := node["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func recipeNodesByID(nodes []any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(nodes))
	for _, value := range nodes {
		node := value.(map[string]any)
		out[node["id"].(string)] = node
	}
	return out
}
