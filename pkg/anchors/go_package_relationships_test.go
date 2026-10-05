package codeanchor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeGoPackageRelationshipsResolvesSplitCallsAndPointerImplementationOffline(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: "production"}
	sources := []GoPackageSource{
		goRelationshipSource("dispatch/queue.go", `package dispatch
import "context"
type Queue struct{}
func (*Queue) Drain(context.Context) {}
`),
		goRelationshipSource("dispatch/worker.go", `package dispatch
import (
    "context"
    "fmt"
)
type Worker struct { Queue *Queue }
func (w *Worker) Sync() { w.Queue.Drain(context.Background()); fmt.Println("done") }
`),
		goRelationshipSource("dispatch/store.go", `package dispatch
type OperationStore interface { Save(string) error }
type MemoryOperationStore struct{}
func (*MemoryOperationStore) Save(value string) error { return nil }
`),
		goRelationshipSource("dispatch/queue_test.go", `package dispatch
import "testing"
func TestDrain(t *testing.T) { q := &Queue{}; q.Drain() }
`),
	}
	snapshot := GoPackageSnapshot{Package: key, Sources: sources}
	snapshot.MembershipDigest = GoPackageMembershipDigest(key, goRelationshipMembers(key, sources))

	analysis, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.NoError(t, err)
	require.False(t, analysis.Complete, "unavailable imports are disclosed even when local proofs remain valid")
	require.Contains(t, analysis.Diagnostics, GoRelationshipDiagnostic{Code: "dependency_unavailable", Detail: "context"})
	require.Contains(t, analysis.Diagnostics, GoRelationshipDiagnostic{Code: "dependency_unavailable", Detail: "fmt"})
	require.Contains(t, analysis.Diagnostics, GoRelationshipDiagnostic{Code: "dependency_unavailable", Detail: "testing"})
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipCalls, SourcePath: "dispatch/worker.go", SourceFQN: "example.com/app/dispatch.Worker.Sync", TargetFQN: "example.com/app/dispatch.Queue.Drain"})
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipCalls, SourcePath: "dispatch/queue_test.go", SourceFQN: "example.com/app/dispatch.TestDrain", TargetFQN: "example.com/app/dispatch.Queue.Drain"})
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipImplements, SourcePath: "dispatch/store.go", SourceFQN: "example.com/app/dispatch.MemoryOperationStore", TargetFQN: "example.com/app/dispatch.OperationStore", PointerOnly: true})
	for _, relationship := range analysis.Relationships {
		require.NotEqual(t, "fmt.Println", relationship.TargetFQN)
	}
}

func TestAnalyzeGoPackageRelationshipsRejectsIncompleteEmbeddedInterfaceAndHandlesRecursiveInterface(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/model", Directory: "model", PackageName: "model", BuildVariant: "production"}
	sources := []GoPackageSource{
		goRelationshipSource("model/model.go", `package model
import missing "example.com/missing"
type Node interface { Next() Node }
type Linked struct{}
func (*Linked) Next() Node { return nil }
type Broken interface { missing.External }
type Empty struct{}
`),
	}
	snapshot := GoPackageSnapshot{Package: key, Sources: sources, MembershipDigest: GoPackageMembershipDigest(key, goRelationshipMembers(key, sources))}

	analysis, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.NoError(t, err)
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipImplements, SourcePath: "model/model.go", SourceFQN: "example.com/app/model.Linked", TargetFQN: "example.com/app/model.Node", PointerOnly: true})
	for _, relationship := range analysis.Relationships {
		require.NotEqual(t, "example.com/app/model.Broken", relationship.TargetFQN, "an unresolved embedded type cannot degrade into an empty interface")
	}
}

