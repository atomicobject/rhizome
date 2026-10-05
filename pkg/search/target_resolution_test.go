package search

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

type stubTargetResolverStore struct {
	bySymbol      map[string][]codeanchor.IntelAnchor
	byFQN         map[string][]codeanchor.IntelAnchor
	notePaths     []string
	filePaths     []string
	symbolCalls   int
	notePathCalls int
	filePathCalls int
}

func (s *stubTargetResolverStore) IntelAnchorsBySymbol(_ context.Context, symbol string, _ int) ([]codeanchor.IntelAnchor, error) {
	s.symbolCalls++
	return append([]codeanchor.IntelAnchor(nil), s.bySymbol[symbol]...), nil
}

func (s *stubTargetResolverStore) IntelAnchorsByFQNsLimited(_ context.Context, fqns []string, _ int) (map[string][]codeanchor.IntelAnchor, error) {
	out := make(map[string][]codeanchor.IntelAnchor, len(fqns))
	for _, fqn := range fqns {
		out[fqn] = append([]codeanchor.IntelAnchor(nil), s.byFQN[fqn]...)
	}
	return out, nil
}

func (s *stubTargetResolverStore) IntelNotePaths(_ context.Context) ([]string, error) {
	s.notePathCalls++
	return append([]string(nil), s.notePaths...), nil
}

func (s *stubTargetResolverStore) ListFiles(_ context.Context, _ int) ([]string, error) {
	s.filePathCalls++
	return append([]string(nil), s.filePaths...), nil
}

func TestResolveQuerySpecTargets_UsesSuppliedPathKindsWithoutStoreWideReads(t *testing.T) {
	store := &stubTargetResolverStore{filePaths: []string{"unrelated/file.go"}}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text:      "search subsystem overview",
		Intent:    IntentSubsystemOverview,
		PathKinds: map[string]PathKind{"pkg/search/service.go": PathKindCode},
	})

	if spec.TargetStatus != TargetStatusInferredPath {
		t.Fatalf("expected inferred path status, got %q", spec.TargetStatus)
	}
	if len(spec.ExplicitSeedPaths) != 1 || spec.ExplicitSeedPaths[0] != "pkg/search" {
		t.Fatalf("expected pkg/search explicit seed, got %#v", spec.ExplicitSeedPaths)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if store.notePathCalls != 0 || store.filePathCalls != 0 {
		t.Fatalf("expected supplied path kinds to avoid store-wide reads, got notes=%d files=%d", store.notePathCalls, store.filePathCalls)
	}
}

func TestResolveQuerySpecTargets_SkipsPathKindHydrationWhenRepairIsUnnecessary(t *testing.T) {
	store := &stubTargetResolverStore{filePaths: []string{"pkg/search/service.go"}}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text:   "find service",
		Intent: IntentSearch,
	})

	if !spec.targetsResolved {
		t.Fatal("expected target resolution to complete")
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if store.notePathCalls != 0 || store.filePathCalls != 0 {
		t.Fatalf("expected no hydration when repair is unnecessary, got notes=%d files=%d", store.notePathCalls, store.filePathCalls)
	}
}

func TestResolveQuerySpecTargets_UsesDurableHTMLOwnership(t *testing.T) {
	tests := []struct {
		name      string
		notePaths []string
		filePaths []string
		wantKind  PathKind
	}{
		{name: "code", filePaths: []string{"docs/Reference.html"}, wantKind: PathKindCode},
		{name: "note wins", notePaths: []string{"docs/Reference.html"}, filePaths: []string{"docs/Reference.html"}, wantKind: PathKindNote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, _ := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
				notePaths: tt.notePaths,
				filePaths: tt.filePaths,
			}, QuerySpec{Text: "subsystem overview for Reference", Intent: IntentSubsystemOverview})
			if got := spec.PathKinds["docs/Reference.html"]; got != tt.wantKind {
				t.Fatalf("expected HTML path kind %q, got %#v", tt.wantKind, spec.PathKinds)
			}
		})
	}
}

