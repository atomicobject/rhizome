# Traceability Links

When code, tests, comments, coderefs, or code anchors need to cite requirement context, link to the smallest durable ontology node that explains the local constraint:

1. Use an acceptance-criterion link when the code or test satisfies one observable criterion.
2. Use a user-story link when the implementation spans several criteria for one story.
3. Use the parent spec link when the behavior is cross-story or spec-wide.
4. Use a reference, effort-local rationale entry, or rationale node when the citation explains architecture, provenance, or trade-off rather than requirement scope.

Do not invent `filename#SPEC-...` or heading-only links by hand. Use recipe `locator.wikilink` / `locator.linkTarget` values when they are present, or ask the helper for the durable target:

```bash
rzm agent node-link --target '<spec-path>#<story-or-criterion-heading-or-id>' --ensure plan
rzm agent node-link --ref '{"notePath":"<spec-path>","nodeId":"<story-node-id>","typeName":"UserStory","kind":"EMBEDDED"}' --ensure plan
```

If `node-link` or a recipe locator reports `requiresFix`, use a write-capable Rhizome surface only when mutation is authorized, then rerun the helper and use the returned `linkTarget.wikilink`. If the target cannot be resolved, link to the nearest durable parent and record the missing precise target as follow-up instead of fabricating a locator.
