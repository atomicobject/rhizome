package identifierreconcile

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology"
	identifierrewrite "github.com/atomicobject/rhizome/pkg/validate/identifierreconcile/rewrite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestIdentifierLinkDiscoveryCanonicalSnapshotIsDeterministicAndDetached(t *testing.T) {
	notes := [][2]string{
		{"specs/a.md", "---\nid: SPEC-0001\naliases: [OLD-A]\n---\n"},
		{"specs/b.md", "---\nid: SPEC-0002\naliases: [OLD-B]\n---\n"},
		{"notes/inbound-a.md", "[[OLD-A]]\n"},
		{"notes/inbound-b.md", "[[OLD-B|shown]] Review OLD-A in prose.\n"},
		{"notes/zero.md", "plain prose\n"},
	}
	rewriteFor := func(notePath, oldID, newID string) identifierrewrite.IdentifierRewrite {
		ref := ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindNote, TypeName: "Spec"}
		return identifierrewrite.IdentifierRewrite{
			Mode: identifierrewrite.IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref,
			OldIdentifier: oldID, NewIdentifier: newID, PreferredField: "id", AliasesField: "aliases",
		}
	}
	first, second := rewriteFor("specs/a.md", "OLD-A", "SPEC-0001"), rewriteFor("specs/b.md", "OLD-B", "SPEC-0002")
	discover := func(order [][2]string, rewrites []identifierrewrite.IdentifierRewrite) *IdentifierLinkDiscovery {
		t.Helper()
		root, _ := identifierLinkDiscoveryFixture(t, nil)
		for _, note := range order {
			absolute := filepath.Join(root, filepath.FromSlash(note[0]))
			require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
			require.NoError(t, os.WriteFile(absolute, []byte(note[1]), 0o644))
		}
		fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{NoteMetadata: testNoteMetadata(t), VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: rewrites})
		require.NoError(t, err)
		links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
		require.NoError(t, err)
		return links
	}
	reversed := append([][2]string(nil), notes...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	forward := discover(notes, []identifierrewrite.IdentifierRewrite{first, second})
	reverse := discover(reversed, []identifierrewrite.IdentifierRewrite{second, first})

	forwardSnapshot, err := forward.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{second, first})
	require.NoError(t, err)
	reverseSnapshot, err := reverse.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{first, second})
	require.NoError(t, err)
	require.Equal(t, forward.sealed, reverse.sealed)
	require.Equal(t, forwardSnapshot, reverseSnapshot)
	wantPaths := []string{"notes/inbound-a.md", "notes/inbound-b.md", "notes/zero.md", "specs/a.md", "specs/b.md"}
	require.Equal(t, wantPaths, linkSourcePaths(forwardSnapshot.SourcePreconditions))
	require.Equal(t, wantPaths, linkPlanPaths(forwardSnapshot.Plans))
	require.Len(t, forwardSnapshot.ReviewPlans, len(wantPaths))
	require.Equal(t, sourcePreconditionFingerprint(forwardSnapshot.SourcePreconditions), forwardSnapshot.SourceFingerprint)
	inboundB := structuredLinkPlanByPath(t, forwardSnapshot.Plans, "notes/inbound-b.md")
	require.Len(t, inboundB.Edits, 1)
	reviewB := identifierReviewPlanByPath(t, forwardSnapshot.ReviewPlans, "notes/inbound-b.md")
	require.Len(t, reviewB.Candidates, 1)

	// Mutating every nested level of a returned snapshot must not reach the
	// sealed discovery that produced it.
	pristine, err := forward.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{first, second})
	require.NoError(t, err)
	forwardSnapshot.SourcePreconditions[0].SourceHash = "mutated"
	forwardSnapshot.Plans[0].NotePath = "notes/mutated.md"
	forwardSnapshot.Plans[1].Edits[0].Replacement = "MUTATED"
	forwardSnapshot.ReviewPlans[1].Candidates[0].Value = "MUTATED"
	forwardSnapshot.ReviewPlans[1].Candidates[0].Rewrites[0].NewIdentifier = "MUTATED"
	forwardSnapshot.SchemaHash = "mutated"
	again, err := forward.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{first, second})
	require.NoError(t, err)
	require.Equal(t, pristine, again)
}