func TestResolveQuerySpecTargets_InfersModuleDirectoryBeforeDowngrade(t *testing.T) {
	store := &stubTargetResolverStore{filePaths: []string{"pkg/search/service.go"}}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text:   "search subsystem overview",
		Intent: IntentSubsystemOverview,
	})

	if spec.Intent != IntentSubsystemOverview {
		t.Fatalf("expected subsystem_overview to remain applied, got %q", spec.Intent)
	}
	if spec.TargetStatus != TargetStatusInferredPath {
		t.Fatalf("expected inferred path status, got %q", spec.TargetStatus)
	}
	if len(spec.ExplicitSeedPaths) != 1 || spec.ExplicitSeedPaths[0] != "pkg/search" {
		t.Fatalf("expected pkg/search explicit seed, got %#v", spec.ExplicitSeedPaths)
	}
	if store.notePathCalls != 1 || store.filePathCalls != 1 {
		t.Fatalf("expected one note and one file path-kind hydration, got notes=%d files=%d", store.notePathCalls, store.filePathCalls)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
}

func TestResolveQuerySpecTargets_ResolvesExactSymbolMatch(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"Service": {{AnchorID: "a1", Path: "pkg/search/service.go", Symbol: "Service", FQN: "pkg.search.Service", Kind: "type"}},
		},
	}, QuerySpec{Text: "go to def Service", Intent: IntentGoToDef})

	if spec.TargetStatus != TargetStatusInferredSymbol {
		t.Fatalf("expected inferred symbol status, got %q", spec.TargetStatus)
	}
	if len(spec.ExplicitSeedPaths) != 1 || spec.ExplicitSeedPaths[0] != "pkg/search/service.go" {
		t.Fatalf("expected symbol resolution to seed pkg/search/service.go, got %#v", spec.ExplicitSeedPaths)
	}
	if len(spec.Seeds) == 0 {
		t.Fatalf("expected seeds from symbol resolution")
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
}

func TestResolveQuerySpecTargets_PrefersSpecificSymbolOverLanguageQualifier(t *testing.T) {
	for _, query := range []string{"definition of Python slugify", "definition of slugify in Python"} {
		t.Run(query, func(t *testing.T) {
			store := &stubTargetResolverStore{
				filePaths: []string{"notes/Python.md"},
				bySymbol: map[string][]codeanchor.IntelAnchor{
					"Python":  {{AnchorID: "language", Lang: codeanchor.LangPy, Path: "notes/Python.md", Symbol: "Python", FQN: "notes.Python", Kind: "section"}},
					"slugify": {{AnchorID: "target", Lang: codeanchor.LangPy, Path: "src/utils.py", Symbol: "slugify", FQN: "src.utils.slugify", Kind: "function"}},
				},
			}
			spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: query, Intent: IntentGoToDef})

			if len(warnings) != 0 {
				t.Fatalf("expected no warnings, got %#v", warnings)
			}
			if spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "src.utils.slugify" {
				t.Fatalf("expected slugify target, got %#v", spec.ResolvedTarget)
			}
		})
	}
}

func TestResolveQuerySpecTargets_PreservesLanguageNamedTargetWhenSoleOrQuoted(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
	}{
		{name: "sole", query: "definition of Go"},
		{name: "quoted among context", query: `definition of "Go" helper`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
				"Go":     {{AnchorID: "target", Path: "src/go.go", Symbol: "Go", FQN: "pkg.Go", Kind: "function"}},
				"helper": {{AnchorID: "context", Path: "src/helper.go", Symbol: "helper", FQN: "pkg.helper", Kind: "function"}},
			}}
			spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: test.query, Intent: IntentGoToDef})

			if len(warnings) != 0 {
				t.Fatalf("expected no warnings, got %#v", warnings)
			}
			if spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "pkg.Go" {
				t.Fatalf("expected language-named target, got %#v", spec.ResolvedTarget)
			}
		})
	}
}

func TestResolveQuerySpecTargets_PrefersIdentifierOverSameNamedDirectory(t *testing.T) {
	store := &stubTargetResolverStore{
		filePaths: []string{"pkg/search/indexgeneration/store.go"},
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"IndexGeneration": {{AnchorID: "target", Path: "pkg/app/unifiedsearch/index_generation.go", Symbol: "IndexGeneration", FQN: "pkg.app.unifiedsearch.IndexGeneration", Kind: "function"}},
		},
	}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "tests for IndexGeneration", Intent: IntentTestsForCode})

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "pkg.app.unifiedsearch.IndexGeneration" {
		t.Fatalf("expected indexed symbol target, got %#v", spec.ResolvedTarget)
	}
}

