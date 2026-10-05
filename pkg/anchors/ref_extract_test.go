package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSymbolRefRows_IncludesMemberRefs(t *testing.T) {
	rows := BuildSymbolRefRows("csharp/Worker.cs", FileSummary{
		FilePath: "csharp/Worker.cs",
		Lang:     LangCs,
		Calls: []CallSite{{
			File:         "csharp/Worker.cs",
			OwnerFQN:     "Polyglot.Todo.Worker.Run",
			CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.SyncClient", Name: "Push"},
		}},
		TypeRefs: []TypeRef{{
			File:     "csharp/Worker.cs",
			OwnerFQN: "Polyglot.Todo.Worker.Run",
			TypeSym:  SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"},
		}},
		MemberRefs: []MemberRef{{
			File:     "csharp/Worker.cs",
			OwnerFQN: "Polyglot.Todo.Worker.Run",
			Sym:      SymbolRef{Lang: LangCs, Pkg: "Polyglot.Todo.Constants.LegalStatuses.Pcs", Name: "Active"},
		}},
	})
	require.Len(t, rows, 3)
	require.Contains(t, rows, SymbolRefRow{
		SrcPath:  "csharp/Worker.cs",
		OwnerFQN: "Polyglot.Todo.Worker.Run",
		RefKind:  RefKindMemberRef,
		DstLang:  LangCs,
		DstPkg:   "Polyglot.Todo.Constants.LegalStatuses.Pcs",
		DstName:  "Active",
		DstFQN:   "Polyglot.Todo.Constants.LegalStatuses.Pcs.Active",
	})
}

func TestBuildSymbolRefRows_PreservesPHPMemberIdentity(t *testing.T) {
	rows := BuildSymbolRefRows("php/Worker.php", FileSummary{
		FilePath: "php/Worker.php",
		Lang:     LangPhp,
		Calls: []CallSite{{
			File: "php/Worker.php", OwnerFQN: `Polyglot\Todo\Worker::run`,
			CalleeSymbol: SymbolRef{Lang: LangPhp, Pkg: `Polyglot\Todo\SyncClient`, Name: "pushUpdates", Member: true},
		}, {
			OwnerFQN:     ` Polyglot\Todo\Worker::run `,
			CalleeSymbol: SymbolRef{Pkg: ` Polyglot\Todo\SyncClient `, Name: " pushUpdates ", Member: true},
		}, {
			OwnerFQN:     `Polyglot\Todo\Worker::run`,
			CalleeSymbol: SymbolRef{Lang: LangPhp, Pkg: `Polyglot\Todo\SyncClient`, Name: "pushUpdates"},
		}},
	})
	require.ElementsMatch(t, []SymbolRefRow{{
		SrcPath: "php/Worker.php", OwnerFQN: `Polyglot\Todo\Worker::run`, RefKind: RefKindCalls,
		DstLang: LangPhp, DstPkg: `Polyglot\Todo\SyncClient`, DstName: "pushUpdates", DstFQN: `Polyglot\Todo\SyncClient::pushUpdates`, DstMember: true,
	}, {
		SrcPath: "php/Worker.php", OwnerFQN: `Polyglot\Todo\Worker::run`, RefKind: RefKindCalls,
		DstLang: LangPhp, DstPkg: `Polyglot\Todo\SyncClient`, DstName: "pushUpdates", DstFQN: `Polyglot\Todo\SyncClient\pushUpdates`,
	}}, rows)
}
