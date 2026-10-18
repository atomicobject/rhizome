package store

// CodeStats is the JSON output contract for `rzm code stats`.
// Its JSON fields are consumed by code-coverage audit clients.
type CodeStats struct {
	Languages                map[string]LanguageCounts               `json:"languages"`
	Edges                    map[string]EdgeCounts                   `json:"edges"`
	ReferenceClassifications map[string]ReferenceClassificationStats `json:"referenceClassifications"`
	UnresolvedCallees        map[string][]UnresolvedCallee           `json:"unresolvedCallees"`
	ByDirectory              map[string][]DirectoryDistribution      `json:"byDirectory"`
}

// LanguageCounts summarizes what got indexed for a single language.
type LanguageCounts struct {
	Symbols int `json:"symbols"`
	Files   int `json:"files"`
	Modules int `json:"modules"`
}

// EdgeCounts measures the extraction-vs-resolution gap for a single language.
// RATIONALE: the LEFT JOIN distinction between extracted and resolved is the
// load-bearing diagnostic signal. CallsExtracted - CallsResolved = gap surface.
type EdgeCounts struct {
	CallsExtracted  int     `json:"callsExtracted"`
	CallsResolved   int     `json:"callsResolved"`
	CallsUnresolved int     `json:"callsUnresolved"`
	ResolutionRate  float64 `json:"resolutionRate"`
	MemberRefs      int     `json:"memberRefs"`
	Imports         int     `json:"imports"`
}

// UnresolvedCallee is one row in the per-language top-N unresolved-callees list.
// NOTE: language built-ins (e.g. PHP `empty`, `isset`, `sprintf`) dominate this
// list. The audit phase classifies them via a coverage catalog and filters via
// the --exclude-from flag.
type UnresolvedCallee struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
}

// DirectoryDistribution shows top-level directory file distribution per language.
type DirectoryDistribution struct {
	Dir   string `json:"dir"`
	Lang  string `json:"lang"`
	Files int    `json:"files"`
}

// ClassificationCountSet is the exhaustive, disjoint reference-classification
// vocabulary. Fields are intentionally explicit so consumers do not have to
// infer a missing class from a sparse map.
type ClassificationCountSet struct {
	LocalResolved        int `json:"local_resolved"`
	ExternalClassified   int `json:"external_classified"`
	RuntimeGlobalBuiltin int `json:"runtime_global_builtin"`
	Unknown              int `json:"unknown"`
}

// ReferenceKindClassificationStats reports use counts separately from target
// identity counts. Multiple raw spellings can therefore converge on one
// canonical external target without hiding their use-site fan-in.
type ReferenceKindClassificationStats struct {
	Associations  ClassificationCountSet `json:"associations"`
	UniqueTargets ClassificationCountSet `json:"uniqueTargets"`
}

// ReferenceClassificationStats reports all durable reference associations for
// one source language, both overall and split by the persisted relation kind.
type ReferenceClassificationStats struct {
	Associations  ClassificationCountSet                      `json:"associations"`
	UniqueTargets ClassificationCountSet                      `json:"uniqueTargets"`
	ByKind        map[string]ReferenceKindClassificationStats `json:"byKind"`
}