func TestResolveQuerySpecTargets_PreservesLowercaseFileBasenameTarget(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		query string
		want  string
	}{
		{name: "sole basename", files: []string{"src/main.ts"}, query: "definition for main", want: "src/main.ts"},
		{name: "exact basename beats partial basenames", files: []string{"src/barrel.ts", "src/barrel-middle.ts", "src/barrel-target.ts"}, query: "definition for barrel", want: "src/barrel.ts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubTargetResolverStore{filePaths: tt.files}
			spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: tt.query, Intent: IntentGoToDef})

			if len(warnings) != 0 {
				t.Fatalf("expected no warnings, got %#v", warnings)
			}
			if spec.TargetStatus == TargetStatusAmbiguous {
				t.Fatalf("expected no ambiguity, got candidates %#v", spec.TargetCandidates)
			}
			if spec.ResolvedTarget == nil || spec.ResolvedTarget.Path != tt.want || spec.ResolvedTarget.FQN != "" {
				t.Fatalf("expected file target %q, got %#v", tt.want, spec.ResolvedTarget)
			}
		})
	}
}

func TestResolveQuerySpecTargets_PrefersDottedFQNOverDottedFilename(t *testing.T) {
	store := &stubTargetResolverStore{
		filePaths: []string{"docs/Foo.Bar"},
		byFQN: map[string][]codeanchor.IntelAnchor{
			"Foo.Bar": {{AnchorID: "target", Path: "src/foo.go", Symbol: "Bar", FQN: "Foo.Bar", Kind: "function"}},
		},
	}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "definition of Foo.Bar", Intent: IntentGoToDef})

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "Foo.Bar" {
		t.Fatalf("expected dotted FQN target, got %#v", spec.ResolvedTarget)
	}
}

func TestResolveQuerySpecTargets_ExplicitKnownFileExtensionBeatsSymbol(t *testing.T) {
	store := &stubTargetResolverStore{
		filePaths: []string{"src/main.ts"},
		byFQN: map[string][]codeanchor.IntelAnchor{
			"main.ts": {{AnchorID: "other", Path: "src/other.ts", Symbol: "ts", FQN: "main.ts", Kind: "function"}},
		},
	}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "definition for main.ts", Intent: IntentGoToDef})

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if spec.ResolvedTarget == nil || spec.ResolvedTarget.Path != "src/main.ts" || spec.ResolvedTarget.FQN != "" {
		t.Fatalf("expected explicit filename target, got %#v", spec.ResolvedTarget)
	}
}

func TestResolveQuerySpecTargets_UsesMeaningfulPathQualifierBeforeAmbiguity(t *testing.T) {
	store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"SearchChunksByVector": {
			{AnchorID: "note", Path: "note/store.go", Symbol: "SearchChunksByVector", FQN: "note.Store.SearchChunksByVector", Kind: "method"},
			{AnchorID: "code", Path: "code/store.go", Symbol: "SearchChunksByVector", FQN: "code.Store.SearchChunksByVector", Kind: "method"},
		},
		"vector": {{AnchorID: "generic", Path: "vector.go", Symbol: "vector", FQN: "pkg.vector", Kind: "type"}},
	}}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "tests for code vector SearchChunksByVector", Intent: IntentTestsForCode})

	if len(warnings) != 0 || spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "code.Store.SearchChunksByVector" {
		t.Fatalf("expected code-qualified target, got status=%q selected=%#v candidates=%#v warnings=%#v", spec.TargetStatus, spec.ResolvedTarget, spec.TargetCandidates, warnings)
	}
}

func TestResolveQuerySpecTargets_FollowsStoredReExportTarget(t *testing.T) {
	store := &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"barrelCall": {{AnchorID: "alias", Lang: codeanchor.LangTS, Path: "src/barrel-middle.ts", Symbol: "barrelCall", FQN: "app.barrel-middle.barrelCall", Kind: "field", Signature: "re-export from app.barrel-target.targetCall"}},
		},
		byFQN: map[string][]codeanchor.IntelAnchor{
			"app.barrel-target.targetCall": {{AnchorID: "target", Lang: codeanchor.LangTS, Path: "src/barrel-target.ts", Symbol: "targetCall", FQN: "app.barrel-target.targetCall", Kind: "function"}},
		},
	}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "callers of TypeScript barrelCall", Intent: IntentCallers})
	if len(warnings) != 0 || spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "app.barrel-target.targetCall" {
		t.Fatalf("expected canonical re-export target, got target=%#v warnings=%#v", spec.ResolvedTarget, warnings)
	}
}

