---
summary: "Navigation hub for the bundled Agentic Engineering starter, its team extension docs, and specification-delivery substrate."
tags: [type/hub, subsystem/agentic-engineering]
---

# Agentic Engineering starter (Hub)

## Purpose

The Agentic Engineering starter is Rhizome's repository harness for software delivery work. It combines a lean workflow router that takes the phase as an argument, two separate workflow skills, team-owned concern documents under `docs/engineering/`, and the specification-delivery ontology/query substrate.

It does not prescribe a complete organizational process. Installed repositories tune their policy in `docs/engineering/`; schema and ontology mechanics stay managed with the core `rhizome` guidance.

## Start here

1. [Choosing Your Starter](../reference/guides/choosing-your-starter.md) for adoption and composition choices.
2. [[Agentic Engineering starter - adoption guide]] for scaffold and migration behavior.
3. [[Agentic Engineering starter - workflow]] for the delivery route.
4. [[Agent Skills (Hub)]] for installed-skill ownership.
5. [[Init (Hub)]] for source templates and refresh behavior.

## Delivery model

The normal sequence is classify -> specify -> effort -> plan -> implement -> verify -> close. `agentic-engineering` routes an unclear request and takes the phase as its first argument (`agentic-engineering plan`). `foundation-review` is a plan-requested pause on formative decisions; `ingest-transcript` turns provenance-bearing sources into durable context. Team policy is factored by concern in `docs/engineering/` (testing, quality gates, documentation, review and approval, architecture, release) and outranks skill defaults.

Use an effort when scope must remain frozen across phases. Use a foundation review when early work sets a durable contract. Keep commands and test expectations local to the repository rather than assuming Rhizome supplies them.

## Domain-accurate assets

The specification ontology and query bundle remain named `spec-driven.graphql` and `spec-driven.yaml` where that accurately describes their modeled domain. `complex-domain` builds on `agentic-engineering` while adding source-backed requirements and traceability.