func TestAnalyzeGoPackageRelationshipsAcceptsValidGenericCallConstraintWithoutTreatingItAsAnImplementerTarget(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/model", Directory: "model", PackageName: "model", BuildVariant: "production"}
	sources := []GoPackageSource{goRelationshipSource("model/generic.go", `package model
type Number interface { ~int | ~int64 }
func Convert[T Number](value T) T { return value }
func Use() int { return Convert(1) }
type Local int
`)}
	snapshot := GoPackageSnapshot{Package: key, Sources: sources, MembershipDigest: GoPackageMembershipDigest(key, goRelationshipMembers(key, sources))}

	analysis, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.NoError(t, err)
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipCalls, SourcePath: "model/generic.go", SourceFQN: "example.com/app/model.Use", TargetFQN: "example.com/app/model.Convert"})
	for _, relationship := range analysis.Relationships {
		require.False(t, relationship.Kind == GoRelationshipImplements && relationship.TargetFQN == "example.com/app/model.Number")
	}
}

func TestAnalyzeGoPackageRelationshipsHandlesAliasesPromotedMethodsAndWrongSignatures(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/model", Directory: "model", PackageName: "model", BuildVariant: "production"}
	sources := []GoPackageSource{
		goRelationshipSource("model/contracts.go", `package model
type Writer interface { Save(string) error }
type Base struct{}
func (*Base) Save(string) error { return nil }
type Embedded struct { *Base }
type EmbeddedAlias = Embedded
type Wrong struct{}
func (*Wrong) Save(int) error { return nil }
`),
		goRelationshipSource("model/use.go", `package model
func (e *Embedded) Persist(value string) error { return e.Save(value) }
func PersistAlias(e *EmbeddedAlias, value string) error { return e.Save(value) }
`),
	}
	snapshot := GoPackageSnapshot{Package: key, Sources: sources, MembershipDigest: GoPackageMembershipDigest(key, goRelationshipMembers(key, sources))}

	analysis, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.NoError(t, err)
	require.True(t, analysis.Complete)
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipCalls, SourcePath: "model/use.go", SourceFQN: key.ImportPath + ".Embedded.Persist", TargetFQN: key.ImportPath + ".Base.Save"})
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipCalls, SourcePath: "model/use.go", SourceFQN: key.ImportPath + ".PersistAlias", TargetFQN: key.ImportPath + ".Base.Save"})
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipImplements, SourcePath: "model/contracts.go", SourceFQN: key.ImportPath + ".Embedded", TargetFQN: key.ImportPath + ".Writer"})
	require.Contains(t, analysis.Relationships, GoDerivedRelationship{Kind: GoRelationshipImplements, SourcePath: "model/contracts.go", SourceFQN: key.ImportPath + ".EmbeddedAlias", TargetFQN: key.ImportPath + ".Writer"})
	for _, relationship := range analysis.Relationships {
		require.False(t, relationship.Kind == GoRelationshipImplements && relationship.SourceFQN == key.ImportPath+".Wrong")
	}
}

func TestAnalyzeGoPackageRelationshipsRejectsIncompleteMembershipAndStaleBytes(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: "production"}
	good := goRelationshipSource("dispatch/store.go", `package dispatch
type Store interface { Save() }
type Memory struct{}
func (*Memory) Save() {}
`)
	untrusted := goRelationshipSource("dispatch/methods.go", "package dispatch\n")
	untrusted.ParseStatus = ParseErrored
	sources := []GoPackageSource{good, untrusted}
	snapshot := GoPackageSnapshot{Package: key, Sources: sources, MembershipDigest: GoPackageMembershipDigest(key, goRelationshipMembers(key, sources))}

	analysis, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.NoError(t, err)
	require.False(t, analysis.Complete)
	require.Empty(t, analysis.Relationships, "an untrusted sibling cannot leave a complete method-set proof")

	stale := snapshot
	stale.Sources = append([]GoPackageSource(nil), sources...)
	stale.Sources[0].Content = []byte("package dispatch\n")
	_, err = AnalyzeGoPackageRelationships(context.Background(), stale)
	require.ErrorContains(t, err, "source hash mismatch")
}