func TestResolveQuerySpecTargets_UsesLanguageQualifierForCompoundMemberFallback(t *testing.T) {
	store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"pushUpdates": {
			{AnchorID: "php", Lang: codeanchor.LangPhp, Path: "php/SyncClient.php", Symbol: "pushUpdates", FQN: `Polyglot\Todo\SyncClient::pushUpdates`, Kind: "method"},
			{AnchorID: "php-other", Lang: codeanchor.LangPhp, Path: "php/OtherClient.php", Symbol: "pushUpdates", FQN: `Polyglot\Todo\OtherClient::pushUpdates`, Kind: "method"},
			{AnchorID: "ts", Lang: codeanchor.LangTS, Path: "ts/sync.ts", Symbol: "pushUpdates", FQN: "app.sync.pushUpdates", Kind: "function"},
		},
	}}
	for _, query := range []string{"callers of PHP SyncClient.pushUpdates", `callers of PHP Polyglot\Todo\SyncClient::pushUpdates`} {
		spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: query, Intent: IntentCallers})

		if len(warnings) != 0 {
			t.Fatalf("%q: expected no warnings, got %#v", query, warnings)
		}
		if spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != `Polyglot\Todo\SyncClient::pushUpdates` {
			t.Fatalf("%q: expected PHP member target, got %#v", query, spec.ResolvedTarget)
		}
	}
}

func TestResolveQuerySpecTargets_QualifiedMemberDoesNotFallBackToUnrelatedOwner(t *testing.T) {
	store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"pushUpdates": {
			{AnchorID: "php-other", Lang: codeanchor.LangPhp, Path: "php/OtherClient.php", Symbol: "pushUpdates", FQN: `Polyglot\Todo\OtherClient::pushUpdates`, Kind: "method"},
			{AnchorID: "php-wrong", Lang: codeanchor.LangPhp, Path: "php/WrongClient.php", Symbol: "pushUpdates", FQN: `Polyglot\Todo\WrongClient::pushUpdates`, Kind: "method"},
		},
	}}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "callers of PHP MissingClient.pushUpdates", Intent: IntentCallers})

	if spec.TargetStatus != TargetStatusUnresolved || spec.ResolvedTarget != nil || len(spec.TargetCandidates) != 0 {
		t.Fatalf("expected nonexistent qualified owner to remain unresolved, got status=%q selected=%#v candidates=%#v", spec.TargetStatus, spec.ResolvedTarget, spec.TargetCandidates)
	}
	if len(warnings) == 0 {
		t.Fatal("expected unresolved-target warning")
	}
}

func TestResolveQuerySpecTargets_QualifiedMemberPreservesCaseSensitiveOwner(t *testing.T) {
	store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"push": {
			{AnchorID: "upper", Lang: codeanchor.LangTS, Path: "Client.ts", Symbol: "push", FQN: "app.Client.push", Kind: "method"},
			{AnchorID: "lower", Lang: codeanchor.LangTS, Path: "client.ts", Symbol: "push", FQN: "app.client.push", Kind: "method"},
		},
	}}

	resolved, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "callers of TypeScript Client.push", Intent: IntentCallers})
	if len(warnings) != 0 || resolved.ResolvedTarget == nil || resolved.ResolvedTarget.FQN != "app.Client.push" {
		t.Fatalf("expected exact-case TypeScript owner, got target=%#v warnings=%#v", resolved.ResolvedTarget, warnings)
	}

	mismatched, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"push": {{AnchorID: "upper", Lang: codeanchor.LangTS, Path: "Client.ts", Symbol: "push", FQN: "app.Client.push", Kind: "method"}},
	}}, QuerySpec{Text: "callers of TypeScript CLIENT.push", Intent: IntentCallers})
	if mismatched.TargetStatus != TargetStatusUnresolved || mismatched.ResolvedTarget != nil || len(warnings) == 0 {
		t.Fatalf("expected mismatched-case TypeScript owner to remain unresolved, got status=%q target=%#v warnings=%#v", mismatched.TargetStatus, mismatched.ResolvedTarget, warnings)
	}
}