func TestIdentifierLinkDiscoveryRejectsMissingUnsealedMutatedAndRewriteMismatch(t *testing.T) {
	rewrite := linkDiscoveryRewrite("docs/a.md", "docs/A-2-a.md", "A-1", "A-2")
	content := "[[a|A-1]]"
	plan := linkDiscoveryPlan(t, "docs/inbound.md", content, []identifierrewrite.IdentifierRewrite{rewrite}, rewrite.OldRef)
	sources := []SourcePrecondition{{NotePath: plan.NotePath, SourceHash: plan.SourceFingerprint}}

	var missing *IdentifierLinkDiscovery
	_, err := missing.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "complete identifier link discovery")
	_, err = (&IdentifierLinkDiscovery{}).validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "complete identifier link discovery")

	discovery := sealIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, sources, []identifierrewrite.StructuredLinkRewritePlan{plan})
	_, err = discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{linkDiscoveryRewrite("docs/b.md", "docs/B-2-b.md", "B-1", "B-2")})
	require.ErrorContains(t, err, "rewrite union")

	discovery.sourcePreconditions[0].SourceHash = markdownsyntax.StructuredLinkSourceFingerprint("different")
	_, err = discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "source fingerprint")

	discovery = sealIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, sources, []identifierrewrite.StructuredLinkRewritePlan{plan})
	discovery.schemaHash = "mutated-schema"
	_, err = discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "changed after sealing")

	emptySchema := sealRawIdentifierLinkDiscoveryWithSchemaForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, sources, []identifierrewrite.StructuredLinkRewritePlan{plan}, "")
	_, err = emptySchema.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "requires schema hash")
}

func TestIdentifierLinkDiscoveryRejectsInvalidSourceInventories(t *testing.T) {
	rewrite := linkDiscoveryRewrite("docs/a.md", "docs/A-2-a.md", "A-1", "A-2")
	hashA := markdownsyntax.StructuredLinkSourceFingerprint("a")
	hashB := markdownsyntax.StructuredLinkSourceFingerprint("b")
	tests := []struct {
		name    string
		sources []SourcePrecondition
		want    string
	}{
		{name: "duplicate", sources: []SourcePrecondition{{NotePath: "docs/a.md", SourceHash: hashA}, {NotePath: "docs/a.md", SourceHash: hashA}}, want: "duplicated"},
		{name: "conflicting", sources: []SourcePrecondition{{NotePath: "docs/a.md", SourceHash: hashA}, {NotePath: "docs/a.md", SourceHash: hashB}}, want: "conflicting"},
		{name: "not canonical", sources: []SourcePrecondition{{NotePath: "docs/../a.md", SourceHash: hashA}}, want: "canonical vault-relative"},
		{name: "malformed hash", sources: []SourcePrecondition{{NotePath: "docs/a.md", SourceHash: "not-a-sha256"}}, want: "SHA-256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discovery := sealRawIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, tt.sources, nil)
			_, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestIdentifierLinkDiscoveryRejectsInvalidOrMismatchedPlans(t *testing.T) {
	rewrite := linkDiscoveryRewrite("docs/a.md", "docs/A-2-a.md", "A-1", "A-2")
	content := "[[a|A-1]]"
	plan := linkDiscoveryPlan(t, "docs/inbound.md", content, []identifierrewrite.IdentifierRewrite{rewrite}, rewrite.OldRef)
	sources := []SourcePrecondition{{NotePath: plan.NotePath, SourceHash: plan.SourceFingerprint}}

	t.Run("absent source path", func(t *testing.T) {
		discovery := sealRawIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, nil, []identifierrewrite.StructuredLinkRewritePlan{plan})
		_, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
		require.ErrorContains(t, err, "absent from complete source inventory")
	})
	t.Run("duplicate path", func(t *testing.T) {
		discovery := sealRawIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, sources, []identifierrewrite.StructuredLinkRewritePlan{plan, plan})
		_, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
		require.ErrorContains(t, err, "duplicate plan")
	})
	t.Run("source mismatch", func(t *testing.T) {
		mismatched := []SourcePrecondition{{NotePath: plan.NotePath, SourceHash: markdownsyntax.StructuredLinkSourceFingerprint("new content")}}
		discovery := sealRawIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, mismatched, []identifierrewrite.StructuredLinkRewritePlan{plan})
		_, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
		require.ErrorContains(t, err, "does not match complete source inventory")
	})
	t.Run("unsealed", func(t *testing.T) {
		discovery := sealRawIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, sources, []identifierrewrite.StructuredLinkRewritePlan{{NotePath: plan.NotePath, SourceFingerprint: plan.SourceFingerprint}})
		_, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
		require.ErrorContains(t, err, "structured link rewrite plan changed")
	})
	t.Run("mutated sealed plan", func(t *testing.T) {
		discovery := sealIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, sources, []identifierrewrite.StructuredLinkRewritePlan{plan})
		discovery.plans[0].SourceFingerprint = markdownsyntax.StructuredLinkSourceFingerprint("forged")
		_, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
		require.ErrorContains(t, err, "structured link rewrite plan changed")
	})
	t.Run("unrelated diagnostic", func(t *testing.T) {
		other := linkDiscoveryRewrite("docs/other.md", "docs/OTHER-2-other.md", "OTHER-1", "OTHER-2")
		otherContent := "[[OTHER-1]]"
		scan := markdownsyntax.ScanStructuredLinkSnapshot(otherContent)
		diagnosticPlan := identifierrewrite.PlanStructuredLinkRewrites(identifierrewrite.StructuredLinkRewriteInput{
			NotePath: "docs/diagnostic.md", Content: otherContent, LinkSnapshot: &scan, Rewrites: []identifierrewrite.IdentifierRewrite{other},
		})
		require.NotEmpty(t, diagnosticPlan.Diagnostics)
		diagnosticSources := []SourcePrecondition{{NotePath: diagnosticPlan.NotePath, SourceHash: diagnosticPlan.SourceFingerprint}}
		discovery := sealIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, diagnosticSources, []identifierrewrite.StructuredLinkRewritePlan{diagnosticPlan})
		_, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
		require.ErrorContains(t, err, "diagnostic does not match rewrite union")
	})
}