func TestAnalyzeGoPackageRelationshipsDoesNotTrustOmittedUnclassifiedSibling(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: "production"}
	sources := []GoPackageSource{goRelationshipSource("dispatch/store.go", `package dispatch
type Store interface { Save() }
type Memory struct{}
func (*Memory) Save() {}
`)}
	members := goRelationshipMembers(key, sources)
	members = append(members, GoPackageMember{Path: "dispatch/broken.go", Hash: "broken-hash", ParseStatus: ParseErrored})
	snapshot := GoPackageSnapshot{Package: key, Sources: sources, Membership: members, MembershipDigest: GoPackageMembershipDigest(key, members)}

	analysis, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.NoError(t, err)
	require.False(t, analysis.Complete)
	require.Empty(t, analysis.Relationships)
	require.Contains(t, analysis.Diagnostics, GoRelationshipDiagnostic{Code: "source_package_unclassified", Path: "dispatch/broken.go", Detail: string(ParseErrored)})
}

func TestAnalyzeGoPackageRelationshipsSeparatesPackageIdentityAndCancellation(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch_test", BuildVariant: "external_test"}
	source := goRelationshipSource("dispatch/external_test.go", "package dispatch\n")
	snapshot := GoPackageSnapshot{Package: key, Sources: []GoPackageSource{source}}
	snapshot.MembershipDigest = GoPackageMembershipDigest(key, goRelationshipMembers(key, snapshot.Sources))
	_, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.ErrorContains(t, err, "package name mismatch")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = AnalyzeGoPackageRelationships(ctx, snapshot)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAnalyzeGoPackageRelationshipsUsesActiveBuildConstraints(t *testing.T) {
	key := GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: CurrentGoBuildVariant()}
	excludedOS := "windows"
	if runtime.GOOS == excludedOS {
		excludedOS = "linux"
	}
	excludedArch := "amd64"
	if runtime.GOARCH == excludedArch {
		excludedArch = "arm64"
	}
	sources := []GoPackageSource{
		goRelationshipSource("dispatch/active.go", `package dispatch
type Queue struct{}
func (*Queue) Drain() {}
func Run(q *Queue) { q.Drain() }
`),
		goRelationshipSource("dispatch/excluded_"+excludedOS+".go", `package dispatch
type Queue struct{}
func (*Queue) Drain() {}
func ExcludedOS(q *Queue) { q.Drain() }
`),
		goRelationshipSource("dispatch/excluded_"+excludedArch+".go", `package dispatch
type Queue struct{}
func (*Queue) Drain() {}
func ExcludedArch(q *Queue) { q.Drain() }
`),
		goRelationshipSource("dispatch/excluded_tag.go", `//go:build rhizome_never_enabled

package dispatch
type Queue struct{}
func broken(
`),
	}
	sources[len(sources)-1].ParseStatus = ParseErrored
	snapshot := GoPackageSnapshot{Package: key, Sources: sources, MembershipDigest: GoPackageMembershipDigest(key, goRelationshipMembers(key, sources))}

	analysis, err := AnalyzeGoPackageRelationships(context.Background(), snapshot)
	require.NoError(t, err)
	require.True(t, analysis.Complete, "excluded duplicate declarations must not corrupt active-package proof: %+v", analysis.Diagnostics)
	require.Equal(t, []GoDerivedRelationship{{
		Kind: GoRelationshipCalls, SourcePath: "dispatch/active.go",
		SourceFQN: key.ImportPath + ".Run", TargetFQN: key.ImportPath + ".Queue.Drain",
	}}, analysis.Relationships)
}

func goRelationshipSource(path, content string) GoPackageSource {
	digest := sha256.Sum256([]byte(content))
	return GoPackageSource{Path: path, Hash: hex.EncodeToString(digest[:]), Content: []byte(content), ParseStatus: ParseOK}
}

func goRelationshipMembers(key GoPackageKey, sources []GoPackageSource) []GoPackageMember {
	members := make([]GoPackageMember, 0, len(sources))
	for _, source := range sources {
		members = append(members, GoPackageMember{Path: source.Path, Hash: source.Hash, ParseStatus: source.ParseStatus, PackageKey: key.StorageKey()})
	}
	return members
}
