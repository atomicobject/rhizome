package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestExecute_NodeWorkspaceNavigationPreservesRoleLabelsMatchingFilenames(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/Overview.md"]) {
  plan: Material! @link @workspaceMember(label: "Plan")
}
type Material @node(paths: ["notes/Plan.md"]) {
  summary: String
}
`, map[string]string{
		"notes/Overview.md": "---\nplan: '[[Plan]]'\n---\n# Delivery workspace\n",
		"notes/Plan.md":     "# Approved implementation details\n",
	})
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace {
    relationGroups { navigation ownerTitle items { title targetTitle } }
  } }
}`, map[string]any{"ref": "notes/Overview.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	require.Len(t, groups, 1)
	group := groups[0].(map[string]any)
	require.Equal(t, true, group["navigation"])
	require.Equal(t, "Delivery workspace", group["ownerTitle"])
	items := group["items"].([]any)
	require.Len(t, items, 2)
	require.Equal(t, "Overview", items[0].(map[string]any)["title"])
	require.Equal(t, "Delivery workspace", items[0].(map[string]any)["targetTitle"])
	require.Equal(t, "Plan", items[1].(map[string]any)["title"])
	require.Equal(t, "Approved implementation details", items[1].(map[string]any)["targetTitle"])
}

func TestExecute_NodeWorkspaceNavigationFiltersBeforeOwnerLimit(t *testing.T) {
	notes := map[string]string{
		"notes/z-workspaces/valid.md": "---\nplan: '[[../materials/shared]]'\n---\n# Valid effort\n",
		"notes/materials/shared.md":   "# Shared material\n",
		"notes/materials/other.md":    "# Other material\n",
	}
	for i := 0; i < workspaceNavigationMaxOwnerRelations+5; i++ {
		path := fmt.Sprintf("notes/a-noise-workspaces/noise-%03d.md", i)
		notes[path] = fmt.Sprintf("---\nplan: '[[../materials/shared]]'\nmaterials:\n  - '[[../materials/other]]'\n---\n# Noise %03d\n", i)
	}

	env := newCustomQueryTestEnv(t, `
type PlanWorkspace @node(paths: ["notes/z-workspaces/*.md"]) {
  plan: Material! @link @workspaceMember(label: "Plan")
}

type MaterialsWorkspace @node(paths: ["notes/a-noise-workspaces/*.md"]) {
  plan: Material! @link
  materials: [Material!] @link @workspaceMember
}

type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation ownerTitle items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	groups := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["relationGroups"].([]any)
	require.Condition(t, func() bool {
		for _, raw := range groups {
			group := raw.(map[string]any)
			if group["navigation"] == true && group["ownerTitle"] == "Valid effort" {
				return true
			}
		}
		return false
	}, "unannotated relations on another workspace type cannot consume the owner limit")
}

func TestExecute_NodeWorkspaceNavigationBatchesManyOwners(t *testing.T) {
	const ownerCount = 24
	notes := map[string]string{"notes/materials/shared.md": "# Shared plan\n"}
	for i := 0; i < ownerCount; i++ {
		notes[fmt.Sprintf("notes/workspaces/owner-%02d.md", i)] = fmt.Sprintf("---\nmember: '[[../materials/shared]]'\n---\n# Owner %02d\n", i)
	}
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) {
  member: Material! @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	spy := &workspaceNavigationSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation ownerTitle items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	groups := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["relationGroups"].([]any)
	var navigationCount int
	for _, raw := range groups {
		if raw.(map[string]any)["navigation"] == true {
			navigationCount++
		}
	}
	require.Equal(t, ownerCount, navigationCount)
	require.Equal(t, 2, spy.memberCalls, "owner discovery and member expansion each use one relation read")
	require.Equal(t, ownerCount, spy.maxMemberSources, "all owners expand through one batched request")
}

func TestExecute_NodeWorkspaceNavigationReportsMemberTruncation(t *testing.T) {
	notes := make(map[string]string)
	var owner strings.Builder
	owner.WriteString("---\nmembers:\n")
	for i := 0; i <= workspaceNavigationMaxMemberRelationsPerOwner; i++ {
		owner.WriteString(fmt.Sprintf("  - '[[../materials/member-%03d]]'\n", i))
		notes[fmt.Sprintf("notes/materials/member-%03d.md", i)] = fmt.Sprintf("# Member %03d\n", i)
	}
	owner.WriteString("---\n# Large workspace\n")
	notes["notes/workspaces/large.md"] = owner.String()
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) {
  members: [Material!] @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation items { title } } } }
}`, map[string]any{"ref": "notes/workspaces/large.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "more than 200 outbound membership relations")
}

func TestExecute_NodeWorkspaceNavigationReportsOwnerTruncation(t *testing.T) {
	notes := map[string]string{"notes/materials/shared.md": "# Shared plan\n"}
	for i := 0; i <= workspaceNavigationMaxOwnerRelations; i++ {
		notes[fmt.Sprintf("notes/workspaces/owner-%03d.md", i)] = fmt.Sprintf("---\nmember: '[[../materials/shared]]'\n---\n# Owner %03d\n", i)
	}
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) {
  member: Material! @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "more than 200 inbound membership relations")
}

func TestExecute_NodeWorkspaceNavigationAllowsExactOwnerLimitBeforeEmptyTypeBatch(t *testing.T) {
	notes := map[string]string{
		"notes/materials/shared.md":   "# Shared plan\n",
		"notes/materials/other.md":    "# Other plan\n",
		"notes/z-workspaces/empty.md": "---\nlog: '[[../materials/other]]'\n---\n# Unrelated workspace\n",
	}
	for i := 0; i < workspaceNavigationMaxOwnerRelations; i++ {
		notes[fmt.Sprintf("notes/a-workspaces/owner-%03d.md", i)] = fmt.Sprintf("---\nplan: '[[../materials/shared]]'\n---\n# Owner %03d\n", i)
	}
	env := newCustomQueryTestEnv(t, `
type AWorkspace @node(paths: ["notes/a-workspaces/*.md"]) {
  plan: Material! @link @workspaceMember
}
type ZWorkspace @node(paths: ["notes/z-workspaces/*.md"]) {
  log: Material! @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	groups := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["relationGroups"].([]any)
	var navigationCount int
	for _, raw := range groups {
		if raw.(map[string]any)["navigation"] == true {
			navigationCount++
		}
	}
	require.Equal(t, workspaceNavigationMaxOwnerRelations, navigationCount)
}

func TestWorkspaceNavigationPromotionPreservesFullNodeIdentity(t *testing.T) {
	promoted := ontology.NodeRef{NotePath: "notes/material.md", Fragment: "shared", NodeID: "member", Kind: ontology.NodeKindEmbedded}
	unrelated := promoted
	unrelated.NodeID = "other"
	refs := workspaceNavigationRefSet([]workspaceRelationGroup{{Items: []workspaceRelationItem{{Ref: promoted}}}})
	require.True(t, workspaceNavigationContainsRef(refs, promoted))
	require.False(t, workspaceNavigationContainsRef(refs, unrelated), "matching author-facing locators do not collapse distinct embedded nodes")
}

type workspaceNavigationSpyStore struct {
	*codeanchorsqlite.Store
	memberCalls      int
	maxMemberSources int
}

func (s *workspaceNavigationSpyStore) OntologyNeighborhood(ctx context.Context, request readmodel.NeighborhoodQuery) ([]codeanchorsqlite.OntologyEdgeRow, error) {
	for _, relation := range request.Relations {
		if relation == "member" {
			s.memberCalls++
			if len(request.Sources) > s.maxMemberSources {
				s.maxMemberSources = len(request.Sources)
			}
			break
		}
	}
	return s.Store.OntologyNeighborhood(ctx, request)
}