func TestIdentifierLinkDiscoverySealsReviewPlansToSchemaSourceAndRewriteUnion(t *testing.T) {
	rewrite := linkDiscoveryRewrite("docs/a.md", "docs/A-2-a.md", "A-1", "A-2")
	content := "Mention A-1."
	scan := markdownsyntax.ScanStructuredLinkSnapshot(content)
	reviewPlan := identifierrewrite.PlanIdentifierReviewCandidates(identifierrewrite.IdentifierReviewCandidateInput{
		NotePath: "docs/review.md", Content: content, LinkSnapshot: &scan, Rewrites: []identifierrewrite.IdentifierRewrite{rewrite},
	})
	sources := []SourcePrecondition{{NotePath: reviewPlan.NotePath, SourceHash: reviewPlan.SourceFingerprint}}
	discovery := sealIdentifierLinkDiscoveryForTest(t, []identifierrewrite.IdentifierRewrite{rewrite}, sources, nil, []identifierrewrite.IdentifierReviewCandidatePlan{reviewPlan})

	snapshot, err := discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
	require.NoError(t, err)
	require.Len(t, snapshot.ReviewPlans, 1)
	require.Equal(t, "test-schema-v1", snapshot.SchemaHash)
	require.Len(t, snapshot.ReviewPlans[0].Candidates[0].Rewrites, 1)

	discovery.reviewPlans[0].Candidates[0].Value = "forged"
	_, err = discovery.validatedSnapshotFor([]identifierrewrite.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "changed after planning")
}

func sealIdentifierLinkDiscoveryForTest(t *testing.T, rewrites []identifierrewrite.IdentifierRewrite, sources []SourcePrecondition, plans []identifierrewrite.StructuredLinkRewritePlan, reviewPlanGroups ...[]identifierrewrite.IdentifierReviewCandidatePlan) *IdentifierLinkDiscovery {
	t.Helper()
	canonicalSources := append([]SourcePrecondition(nil), sources...)
	sort.Slice(canonicalSources, func(i, j int) bool { return canonicalSources[i].NotePath < canonicalSources[j].NotePath })
	canonicalPlans := append([]identifierrewrite.StructuredLinkRewritePlan(nil), plans...)
	sort.Slice(canonicalPlans, func(i, j int) bool { return canonicalPlans[i].NotePath < canonicalPlans[j].NotePath })
	var reviewPlans []identifierrewrite.IdentifierReviewCandidatePlan
	if len(reviewPlanGroups) > 0 {
		reviewPlans = append([]identifierrewrite.IdentifierReviewCandidatePlan(nil), reviewPlanGroups[0]...)
		sort.Slice(reviewPlans, func(i, j int) bool { return reviewPlans[i].NotePath < reviewPlans[j].NotePath })
	}
	return sealRawIdentifierLinkDiscoveryWithMembersForTest(t, rewrites, canonicalSources, canonicalPlans, reviewPlans, "test-schema-v1")
}

