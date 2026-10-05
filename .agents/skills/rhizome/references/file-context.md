# File context

Use `file-context` when the path is known and the agent needs the contracts around that file or directory: ancestor docs, code anchors, coderefs, related notes, and nearby dependencies.

```bash
rzm agent file-context --session-id <id> \
  --file <path> \
  --intent "<what you will do with this file>"
```

The file flag may be repeated, but all targets share a context budget. Prefer one high-value directory or a few non-overlapping entrypoints. Put the most important target first. Do not blanket-contextualize trivial helpers or files already covered in the session.

Use `--submodule-depth 1` only when child-module documentation matters. Apply the core skill's evidence-authority rule: ancestor docs, code anchors, coderefs, and companion bindings establish relevance, not authority. Inspect scope, provenance, and lifecycle before treating a claim as a constraint. If behavior changes an existing durable explanation, update its nearest owning documentation surface and rerun focused context when that verifies the binding.

Reuse current context already covering the affected boundary. Stop when applicable constraints and evidence for the next decision are understood; expand only for a concrete gap or changed scope.

If the path is unknown or the question crosses multiple areas, start with `semantic-query`. If the question needs an exact symbol or reference, use the code evidence tools instead.