func TestResolveQuerySpecTargets_CallableIntentIgnoresFieldHomonyms(t *testing.T) {
	store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"IndexGeneration": {
			{AnchorID: "func", Path: "generation.go", Symbol: "IndexGeneration", FQN: "pkg.IndexGeneration", Kind: "func"},
			{AnchorID: "field", Path: "result.go", Symbol: "IndexGeneration", FQN: "pkg.Result.IndexGeneration", Kind: "field"},
		},
	}}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: "callers of IndexGeneration", Intent: IntentCallers})

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "pkg.IndexGeneration" {
		t.Fatalf("expected callable target, got %#v", spec.ResolvedTarget)
	}
}

func TestResolveQuerySpecTargets_DoesNotResolveDistinctSameFileSymbolsAsUnique(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"Run": {
				{AnchorID: "a1", Path: "pkg/worker.go", Symbol: "Run", FQN: "pkg.Worker.Run", Kind: "method"},
				{AnchorID: "a2", Path: "pkg/worker.go", Symbol: "Run", FQN: "pkg.Supervisor.Run", Kind: "method"},
			},
		},
	}, QuerySpec{Text: "go to def Run", Intent: IntentGoToDef})

	if spec.TargetStatus != TargetStatusAmbiguous || spec.ResolvedTarget != nil {
		t.Fatalf("expected same-file distinct symbols to remain ambiguous, got status=%q selected=%#v", spec.TargetStatus, spec.ResolvedTarget)
	}
	if len(spec.TargetCandidates) != 2 {
		t.Fatalf("expected both target candidates, got %#v", spec.TargetCandidates)
	}
	if len(warnings) == 0 {
		t.Fatal("expected ambiguity warning")
	}
}

func TestResolveQuerySpecTargets_RefactorImpactRequiresConcreteTarget(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"Service": {{AnchorID: "a1", Path: "pkg/search/service.go", Symbol: "Service", FQN: "pkg.search.Service", Kind: "type"}},
		},
	}, QuerySpec{Text: "refactor impact of Service", Intent: IntentRefactorImpact})

	if spec.TargetStatus != TargetStatusInferredSymbol {
		t.Fatalf("expected inferred symbol status, got %q", spec.TargetStatus)
	}
	if len(spec.Seeds) == 0 || len(spec.ExplicitSeedPaths) != 1 {
		t.Fatalf("expected resolved refactor target seeds, got seeds=%#v paths=%#v", spec.Seeds, spec.ExplicitSeedPaths)
	}
	if spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "pkg.search.Service" || spec.ResolvedTarget.Path != "pkg/search/service.go" {
		t.Fatalf("expected exact resolved target identity, got %#v", spec.ResolvedTarget)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
}

func TestResolveQuerySpecTargets_RefactorImpactBlocksAmbiguousFallback(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"Service": {
				{AnchorID: "a1", Path: "pkg/search/service.go", Symbol: "Service", FQN: "pkg.search.Service", Kind: "type"},
				{AnchorID: "a2", Path: "pkg/cache/service.go", Symbol: "Service", FQN: "pkg.cache.Service", Kind: "type"},
			},
		},
	}, QuerySpec{Text: "refactor impact of Service", Intent: IntentRefactorImpact})

	if spec.TargetStatus != TargetStatusAmbiguous {
		t.Fatalf("expected ambiguous status, got %q", spec.TargetStatus)
	}
	if !ShouldBlockPrecisionFallback(spec) {
		t.Fatalf("expected refactor impact fallback to be blocked")
	}
	if len(warnings) < 2 {
		t.Fatalf("expected ambiguity and precision warnings, got %#v", warnings)
	}
}

