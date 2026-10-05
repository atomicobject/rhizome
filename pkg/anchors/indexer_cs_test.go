//go:build cgo
// +build cgo

package codeanchor

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func csCodeRef(t *testing.T, root, absPath string) paths.CodePathRef {
	t.Helper()
	rootPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := rootPaths.RelCodeStrict(absPath)
	require.NoError(t, err)
	return paths.CodePathRef{Rel: rel, Abs: paths.ResolveSymlinks(absPath)}
}

func TestCSharpIndexer_SymbolsCallsAndSupers(t *testing.T) {
	content := []byte(`
using System;

namespace Foo.Bar {
	[Instrument(Tag = "svc")]
	public class SyncClient : BaseClient, IDisposable {
		public void PushUpdates() {
			Instrumenter.Start();
			Logger.Info("ok");
		}
	}
	}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "SyncClient.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	t.Logf("symbols=%v calls=%v supers=%v annotations=%v", summary.Symbols, summary.Calls, summary.Supers, summary.Annotations)

	require.Equal(t, LangCs, summary.Lang)
	require.Len(t, summary.Symbols, 2)

	classSym := summary.Symbols[0]
	require.Equal(t, "Foo.Bar", classSym.Pkg)
	require.Equal(t, "SyncClient", classSym.Name)

	methodSym := summary.Symbols[1]
	require.Equal(t, "Foo.Bar.SyncClient", methodSym.Pkg)
	require.Equal(t, "PushUpdates", methodSym.Name)

	require.Len(t, summary.Calls, 2)
	require.Contains(t, summary.Calls, CallSite{File: "csharp/SyncClient.cs", CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Foo.Bar.Instrumenter", Name: "Start"}, OwnerFQN: "Foo.Bar.SyncClient.PushUpdates"})

	require.Len(t, summary.Supers, 2)
	require.Contains(t, summary.Supers, SuperEdge{
		ChildFQN:  Symbol{Pkg: "Foo.Bar", Name: "SyncClient"}.NormalizeFQN(),
		ParentFQN: Symbol{Pkg: "Foo.Bar", Name: "BaseClient"}.NormalizeFQN(),
	})

	require.NotEmpty(t, summary.Annotations)
	require.Contains(t, summary.Annotations, AnnotationUse{
		OwnerFQN: "Foo.Bar.SyncClient",
		AnnSymbol: SymbolRef{
			Lang: LangCs,
			Pkg:  "System",
			Name: "Instrument",
		},
	})
}

func TestCSharpIndexer_QualifiedInvocation(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo.Worker {
	public class Worker {
		public void Run() {
			Polyglot.Todo.SyncClient.PushUpdates("worker-run");
		}
	}
	}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	var buf bytes.Buffer
	prevWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prevWriter)
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)
	require.NotContains(t, buf.String(), "[csharp index]")
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.SyncClient", Name: "PushUpdates"},
		OwnerFQN:     "Polyglot.Todo.Worker.Worker.Run",
	})
}

func TestCSharpIndexer_PropertiesAndFields(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class WorkItem {
	private readonly string _backing;
	private int _priority, _retries;
	public string Title { get; init; } = "";
	public int Priority => _priority;

	public WorkItem(string title) {
		_backing = title;
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "WorkItem.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymField,
		File:      "csharp/WorkItem.cs",
		Pkg:       "Polyglot.Todo.WorkItem",
		Name:      "_backing",
		StartLine: 5,
		EndLine:   5,
	})
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymField,
		File:      "csharp/WorkItem.cs",
		Pkg:       "Polyglot.Todo.WorkItem",
		Name:      "_priority",
		StartLine: 6,
		EndLine:   6,
	})
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymField,
		File:      "csharp/WorkItem.cs",
		Pkg:       "Polyglot.Todo.WorkItem",
		Name:      "_retries",
		StartLine: 6,
		EndLine:   6,
	})
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymField,
		File:      "csharp/WorkItem.cs",
		Pkg:       "Polyglot.Todo.WorkItem",
		Name:      "Title",
		StartLine: 7,
		EndLine:   7,
	})
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymField,
		File:      "csharp/WorkItem.cs",
		Pkg:       "Polyglot.Todo.WorkItem",
		Name:      "Priority",
		StartLine: 8,
		EndLine:   8,
	})
}

func TestCSharpIndexer_LocalFunctionCallsBelongToOuterMethod(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class Worker {
	public void Run() {
		void LocalHelper() {
			Reporter.Start();
		}

		LocalHelper();
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)
	require.Len(t, summary.Symbols, 2)
	require.NotContains(t, summary.Symbols, Symbol{
		Lang: LangCs,
		Kind: SymMethod,
		File: "csharp/Worker.cs",
		Pkg:  "Polyglot.Todo.Worker",
		Name: "LocalHelper",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Reporter", Name: "Start"},
		OwnerFQN:     "Polyglot.Todo.Worker.Run",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Worker", Name: "LocalHelper"},
		OwnerFQN:     "Polyglot.Todo.Worker.Run",
	})
}

func TestCSharpIndexer_CollectionExpressions_BestEffortExtraction(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public record WorkItem
{
	public required string Id { get; init; }
}

public class Worker {
	public List<WorkItem> Backlog { get; init; } = [];

	public List<WorkItem> Build() {
		Reporter.Start();
		return [new WorkItem { Id = "a" }];
	}

	public List<WorkItem> Fallback(List<WorkItem>? items) {
		return items ?? [];
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseRecovered, summary.ParseStatus)
	require.Len(t, summary.Symbols, 6)
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymField,
		File:      "csharp/Worker.cs",
		Pkg:       "Polyglot.Todo.WorkItem",
		Name:      "Id",
		StartLine: 6,
		EndLine:   6,
	})
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymField,
		File:      "csharp/Worker.cs",
		Pkg:       "Polyglot.Todo.Worker",
		Name:      "Backlog",
		StartLine: 10,
		EndLine:   10,
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Reporter", Name: "Start"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
}

func TestCSharpIndexer_CollectionExpressions_MergesRecoveredCallsWithoutDuplicates(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class Worker {
	public void Build() {
		Reporter.Start();
		var items = [];
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseRecovered, summary.ParseStatus)
	require.Len(t, summary.Symbols, 2)
	require.Len(t, summary.Calls, 1)
	require.Equal(t, "Polyglot.Todo.Worker.Build", summary.Calls[0].OwnerFQN)
}

func TestCSharpIndexer_CollectionExpressions_HandlesArgumentAndObjectInitializerForms(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class Worker {
	public object Build(object filters) {
		Reporter.Start();
		Load(filters, []);
		var request = new Request {
			Values = [ComputeValue()],
		};
		Reporter.Stop();
		return request;
	}

	private void Load(object filters, object values) {}
	private string ComputeValue() => "x";
}

public class Request {
	public object Values { get; init; } = [];
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseRecovered, summary.ParseStatus)
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Reporter", Name: "Start"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Reporter", Name: "Stop"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Worker", Name: "Load"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Worker", Name: "ComputeValue"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
}

func TestCSharpIndexer_CollectionExpressions_HandlesSpreadAndSwitchArmForms(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class Worker {
	public List<string> Build(bool includeDefaults) {
		Reporter.Start();
		var values = includeDefaults
			? [.. Defaults(), "x"]
			: [];
		Reporter.Stop();
		return values;
	}

	private List<string> Defaults() => [];
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseRecovered, summary.ParseStatus)
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Reporter", Name: "Start"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Reporter", Name: "Stop"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Worker", Name: "Defaults"},
		OwnerFQN:     "Polyglot.Todo.Worker.Build",
	})
}

func TestCSharpIndexer_ImportsTypeRefsAndDeclarations(t *testing.T) {
	content := []byte(`
using System.Net.Http;
using Alias = Polyglot.Todo.Models.User;
using static Polyglot.Todo.Constants;
using Polyglot.Todo.Models;

namespace Polyglot.Todo.Services;

public delegate User Factory(User current);

public class Worker : BaseWorker, IDisposable {
	private readonly List<User> _users = new();
	public Status Status { get; init; }
	public event EventHandler<User>? Updated;
	public string this[int index] => _users[index].Name;

	public static implicit operator Worker(User user) => new();

	public User Build(User current, List<Order> orders) {
		var repo = new UserRepo();
		return new User();
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)

	require.ElementsMatch(t, []ImportEdge{
		{Module: "System.Net.Http"},
		{Module: "Polyglot.Todo.Models"},
	}, summary.Imports)
	require.NotContains(t, summary.Imports, ImportEdge{Module: "Polyglot.Todo.Models.User"})
	require.NotContains(t, summary.Imports, ImportEdge{Module: "Polyglot.Todo.Constants"})

	require.Contains(t, summary.Symbols, Symbol{Lang: LangCs, Kind: SymType, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services", Name: "Factory", StartLine: 9, EndLine: 9})
	require.Contains(t, summary.Symbols, Symbol{Lang: LangCs, Kind: SymField, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services.Worker", Name: "Updated", StartLine: 14, EndLine: 14})
	require.Contains(t, summary.Symbols, Symbol{Lang: LangCs, Kind: SymMethod, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services.Worker", Name: "this[]", StartLine: 15, EndLine: 15})
	require.Contains(t, summary.Symbols, Symbol{Lang: LangCs, Kind: SymMethod, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services.Worker", Name: "op_Implicit_Worker", StartLine: 17, EndLine: 17})

	typeRefs := map[string]bool{}
	for _, tr := range summary.TypeRefs {
		typeRefs[tr.TypeSym.Pkg+"."+tr.TypeSym.Name] = true
	}
	require.True(t, typeRefs["Polyglot.Todo.Models.BaseWorker"])
	require.True(t, typeRefs["Polyglot.Todo.Models.IDisposable"])
	require.True(t, typeRefs["Polyglot.Todo.Models.List"])
	require.True(t, typeRefs["Polyglot.Todo.Models.User"])
	require.True(t, typeRefs["Polyglot.Todo.Models.Status"])
	require.True(t, typeRefs["Polyglot.Todo.Models.EventHandler"])
	require.True(t, typeRefs["Polyglot.Todo.Models.Order"])
	require.True(t, typeRefs["Polyglot.Todo.Models.UserRepo"])

	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models", Name: "UserRepo"},
		OwnerFQN:     "Polyglot.Todo.Services.Worker.Build",
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/Worker.cs",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"},
		OwnerFQN:     "Polyglot.Todo.Services.Worker.Build",
	})
}

func TestCSharpIndexer_ResolvesTypedReceiversConservatively(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class SyncClient {
	public void PushParam(string payload) {}
	public void PushLocal(string payload) {}
	public void PushField(string payload) {}
}

public class Worker {
	private readonly SyncClient _client = new();

	public object GetUnknown() => new();

	public void Run(SyncClient client) {
		SyncClient other = new();
		client.PushParam("a");
		other.PushLocal("b");
		_client.PushField("c");
		var unknown = GetUnknown();
		unknown.PushUpdates("d");
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)

	for _, name := range []string{"PushParam", "PushLocal", "PushField"} {
		require.Contains(t, summary.Calls, CallSite{
			File:         "csharp/Worker.cs",
			CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.SyncClient", Name: name},
			OwnerFQN:     "Polyglot.Todo.Worker.Run",
		}, name)
	}
	for _, call := range summary.Calls {
		require.NotEqual(t, SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.unknown", Name: "PushUpdates"}, call.CalleeSymbol)
	}
}

func TestCSharpIndexer_UsingResolutionAndTargetTypedNew(t *testing.T) {
	content := []byte(`
using Alias = Polyglot.Todo.Models.User;
using Polyglot.Todo.Models;

namespace Polyglot.Todo.Services;

public class UserHandler {
	public Alias Build(Alias current) {
		Alias next = new();
		return new();
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "UserHandler.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)
	require.Contains(t, summary.Imports, ImportEdge{Module: "Polyglot.Todo.Models"})

	require.Contains(t, summary.TypeRefs, TypeRef{
		File:     "csharp/UserHandler.cs",
		OwnerFQN: "Polyglot.Todo.Services.UserHandler.Build",
		TypeSym:  SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"},
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/UserHandler.cs",
		OwnerFQN:     "Polyglot.Todo.Services.UserHandler.Build",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"},
	})
}

func TestCSharpIndexer_UsingNamespaceResolutionBeatsCurrentNamespaceFallback(t *testing.T) {
	content := []byte(`
using Polyglot.Todo.Models;

namespace Polyglot.Todo.Services;

public class UserHandler {
	public User Build(User current) {
		User next = new();
		return new();
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "UserHandler.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)

	require.Contains(t, summary.TypeRefs, TypeRef{
		File:     "csharp/UserHandler.cs",
		OwnerFQN: "Polyglot.Todo.Services.UserHandler.Build",
		TypeSym:  SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"},
	})
	require.NotContains(t, summary.TypeRefs, TypeRef{
		File:     "csharp/UserHandler.cs",
		OwnerFQN: "Polyglot.Todo.Services.UserHandler.Build",
		TypeSym:  SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Services", Name: "User"},
	})
	require.Contains(t, summary.Calls, CallSite{
		File:         "csharp/UserHandler.cs",
		OwnerFQN:     "Polyglot.Todo.Services.UserHandler.Build",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"},
	})
	require.NotContains(t, summary.Calls, CallSite{
		File:         "csharp/UserHandler.cs",
		OwnerFQN:     "Polyglot.Todo.Services.UserHandler.Build",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Services", Name: "User"},
	})
}

func TestCSharpIndexer_LocalTypeShadowsImportedNamespaceGuess(t *testing.T) {
	content := []byte(`
using Polyglot.Todo;

namespace Polyglot.Todo.Worker;

public class SyncGateway {
    public void Push(string payload) { SyncClient.PushUpdates(payload); }
}

public class TypedWorker {
    private readonly SyncGateway _gateway = new();
    public void Run() {
        SyncGateway local = new();
        _gateway.Push("field");
        local.Push("local");
    }
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)
	summary, err := idx.IndexFile(content, csCodeRef(t, root, filepath.Join(root, "csharp", "TypedWorker.cs")))
	require.NoError(t, err)

	want := CallSite{
		File: "csharp/TypedWorker.cs", OwnerFQN: "Polyglot.Todo.Worker.TypedWorker.Run",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Worker.SyncGateway", Name: "Push"},
	}
	count := 0
	for _, call := range summary.Calls {
		if call == want {
			count++
		}
	}
	require.Equal(t, 1, count, "deduped caller relationship must resolve to the local SyncGateway")
	require.Contains(t, summary.Calls, CallSite{
		File: "csharp/TypedWorker.cs", OwnerFQN: "Polyglot.Todo.Worker.SyncGateway.Push",
		CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.SyncClient", Name: "PushUpdates"},
	})
}

func TestCSharpIndexer_TypedCopyFromAlias(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class SyncClient {
	public void PushCopy(string payload) {}
	public void PushUpdates(string payload) {}
}

public class Worker {
	public SyncClient Build() => new();

	public void Run(SyncClient client) {
		var alias = client;
		SyncClient copied = alias;
		var rebound = Build();
		copied.PushCopy("b");
		rebound.PushUpdates("d");
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)

	for _, name := range []string{"PushCopy"} {
		require.Contains(t, summary.Calls, CallSite{
			File:         "csharp/Worker.cs",
			CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.SyncClient", Name: name},
			OwnerFQN:     "Polyglot.Todo.Worker.Run",
		}, name)
	}
	// Invocation-return receiver inference is not part of this conservative contract.
	for _, call := range summary.Calls {
		require.NotEqual(t, "PushUpdates", call.CalleeSymbol.Name)
	}
}

func TestCSharpIndexer_GeneratedFilesStayThin(t *testing.T) {
	content := []byte(`
// <auto-generated>
namespace Polyglot.Todo.Generated;

public partial class Snapshot {
	private readonly int _version = 1;
	public int Version { get; init; } = 1;
	public void Build() {}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "Snapshot.Designer.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymClass,
		File:      "csharp/Snapshot.Designer.cs",
		Pkg:       "Polyglot.Todo.Generated",
		Name:      "Snapshot",
		StartLine: 5,
		EndLine:   9,
	})
	require.Contains(t, summary.Symbols, Symbol{
		Lang:      LangCs,
		Kind:      SymMethod,
		File:      "csharp/Snapshot.Designer.cs",
		Pkg:       "Polyglot.Todo.Generated.Snapshot",
		Name:      "Build",
		StartLine: 8,
		EndLine:   8,
	})
	for _, sym := range summary.Symbols {
		require.False(t, sym.Kind == SymField && (sym.Name == "_version" || sym.Name == "Version"), "generated field leaked: %+v", sym)
	}

}

func TestCSharpIndexer_StaticMemberRefs(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo.Constants;

public static class LegalStatuses {
	public static class Pcs {
		public const string Active = "active";
		public static readonly HashSet<string> TerminalStates = new();
	}
}

public class Worker {
	public bool Match(string status) {
		return status == LegalStatuses.Pcs.Active || LegalStatuses.Pcs.TerminalStates.Contains(status);
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)
	require.Contains(t, summary.MemberRefs, MemberRef{
		File:     "csharp/Worker.cs",
		OwnerFQN: "Polyglot.Todo.Constants.Worker.Match",
		Sym:      SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Constants.LegalStatuses.Pcs", Name: "Active"},
	})
	require.Contains(t, summary.MemberRefs, MemberRef{
		File:     "csharp/Worker.cs",
		OwnerFQN: "Polyglot.Todo.Constants.Worker.Match",
		Sym:      SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Constants.LegalStatuses.Pcs", Name: "TerminalStates"},
	})
}

func TestCSharpIndexer_StaticMemberRefs_IgnoreLowercaseInstanceChains(t *testing.T) {
	content := []byte(`
namespace Polyglot.Todo;

public class Worker {
	public string Run(dynamic unknown) {
		return unknown.Status.Value;
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Empty(t, summary.MemberRefs)
}

func TestCSharpIndexer_StaticMemberRefs_FromImportedTypeNamespaceCandidates(t *testing.T) {
	content := []byte(`
using Polyglot.Todo.Constants;
using Polyglot.Todo.Constants.CourtView;
using Polyglot.Todo.Services.Translation.Selectors.Records;

namespace Polyglot.Todo.Services.Translation.Selectors.CourtViewSelector;

public static class Worker {
	public static string Run() {
		return DispositionTermCodes.Code6FN;
	}
}
`)
	root := t.TempDir()
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "csharp", "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, []MemberRef{{
		File:     "csharp/Worker.cs",
		OwnerFQN: "Polyglot.Todo.Services.Translation.Selectors.CourtViewSelector.Worker.Run",
		Sym: SymbolRef{
			Lang: LangCs,
			Pkg:  "Polyglot.Todo.Constants.CourtView.DispositionTermCodes",
			Name: "Code6FN",
		},
	}}, summary.MemberRefs)
}

func TestCSharpIndexer_GlobalUsingAliasNestedTypesAndNameof(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Polyglot.csproj"), []byte("<Project />"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "GlobalUsing.cs"), []byte(`
global using CourtViewTranslationModel = Polyglot.Todo.Models.CourtView;
`), 0o644))
	content := []byte(`
using System.Collections.Generic;

namespace Polyglot.Todo.Services;

public class CourtView {
	public class CaseCommentOverflow {
		public string Text { get; init; } = "";
	}
}

public class Worker {
	public List<CourtViewTranslationModel.CaseCommentOverflow> Build() {
		var item = new CourtViewTranslationModel.CaseCommentOverflow();
		_ = typeof(CourtViewTranslationModel.CaseCommentOverflow);
		_ = nameof(CourtViewTranslationModel.CaseCommentOverflow.Text);
		return new List<CourtViewTranslationModel.CaseCommentOverflow> { item };
	}
}
`)
	idx := NewCSharpIndexerWithRoot(root)

	absPath := filepath.Join(root, "Worker.cs")
	summary, err := idx.IndexFile(content, csCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Contains(t, summary.TypeRefs, TypeRef{
		File:     "Worker.cs",
		OwnerFQN: "Polyglot.Todo.Services.Worker.Build",
		TypeSym:  SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models.CourtView", Name: "CaseCommentOverflow"},
	})
	require.Contains(t, summary.MemberRefs, MemberRef{
		File:     "Worker.cs",
		OwnerFQN: "Polyglot.Todo.Services.Worker.Build",
		Sym:      SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models.CourtView.CaseCommentOverflow", Name: "Text"},
	})
}
