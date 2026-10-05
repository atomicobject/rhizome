# Progressive code-mode fixture

This fixture gives the code-mode comparison one small repository with two
known Python paths, typed context, and a bounded requirement-coverage corpus.
The current notes are governing context; the dated analysis notes are
historical supporting evidence. The candidate requirement is useful context
but is not accepted scope.

`E01-progressive-context/` is the valid model-visible repository.
`E02-invalid-source-trace/` is a separate negative case: its malformed source
and missing trace targets are deliberate and must be reported as invalid
rather than repaired or promoted.

The valid case supports these reads:

- exact code context for `src/expiry.py` and `src/retention.py`
- current policy plus superseded historical evidence for each path
- one accepted requirement without traceability
- one accepted requirement with source, spec, story, and acceptance-criterion
  references
- one candidate requirement that must remain outside accepted coverage

The fixture contains no oracle or expected model answer. Evaluation manifests
choose the valid repository or the marked negative subtree explicitly.
