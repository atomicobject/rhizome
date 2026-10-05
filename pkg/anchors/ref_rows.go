package codeanchor

// RefKind identifies the kind of symbol reference stored in the reverse index.
type RefKind string

const (
	RefKindCalls     RefKind = "calls"
	RefKindTypeRef   RefKind = "type_ref"
	RefKindMemberRef RefKind = "member_ref"
)

// SymbolRefRow is a persisted, deduped symbol reference signal.
// SrcPath is vault-relative, normalized code path.
type SymbolRefRow struct {
	SrcPath  string
	OwnerFQN string
	RefKind  RefKind

	DstLang Lang
	DstPkg  string
	DstName string
	DstFQN  string
	// DstMember distinguishes PHP Class::member refs from namespace\symbol
	// refs that otherwise share the same package and name components.
	DstMember bool
}

// ImportRefRow is a persisted, deduped import/module reference signal.
// SrcPath is vault-relative, normalized code path.
type ImportRefRow struct {
	SrcPath string
	Module  string
}

// ModuleDefRow is a persisted module identity for a source file.
// Module values must align with language import resolution.
type ModuleDefRow struct {
	SrcPath string
	Lang    Lang
	Module  string
}