func TestResolveQuerySpecTargets_BlocksPrecisionFallbackOnAmbiguity(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"Service": {
				{AnchorID: "a1", Path: "pkg/search/service.go", Symbol: "Service", FQN: "pkg.search.Service", Kind: "type"},
				{AnchorID: "a2", Path: "pkg/cache/service.go", Symbol: "Service", FQN: "pkg.cache.Service", Kind: "type"},
			},
		},
	}, QuerySpec{Text: "go to def Service", Intent: IntentGoToDef})

	if spec.TargetStatus != TargetStatusAmbiguous {
		t.Fatalf("expected ambiguous status, got %q", spec.TargetStatus)
	}
	if !ShouldBlockPrecisionFallback(spec) {
		t.Fatalf("expected precision fallback to be blocked")
	}
	if len(spec.TargetCandidates) != 2 {
		t.Fatalf("expected target candidates, got %#v", spec.TargetCandidates)
	}
	if len(warnings) < 2 || warnings[0].Code != "target_ambiguous" {
		t.Fatalf("expected ambiguity warnings, got %#v", warnings)
	}
}

func TestResolveQuerySpecTargets_PrecisionKeepsSharedModuleMatchesAmbiguous(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"Service": {
				{AnchorID: "a1", Path: "pkg/search/service.go", Symbol: "Service", FQN: "pkg.search.Service", Kind: "type"},
				{AnchorID: "a2", Path: "pkg/search/other.go", Symbol: "Service", FQN: "pkg.search.ServiceAlt", Kind: "type"},
			},
		},
	}, QuerySpec{Text: "go to def Service", Intent: IntentGoToDef})

	if spec.TargetStatus != TargetStatusAmbiguous {
		t.Fatalf("expected ambiguous status, got %q", spec.TargetStatus)
	}
	if !ShouldBlockPrecisionFallback(spec) {
		t.Fatalf("expected precision fallback to be blocked")
	}
	if len(spec.TargetCandidates) != 2 {
		t.Fatalf("expected target candidates, got %#v", spec.TargetCandidates)
	}
	if len(warnings) < 2 || warnings[0].Code != "target_ambiguous" {
		t.Fatalf("expected ambiguity warnings, got %#v", warnings)
	}
}

func TestResolveQuerySpecTargets_PrefersIndexedPathMatchesWithoutWalkingVault(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", &stubTargetResolverStore{
		filePaths: []string{
			"pkg/search/service.go",
			"pkg/search/service.go",
			"pkg/cache/service.go",
		},
	}, QuerySpec{Text: "subsystem overview for pkg/search/service.go", Intent: IntentSubsystemOverview})

	if spec.TargetStatus != TargetStatusInferredPath {
		t.Fatalf("expected inferred path status, got %q", spec.TargetStatus)
	}
	if len(spec.ExplicitSeedPaths) != 1 || spec.ExplicitSeedPaths[0] != "pkg/search/service.go" {
		t.Fatalf("expected pkg/search explicit seed, got %#v", spec.ExplicitSeedPaths)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
}

func TestResolveQuerySpecTargets_IsIdempotentAfterFirstPass(t *testing.T) {
	store := &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"Service": {{AnchorID: "a1", Path: "pkg/search/service.go", Symbol: "Service", FQN: "pkg.search.Service", Kind: "type"}},
		},
	}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text:   "go to def Service",
		Intent: IntentGoToDef,
	})
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if store.symbolCalls == 0 {
		t.Fatalf("expected symbol lookup on first resolution")
	}
	firstCalls := store.symbolCalls

	_, warnings = ResolveQuerySpecTargets(context.Background(), "", store, spec)
	if len(warnings) != 0 {
		t.Fatalf("expected no second-pass warnings, got %#v", warnings)
	}
	if store.symbolCalls != firstCalls {
		t.Fatalf("expected second resolution to skip store lookups, got %d -> %d calls", firstCalls, store.symbolCalls)
	}
}

func requestIdentityAnchorStore() *stubTargetResolverStore {
	const path = "pkg/app/unifiedsearch/contract.go"
	return &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"RequestIdentity": {
				{AnchorID: "a1", Path: path, Symbol: "RequestIdentity", Kind: "function", Lang: codeanchor.LangGo, FQN: "github.com/atomicobject/rhizome/pkg/app/unifiedsearch.RequestIdentity"},
				{AnchorID: "a2", Path: path, Symbol: "RequestIdentity", Kind: "field", Lang: codeanchor.LangGo, FQN: "github.com/atomicobject/rhizome/pkg/app/unifiedsearch.ApplicationResult.RequestIdentity"},
				{AnchorID: "a3", Path: path, Symbol: "RequestIdentity", Kind: "field", Lang: codeanchor.LangGo, FQN: "github.com/atomicobject/rhizome/pkg/app/unifiedsearch.ContinuationCursor.RequestIdentity"},
			},
		},
	}
}

