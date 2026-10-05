package codeanchor

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// BuildSymbolRefRows extracts deduped symbol reference rows from a summary.
//
// These rows are the durable, language-neutral reverse index for calls, type
// refs, and member refs. Dedupe by owner/ref-kind/destination so later rebuilds
// can resolve changed symbols without replaying parser-specific duplicates.
func BuildSymbolRefRows(srcPath string, summary FileSummary) []SymbolRefRow {
	srcPath = string(paths.NormalizeCode(srcPath))
	if srcPath == "" {
		return nil
	}
	seen := make(map[SymbolRefRow]struct{})
	addRow := func(owner string, kind RefKind, ref SymbolRef, fallbackLang Lang) {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			return
		}
		lang := ref.Lang
		if lang == "" {
			lang = fallbackLang
		}
		pkg := strings.TrimSpace(ref.Pkg)
		ref.Lang = lang
		ref.Pkg = pkg
		ref.Name = name
		fqn := normalizeSymbol(ref)
		row := SymbolRefRow{
			SrcPath:   srcPath,
			OwnerFQN:  strings.TrimSpace(owner),
			RefKind:   kind,
			DstLang:   lang,
			DstPkg:    pkg,
			DstName:   name,
			DstFQN:    fqn,
			DstMember: lang == LangPhp && ref.Member,
		}
		seen[row] = struct{}{}
	}

	for _, call := range summary.Calls {
		addRow(call.OwnerFQN, RefKindCalls, call.CalleeSymbol, summary.Lang)
	}
	for _, tr := range summary.TypeRefs {
		addRow(tr.OwnerFQN, RefKindTypeRef, tr.TypeSym, summary.Lang)
	}
	for _, mr := range summary.MemberRefs {
		addRow(mr.OwnerFQN, RefKindMemberRef, mr.Sym, summary.Lang)
	}

	if len(seen) == 0 {
		return nil
	}
	rows := make([]SymbolRefRow, 0, len(seen))
	for row := range seen {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].OwnerFQN != rows[j].OwnerFQN {
			return rows[i].OwnerFQN < rows[j].OwnerFQN
		}
		if rows[i].RefKind != rows[j].RefKind {
			return rows[i].RefKind < rows[j].RefKind
		}
		if rows[i].DstLang != rows[j].DstLang {
			return rows[i].DstLang < rows[j].DstLang
		}
		if rows[i].DstPkg != rows[j].DstPkg {
			return rows[i].DstPkg < rows[j].DstPkg
		}
		return rows[i].DstName < rows[j].DstName
	})
	return rows
}

// BuildImportRefRows extracts deduped import reference rows from a summary.
//
// TypeScript imports may already resolve to absolute filesystem paths; when a
// vault root is available, convert those back to vault-relative code paths so
// import edges join against the same persisted key space as files.
func BuildImportRefRows(srcPath string, summary FileSummary, vault paths.VaultPaths) []ImportRefRow {
	srcPath = string(paths.NormalizeCode(srcPath))
	if srcPath == "" || len(summary.Imports) == 0 {
		return nil
	}
	seen := make(map[string]ImportRefRow)
	for _, imp := range summary.Imports {
		module := strings.TrimSpace(imp.Module)
		if module == "" {
			continue
		}
		if summary.Lang == LangTS {
			if rel, err := vault.RelCodeStrict(module); err == nil && rel.String() != "" {
				module = rel.String()
			}
		}
		if _, ok := seen[module]; ok {
			continue
		}
		seen[module] = ImportRefRow{
			SrcPath: srcPath,
			Module:  module,
		}
	}
	if len(seen) == 0 {
		return nil
	}
	rows := make([]ImportRefRow, 0, len(seen))
	for _, row := range seen {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Module < rows[j].Module
	})
	return rows
}

// BuildModuleDefRows extracts module identity rows for a file.
//
// Module definitions are not presentation labels. They are resolver keys used by
// later rebuilds to connect imports and refs after batch ingest has completed.
func BuildModuleDefRows(srcPath string, summary FileSummary, idx LanguageIndexer) []ModuleDefRow {
	srcPath = string(paths.NormalizeCode(srcPath))
	if srcPath == "" {
		return nil
	}
	switch summary.Lang {
	case LangPy:
		var roots []string
		if py, ok := idx.(*PythonIndexer); ok {
			roots = py.sourceRoots
		}
		mod := pythonModuleFromPath(srcPath, roots)
		if strings.TrimSpace(mod) == "" {
			return nil
		}
		return []ModuleDefRow{{
			SrcPath: srcPath,
			Lang:    LangPy,
			Module:  mod,
		}}
	case LangTS:
		return []ModuleDefRow{{
			SrcPath: srcPath,
			Lang:    LangTS,
			Module:  srcPath,
		}}
	case LangGo:
		mod := goImportPathFromSymbols(summary.Symbols)
		if strings.TrimSpace(mod) == "" {
			return nil
		}
		return []ModuleDefRow{{
			SrcPath: srcPath,
			Lang:    LangGo,
			Module:  mod,
		}}
	case LangCs:
		mod := csharpNamespaceFromSymbols(summary.Symbols)
		if strings.TrimSpace(mod) == "" {
			return nil
		}
		return []ModuleDefRow{{
			SrcPath: srcPath,
			Lang:    LangCs,
			Module:  mod,
		}}
	case LangPhp:
		mod := phpNamespaceFromSymbols(summary.Symbols)
		if strings.TrimSpace(mod) == "" {
			// fall back to file path for global-scope (procedural) PHP
			mod = srcPath
		}
		return []ModuleDefRow{{
			SrcPath: srcPath,
			Lang:    LangPhp,
			Module:  mod,
		}}
	default:
		return nil
	}
}

func phpNamespaceFromSymbols(symbols []Symbol) string {
	for _, sym := range symbols {
		pkg := strings.TrimSpace(sym.Pkg)
		if pkg != "" {
			return pkg
		}
	}
	return ""
}

func goImportPathFromSymbols(symbols []Symbol) string {
	for _, sym := range symbols {
		pkg := strings.TrimSpace(sym.Pkg)
		if pkg == "" {
			continue
		}
		if slash := strings.LastIndex(pkg, "/"); slash >= 0 {
			if dotAfterSlash := strings.Index(pkg[slash:], "."); dotAfterSlash >= 0 {
				pkg = pkg[:slash+dotAfterSlash]
			}
		}
		pkg = strings.TrimSpace(pkg)
		if pkg != "" {
			return pkg
		}
	}
	return ""
}

func csharpNamespaceFromSymbols(symbols []Symbol) string {
	shortest := ""
	for _, sym := range symbols {
		if sym.Kind == SymMethod || sym.Kind == SymField {
			continue
		}
		pkg := strings.TrimSpace(sym.Pkg)
		if pkg == "" {
			continue
		}
		if shortest == "" || len(pkg) < len(shortest) {
			shortest = pkg
		}
	}
	if shortest != "" {
		return shortest
	}
	for _, sym := range symbols {
		pkg := strings.TrimSpace(sym.Pkg)
		if pkg == "" {
			continue
		}
		if idx := strings.LastIndex(pkg, "."); idx > 0 {
			return pkg[:idx]
		}
		return pkg
	}
	return ""
}
