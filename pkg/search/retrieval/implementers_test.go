package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestImplementersRetrieverFiltersBeforeCapAndContinuesPastFilteredDuplicateRound(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "implementers.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	key := codeanchor.GoPackageKey{ImportPath: "example.com/app/contracts", Directory: "src", PackageName: "contracts", BuildVariant: codeanchor.CurrentGoBuildVariant()}
	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "target-a", Lang: codeanchor.LangGo, Kind: "interface", Path: "src/contracts.go", Symbol: "TargetA", FQN: key.ImportPath + ".TargetA", Fingerprint: "a"},
		{AnchorID: "target-b", Lang: codeanchor.LangGo, Kind: "interface", Path: "src/contracts.go", Symbol: "TargetB", FQN: key.ImportPath + ".TargetB", Fingerprint: "b"},
		{AnchorID: "target-c", Lang: codeanchor.LangGo, Kind: "interface", Path: "src/contracts.go", Symbol: "TargetC", FQN: key.ImportPath + ".TargetC", Fingerprint: "c"},
		{AnchorID: "before", Lang: codeanchor.LangGo, Kind: "struct", Path: "src/before.go", Symbol: "A0Before", FQN: key.ImportPath + ".A0Before", Fingerprint: "before"},
		{AnchorID: "common", Lang: codeanchor.LangGo, Kind: "struct", Path: "src/common.go", Symbol: "ACommon", FQN: key.ImportPath + ".ACommon", Fingerprint: "common"},
		{AnchorID: "outside", Lang: codeanchor.LangGo, Kind: "struct", Path: "src/outside.go", Symbol: "AOutside", FQN: key.ImportPath + ".AOutside", Fingerprint: "outside"},
		{AnchorID: "filtered", Lang: codeanchor.LangGo, Kind: "struct", Path: "src/filtered.go", Symbol: "BFiltered", FQN: key.ImportPath + ".BFiltered", Fingerprint: "filtered"},
		{AnchorID: "later", Lang: codeanchor.LangGo, Kind: "struct", Path: "src/later.go", Symbol: "YLater", FQN: key.ImportPath + ".YLater", Fingerprint: "later"},
		{AnchorID: "unique", Lang: codeanchor.LangGo, Kind: "struct", Path: "src/unique.go", Symbol: "ZUnique", FQN: key.ImportPath + ".ZUnique", Fingerprint: "unique"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/contracts.go", anchors[:3], nil, nil))
	for _, anchor := range anchors[3:] {
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	}
	members := make([]codeanchor.GoPackageMember, 0, len(anchors)-2)
	summaries := make([]codeanchor.FileSummary, 0, len(anchors)-2)
	for _, path := range []string{"src/contracts.go", "src/before.go", "src/common.go", "src/outside.go", "src/filtered.go", "src/later.go", "src/unique.go"} {
		summaries = append(summaries, codeanchor.FileSummary{FilePath: path, Lang: codeanchor.LangGo, Hash: path, ParseStatus: codeanchor.ParseOK, GoPackage: &key})
		members = append(members, codeanchor.GoPackageMember{Path: path, Hash: path, ParseStatus: codeanchor.ParseOK, PackageKey: key.StorageKey()})
	}
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{Summaries: summaries}))
	rel := func(source, target string) codeanchor.GoDerivedRelationship {
		return codeanchor.GoDerivedRelationship{Kind: codeanchor.GoRelationshipImplements, SourcePath: "src/" + source + ".go", SourceFQN: key.ImportPath + "." + source, TargetFQN: key.ImportPath + "." + target}
	}
	relationships := []codeanchor.GoDerivedRelationship{
		rel("ACommon", "TargetA"), rel("BFiltered", "TargetA"), rel("ZUnique", "TargetA"),
		rel("A0Before", "TargetB"), rel("ACommon", "TargetB"), rel("YLater", "TargetB"),
		rel("AOutside", "TargetC"), rel("ZUnique", "TargetC"),
	}
	// Relationship source paths use the real persisted file names.
	pathByFQN := map[string]string{
		key.ImportPath + ".A0Before": "src/before.go", key.ImportPath + ".ACommon": "src/common.go",
		key.ImportPath + ".AOutside": "src/outside.go", key.ImportPath + ".BFiltered": "src/filtered.go",
		key.ImportPath + ".YLater": "src/later.go", key.ImportPath + ".ZUnique": "src/unique.go",
	}
	for i := range relationships {
		relationships[i].SourcePath = pathByFQN[relationships[i].SourceFQN]
	}
	require.NoError(t, store.ReplaceGoPackageRelationships(ctx, []codeanchor.GoPackageRelationshipReplacement{{
		Package: key, MembershipDigest: codeanchor.GoPackageMembershipDigest(key, members), AnalyzerVersion: codeanchor.GoRelationshipAnalyzerVersion, Complete: true, Relationships: relationships,
	}}))

	retriever := &ImplementersRetriever{Store: store, Limit: 1}
	filtered, err := retriever.Retrieve(ctx, search.QuerySpec{
		Seeds:   []knowledge.Handle{knowledge.AnchorHandle("target-c")},
		Filters: search.Filters{PathPrefixes: []string{"src/unique.go"}},
	})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, key.ImportPath+".ZUnique", filtered[0].FQN)

	retriever.Limit = 4
	allowed := []string{"src/before.go", "src/common.go", "src/later.go", "src/unique.go"}
	combined, err := retriever.Retrieve(ctx, search.QuerySpec{
		Seeds:   []knowledge.Handle{knowledge.AnchorHandle("target-a"), knowledge.AnchorHandle("target-b")},
		Filters: search.Filters{PathPrefixes: allowed},
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		key.ImportPath + ".ACommon", key.ImportPath + ".A0Before",
		key.ImportPath + ".ZUnique", key.ImportPath + ".YLater",
	}, candidateFQNs(combined))
}

func candidateFQNs(candidates []search.Candidate) []string {
	out := make([]string, len(candidates))
	for i := range candidates {
		out[i] = candidates[i].FQN
	}
	return out
}
