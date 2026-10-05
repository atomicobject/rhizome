package codeanchor

// RawSymbolTargetKey is the natural key of an existing parser-emitted target.
// Ingest callers use this instead of depending on SQLite row identifiers.
type RawSymbolTargetKey struct {
	DstLang Lang
	DstPkg  string
	DstName string
	DstFQN  string
}

// ExternalSymbolEvidenceInput associates one existing symbol reference with a
// canonical external target using natural keys only.
type ExternalSymbolEvidenceInput struct {
	OwnerFQN string
	RefKind  RefKind
	Raw      RawSymbolTargetKey
	Target   ExternalTarget
	Evidence ExternalEvidence
}

// ExternalImportEvidenceInput associates an external import binding (or an
// import-only module) with a canonical target using natural keys only.
type ExternalImportEvidenceInput struct {
	Module         string
	BindingOrdinal int
	Target         ExternalTarget
	Evidence       ExternalEvidence
}

// ExternalEvidenceBatch atomically replaces all external evidence owned by one
// source path. Symbol references themselves remain owned by the reverse index.
type ExternalEvidenceBatch struct {
	Symbols []ExternalSymbolEvidenceInput
	Imports []ExternalImportEvidenceInput
}

func cloneExternalEvidenceBatch(batch ExternalEvidenceBatch) ExternalEvidenceBatch {
	out := ExternalEvidenceBatch{
		Symbols: append([]ExternalSymbolEvidenceInput(nil), batch.Symbols...),
		Imports: append([]ExternalImportEvidenceInput(nil), batch.Imports...),
	}
	for i := range out.Symbols {
		out.Symbols[i].Evidence.Version = cloneExternalVersion(out.Symbols[i].Evidence.Version)
	}
	for i := range out.Imports {
		out.Imports[i].Evidence.Version = cloneExternalVersion(out.Imports[i].Evidence.Version)
	}
	return out
}

func cloneExternalVersion(version *ExternalVersionProvenance) *ExternalVersionProvenance {
	if version == nil {
		return nil
	}
	copy := *version
	return &copy
}

// ExternalReferenceClassificationRow is a read-time projection. Class is never
// persisted, so current source-backed definition evidence can take precedence.
type ExternalReferenceClassificationRow struct {
	Association SymbolRefAssociationKey
	Target      ExternalTargetRow
	Evidence    ExternalEvidence
	Class       ExternalReferenceClass
}