func TestResolveBareSymbolPrefersDeclarationOverFields(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", requestIdentityAnchorStore(), QuerySpec{
		Text:   "RequestIdentity",
		Intent: IntentGoToDef,
	})

	if spec.TargetStatus != TargetStatusInferredSymbol {
		t.Fatalf("expected inferred symbol status, got %q", spec.TargetStatus)
	}
	if spec.ResolvedTarget == nil {
		t.Fatal("expected a resolved target")
	}
	if want := "github.com/atomicobject/rhizome/pkg/app/unifiedsearch.RequestIdentity"; spec.ResolvedTarget.FQN != want {
		t.Fatalf("expected declaration FQN %q, got %q", want, spec.ResolvedTarget.FQN)
	}
	for _, warning := range warnings {
		if warning.Code == "target_ambiguous" {
			t.Fatalf("expected no ambiguity warning, got %#v", warnings)
		}
	}
}

func TestResolveQualifiedMemberKeepsField(t *testing.T) {
	spec, _ := ResolveQuerySpecTargets(context.Background(), "", requestIdentityAnchorStore(), QuerySpec{
		Text:   "ApplicationResult.RequestIdentity",
		Intent: IntentGoToDef,
	})

	if spec.ResolvedTarget == nil {
		t.Fatalf("expected a resolved target, got status %q", spec.TargetStatus)
	}
	if want := "github.com/atomicobject/rhizome/pkg/app/unifiedsearch.ApplicationResult.RequestIdentity"; spec.ResolvedTarget.FQN != want {
		t.Fatalf("expected qualified member FQN %q, got %q", want, spec.ResolvedTarget.FQN)
	}
}

func TestAmbiguousDuplicateDefinitionsOrderDeeperPathFirst(t *testing.T) {
	store := &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"BuildAssessedAnswerForFacets": {
				{AnchorID: "b1", Path: "pkg/app/unifiedsearch/execute.go", Symbol: "BuildAssessedAnswerForFacets", Kind: "function", Lang: codeanchor.LangGo, FQN: "github.com/atomicobject/rhizome/pkg/app/unifiedsearch.BuildAssessedAnswerForFacets"},
				{AnchorID: "b2", Path: "pkg/app/unifiedsearch/application/application.go", Symbol: "BuildAssessedAnswerForFacets", Kind: "function", Lang: codeanchor.LangGo, FQN: "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application.BuildAssessedAnswerForFacets"},
			},
		},
	}

	spec, _ := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text:   "BuildAssessedAnswerForFacets",
		Intent: IntentGoToDef,
	})

	if spec.TargetStatus != TargetStatusAmbiguous {
		t.Fatalf("expected ambiguous status, got %q", spec.TargetStatus)
	}
	if len(spec.TargetCandidates) != 2 {
		t.Fatalf("expected 2 candidates, got %#v", spec.TargetCandidates)
	}
	if want := "pkg/app/unifiedsearch/application/application.go"; spec.TargetCandidates[0].Path != want {
		t.Fatalf("expected deeper definition first, got %#v", spec.TargetCandidates)
	}
}

func rollupCollisionStore() *stubTargetResolverStore {
	return &stubTargetResolverStore{
		bySymbol: map[string][]codeanchor.IntelAnchor{
			"rollup": {{AnchorID: "field", Path: "pkg/legacy/batch.go", Symbol: "rollup", FQN: "pkg.legacy.Batch.rollup", Kind: "field"}},
		},
	}
}

