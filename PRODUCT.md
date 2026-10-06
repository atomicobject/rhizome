# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

Rhizome's product UI is a desktop workbench. Narrow browser windows may remain usable where existing layouts naturally reflow, but phone-sized navigation and touch-first workflows are not product requirements.

## Users

Rhizome is for software teams and AI agents working inside real codebases, especially developers, technical leads, and delivery teams who need context that is structured, typed, local, and tied to actual project constraints. Users are often mid-task: inheriting a repository, assessing legacy code, planning work, validating implementation, or trying to keep agents from guessing.

## Product Purpose

Rhizome gives AI agents and software teams a typed knowledge graph over notes, code, ontology, specs, efforts, and retrieval surfaces so they can work with durable project context instead of reconstructing it from scratch. Success means agents and humans can quickly see what matters, trace work to constraints, and act with confidence in dense professional workflows.

## Positioning

Rhizome is a local, typed knowledge graph that binds code, documentation, ontology, specs, and execution artifacts so humans and agents can retrieve and mutate grounded project context instead of relying on generic chat or search context.

## Operating Context

Rhizome operates inside real repositories and Markdown vaults. Teams install and initialize it from the CLI, index notes and code, and start bounded agent sessions that expose repository guidance, structured retrieval, validation, graph navigation, and safe note operations. The desktop web workbench supports dense inspection of notes, ontology, relationships, provenance, status, and execution context alongside CLI-driven workflows.

## Capabilities and Constraints

- Indexes Markdown knowledge, wikilinks, frontmatter, code symbols, references, and code-to-document bindings into a unified local graph.
- Exposes typed agent surfaces for project orientation, exact code evidence, semantic retrieval, ontology queries, validation, reports, and graph-safe mutations.
- Supports spec-driven delivery artifacts such as specs, stories, efforts, requirements, and action items through repository-owned workflows.
- Keeps core operation local; semantic embeddings and external model-backed capabilities are optional and depend on configured providers.
- Optimizes the product UI for desktop working widths, keyboard access, and expert information density rather than phone-sized or touch-first use.

## Brand Commitments

The product voice is precise, grounded, practical, technical, and quietly opinionated: direct enough for expert users, restrained enough for repeated daily use, and confident without hype. Rhizome's identity is visibly inspired by Atomic Object, with AO red, warm black, teal, Merriweather, Barlow, and JetBrains Mono as established foundations unless a future reviewed redesign deliberately replaces them. The mark is the runner r: a Merriweather Black r growing out of a horizontal runner that ends in one AO-red node, with roots below; the wordmark sets "rhizome" on the same runner. SVG masters live in `docs/brand/`, and app icons are generated from them. Do not treat the temporary in-product red diamond as the final identity.

## Evidence on Hand

- The working Go CLI and desktop web application in this repository.
- Repository-owned product, technical, process, and experience specs under `docs/specs/`.
- Executable unit, integration, web, and end-to-end test suites.
- Setup, operating, and agent-surface documentation in `README.md` and `docs/reference/`.
- The Rhizome mark and wordmark in `docs/brand/`.
- No customer testimonials, adoption claims, comparative benchmarks, or external proof assets are established here; future work must not fabricate them.

## Product Principles

1. Keep structure visible. Expose relationships, provenance, status, and next actions instead of hiding complexity behind friendly summaries.
2. Optimize for expert density. Make repeated professional workflows fast to scan, compare, filter, and act on.
3. Make confidence inspectable. Retrieval, validation, and agent outputs should show enough evidence for users to understand why the system believes something.
4. Stay flexible without feeling bloated. Support multiple workflows through composable, predictable controls rather than sprawling dashboards or one-off screens.
5. Practice the product thesis. Product surfaces should demonstrate typed context, durable structure, and grounded human-agent collaboration.

## Accessibility & Inclusion

Target WCAG AA. Respect reduced-motion preferences. Do not rely on color alone for status, validation, graph state, or risk signals; pair color with labels, icons, shape, or placement. Preserve keyboard access and readable focus states for dense workflows.