func sealRawIdentifierLinkDiscoveryForTest(t *testing.T, rewrites []identifierrewrite.IdentifierRewrite, sources []SourcePrecondition, plans []identifierrewrite.StructuredLinkRewritePlan) *IdentifierLinkDiscovery {
	return sealRawIdentifierLinkDiscoveryWithSchemaForTest(t, rewrites, sources, plans, "test-schema-v1")
}

func sealRawIdentifierLinkDiscoveryWithSchemaForTest(t *testing.T, rewrites []identifierrewrite.IdentifierRewrite, sources []SourcePrecondition, plans []identifierrewrite.StructuredLinkRewritePlan, schemaHash string) *IdentifierLinkDiscovery {
	return sealRawIdentifierLinkDiscoveryWithMembersForTest(t, rewrites, sources, plans, nil, schemaHash)
}

func sealRawIdentifierLinkDiscoveryWithMembersForTest(t *testing.T, rewrites []identifierrewrite.IdentifierRewrite, sources []SourcePrecondition, plans []identifierrewrite.StructuredLinkRewritePlan, reviewPlans []identifierrewrite.IdentifierReviewCandidatePlan, schemaHash string) *IdentifierLinkDiscovery {
	t.Helper()
	canonical, err := canonicalRepairRewriteSet(rewrites)
	require.NoError(t, err)
	rewriteFingerprint, err := repairRewriteSetFingerprint(canonical)
	require.NoError(t, err)
	snapshot := identifierLinkDiscoverySnapshot{
		SourceFingerprint:   sourcePreconditionFingerprint(sources),
		SourcePreconditions: append([]SourcePrecondition(nil), sources...),
		SchemaHash:          schemaHash,
		RewriteFingerprint:  rewriteFingerprint,
		Plans:               append([]identifierrewrite.StructuredLinkRewritePlan(nil), plans...),
		ReviewPlans:         append([]identifierrewrite.IdentifierReviewCandidatePlan(nil), reviewPlans...),
	}
	sealed, err := identifierLinkDiscoveryFingerprint(snapshot)
	require.NoError(t, err)
	return &IdentifierLinkDiscovery{
		sourceFingerprint: snapshot.SourceFingerprint, sourcePreconditions: snapshot.SourcePreconditions,
		schemaHash: snapshot.SchemaHash, rewriteFingerprint: snapshot.RewriteFingerprint, plans: snapshot.Plans, reviewPlans: snapshot.ReviewPlans, sealed: sealed,
	}
}

func linkDiscoveryRewrite(oldPath, newPath, oldID, newID string) identifierrewrite.IdentifierRewrite {
	return identifierrewrite.IdentifierRewrite{
		Mode:   identifierrewrite.IdentifierRewritePreferredRekey,
		OldRef: ontology.NodeRef{NotePath: oldPath, TypeName: "Spec"}, NewRef: ontology.NodeRef{NotePath: newPath, TypeName: "Spec"},
		OldIdentifier: oldID, NewIdentifier: newID, PreferredField: "id", AliasesField: "aliases",
	}
}

func linkDiscoveryPlan(t *testing.T, notePath, content string, rewrites []identifierrewrite.IdentifierRewrite, target ontology.NodeRef) identifierrewrite.StructuredLinkRewritePlan {
	t.Helper()
	scan := markdownsyntax.ScanStructuredLinkSnapshot(content)
	return identifierrewrite.PlanStructuredLinkRewrites(identifierrewrite.StructuredLinkRewriteInput{
		NotePath: notePath, Content: content, LinkSnapshot: &scan, Rewrites: rewrites,
		Resolutions: []identifierrewrite.StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{target}}},
	})
}

func linkSourcePaths(input []SourcePrecondition) []string {
	out := make([]string, len(input))
	for index := range input {
		out[index] = input[index].NotePath
	}
	return out
}

func linkPlanPaths(input []identifierrewrite.StructuredLinkRewritePlan) []string {
	out := make([]string, len(input))
	for index := range input {
		out[index] = input[index].NotePath
	}
	return out
}