func TestResolveQuerySpecTargets_ProseWordCollisionStaysSpeculative(t *testing.T) {
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", rollupCollisionStore(), QuerySpec{
		Text:   "the nightly rollup exporter",
		Intent: IntentGoToDef,
	})

	if spec.HasExplicitSeeds || len(spec.ExplicitSeedPaths) != 0 || len(spec.Seeds) != 0 {
		t.Fatalf("expected no seeds from a prose word collision, got paths=%#v seeds=%#v", spec.ExplicitSeedPaths, spec.Seeds)
	}
	if spec.ResolvedTarget != nil {
		t.Fatalf("expected no resolved target, got %#v", spec.ResolvedTarget)
	}
	if spec.ResolutionConfidence > 0.3 {
		t.Fatalf("expected low target confidence, got %v", spec.ResolutionConfidence)
	}
	if len(spec.TargetCandidates) != 1 || spec.TargetCandidates[0].Reason != speculativeTargetReason {
		t.Fatalf("expected informational speculative candidates, got %#v", spec.TargetCandidates)
	}
	if ShouldBlockPrecisionFallback(spec) {
		t.Fatal("expected speculative ambiguity to leave precision fallback open")
	}
	for _, warning := range warnings {
		if warning.Code == "precision_fallback_blocked" {
			t.Fatalf("expected no fallback-blocked warning, got %#v", warnings)
		}
	}
}

func TestResolveQuerySpecTargets_ExactDocumentNameBeatsCollidingMember(t *testing.T) {
	store := rollupCollisionStore()
	store.notePaths = []string{"docs/nightly-rollup-exporter.md", "docs/other.md"}

	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text:   "the nightly rollup exporter",
		Intent: IntentGoToDef,
	})

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	if spec.TargetStatus != TargetStatusInferredPath || spec.ResolutionConfidence < 0.9 {
		t.Fatalf("expected an established document target, got status=%q confidence=%v", spec.TargetStatus, spec.ResolutionConfidence)
	}
	if spec.ResolvedTarget == nil || spec.ResolvedTarget.Path != "docs/nightly-rollup-exporter.md" || spec.ResolvedTarget.Reason != "note_title_exact" {
		t.Fatalf("expected the exactly named note, got %#v", spec.ResolvedTarget)
	}
	if len(spec.ExplicitSeedPaths) != 1 || spec.ExplicitSeedPaths[0] != "docs/nightly-rollup-exporter.md" {
		t.Fatalf("expected the note to be seeded, got %#v", spec.ExplicitSeedPaths)
	}
	if len(spec.TargetCandidates) != 0 {
		t.Fatalf("expected speculative candidates to be discarded, got %#v", spec.TargetCandidates)
	}
}

func TestResolveQuerySpecTargets_BareIdentifierQueryStillResolves(t *testing.T) {
	for _, query := range []string{"Retry", "retry"} {
		t.Run(query, func(t *testing.T) {
			store := &stubTargetResolverStore{
				bySymbol: map[string][]codeanchor.IntelAnchor{
					query: {{AnchorID: "decl", Path: "pkg/net/retry.go", Symbol: query, FQN: "pkg.net." + query, Kind: "function"}},
				},
			}
			spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{Text: query, Intent: IntentGoToDef})

			if len(warnings) != 0 {
				t.Fatalf("expected no warnings, got %#v", warnings)
			}
			if spec.TargetStatus != TargetStatusInferredSymbol || spec.ResolvedTarget == nil || spec.ResolvedTarget.FQN != "pkg.net."+query {
				t.Fatalf("expected the declaration target, got status=%q target=%#v", spec.TargetStatus, spec.ResolvedTarget)
			}
		})
	}
}

func TestResolveQuerySpecTargets_NamedIdentifierAmbiguityStillBlocks(t *testing.T) {
	store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"RollupExporter": {
			{AnchorID: "a1", Path: "pkg/export/rollup.go", Symbol: "RollupExporter", FQN: "pkg.export.RollupExporter", Kind: "type"},
			{AnchorID: "a2", Path: "pkg/legacy/rollup.go", Symbol: "RollupExporter", FQN: "pkg.legacy.RollupExporter", Kind: "type"},
		},
	}}

	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text:   "where is RollupExporter defined",
		Intent: IntentGoToDef,
	})

	if spec.TargetStatus != TargetStatusAmbiguous || len(spec.TargetCandidates) != 2 {
		t.Fatalf("expected ambiguous named identifier, got status=%q candidates=%#v", spec.TargetStatus, spec.TargetCandidates)
	}
	if !ShouldBlockPrecisionFallback(spec) {
		t.Fatal("expected ambiguity from a named identifier to block precision fallback")
	}
	if len(warnings) < 2 || warnings[0].Code != "target_ambiguous" {
		t.Fatalf("expected ambiguity and precision warnings, got %#v", warnings)
	}
}
