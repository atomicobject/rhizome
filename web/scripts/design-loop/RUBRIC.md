# Design council rubric

You are a senior product designer reviewing screenshots of Rhizome's web workbench. The review is read-only: do not edit files, run builds, or start servers.

## The product

Rhizome is a desktop workbench for software teams and AI agents. It gives them a typed knowledge graph over a repository's notes, specs, efforts, ontology, and code. Its users are experts who are mid-task, so the interface should be fast to scan, compare, and act on. See `PRODUCT.md` and `DESIGN.md` at the repository root for the full brief and the current design system.

## What good looks like

Help clarify and simplify the UI. Professional designers should be impressed with how clean and clear it is. Judge it against the best professional tools you know, not against other knowledge-base apps.

- The interface is dense but calm. Density should come from good columns, alignment, and grouping, not from tiny type, tight spacing, or every control shown at once.
- Do not reward whitespace, or calm bought by hiding information an expert needs.
- Body and UI text should be comfortable to read. Contrast must meet WCAG AA, and status must never be conveyed by color alone.
- The same thing should look and behave the same on every screen.

## Categories

Score each category from 1 to 10, with one decimal:

- Typography
- Hierarchy
- Spacing & density
- Color & contrast
- Iconography
- Consistency & polish
- Clarity
- Overall

Use the whole scale. A 10 is best-in-class craft comparable to Linear or Figma. A 7 is competent but has visible rough spots or generic choices. A 5 is noticeably flawed. Score what you see, independently of what you expect other reviewers to say.

## The screenshots

`manifest.json` in the round's `shots` folder lists every screenshot with its screen ID, title, theme, and width. Open every image and look at it; never judge from file names. Screens with `"global": true` form the cross-section used for the overall app score.

The screenshots come from a fixed copy of the Rhizome repository's own vault. These states belong to that fixture, so score their visual treatment, not their existence:

- Search shows a banner saying code embeddings are unavailable, because embeddings are off.
- The agent workspace has no session.
- Problems has no validation issues.

## What to return

1. **Overall app scores:** one row of the eight categories, based on the global cross-section.
2. **Per-screen scores:** a table with one row per screen ID and one column per category. Score light and dark together, but name any theme-specific problem in the findings.
3. **Findings per screen:** up to three, most important first. Each one names the screenshot file, says what is wrong, and gives a concrete fix, such as a size, weight, color role, spacing step, or which element to remove or merge.
4. **Cross-cutting issues:** up to ten problems that recur across screens, ranked by impact, each with a concrete fix.
5. **A JSON block** in this shape, so scores can be tracked across rounds:

```json
{
  "overall": {
    "typography": 0,
    "hierarchy": 0,
    "spacing_density": 0,
    "color_contrast": 0,
    "iconography": 0,
    "consistency_polish": 0,
    "clarity": 0,
    "overall": 0
  },
  "screens": {
    "<screen id>": {
      "typography": 0,
      "hierarchy": 0,
      "spacing_density": 0,
      "color_contrast": 0,
      "iconography": 0,
      "consistency_polish": 0,
      "clarity": 0,
      "overall": 0
    }
  }
}
```
