---
summary: "Feature areas organize specs and delivery work around product or system surfaces."
---

# Feature areas

## Summary

Feature area notes group related specs by the user-facing or system-facing area they affect.

## Authoring

Create one note per durable area under this folder. Use `type: FeatureArea`, an `id`, a short `summary`, and optional `owner`.

Link specs to feature areas with `feature-areas:` frontmatter. A spec may belong to more than one area when the slice genuinely crosses ownership boundaries.

## Parent areas

A feature area may sit under one broader area. Set `parent:` frontmatter to the broader area when this area is a genuine subdivision of it, and leave it empty for a top-level area:

```yaml
---
type: FeatureArea
id: FA-0007
summary: Saved views and their layouts.
parent: "[[views-and-navigation]]"
---
```

Attach each spec to the most specific area that fits; its ancestors already provide the broader context. Body links and folder placement never set a parent, and validation reports a parent chain that loops back on itself.
