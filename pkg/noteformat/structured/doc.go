// Structured parsing uses no filesystem I/O, shared envelopes, or schema logic.
// ParseYAML and ParseJSON return either an immutable root or diagnostics, never
// both. Traverse Items and Entries in order for document-order RFC 6901 addresses.
// An absent Lookup differs from a present Null, empty String, Array, or Object.
//
// ScalarProfile follows the pinned PyYAML 6.0.3 resolver. YAML integer values are
// arbitrary precision decimal text. Floats retain float spelling and are accepted
// only when conversion through SafeLoader's binary64 domain preserves decimal
// value. Sexagesimal, nonfinite, overflowing, underflowing, and lossy numbers are
// rejected. Dates and offset timestamps retain calendar and offset evidence;
// timezone-free, invalid, noncivil-offset, and sub-microsecond timestamps are
// rejected. Quoted and block scalars remain strings. Lexeme contains the parser's
// decoded scalar spelling, not the original quoted bytes; use the source for raw
// syntax. Position claims starts only, with no inferred end ranges.
//
// testdata/scalar-oracle.json contains literal SafeLoader expectations, compared
// with CSafeLoader during generation. testdata/regenerate.py documents manual
// regeneration in a disposable venv. Tests never invoke Python or install tools.
package structured
