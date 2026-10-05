# Architecture

Consulted when placing code, crossing a boundary, or reusing or adding a pattern. Used during planning and implementation.

## Defaults

Reuse an established pattern before proposing an abstraction. Keep a change inside the module that owns the behavior; when it must cross a boundary, name the boundary and the contract it depends on in the plan.

Treat a new schema, public contract, ownership boundary, or reusable pattern as a decision to surface, with its tension and a recommendation, rather than something to introduce quietly inside an implementation.

## Team extensions

Load the owning subsystem note or `<name>-subsystem` skill when a change touches behavior, contracts, or invariants, or when ownership is unclear; a mechanical edit needs no preflight. The constraints in those notes are normative, and a change that moves an invariant updates the note in the same change set and bumps `last-verified`. Map: [subsystems overview](../reference/subsystems/overview.md), folder to note table: [subsystems README](../reference/subsystems/README.md). Keep code files under about 500 lines and split as needed. No backward-compatibility shims: simplify and delete, and provide migration paths instead. The typed query layer, stores single-writer rule, and init template sources of truth (`docs/rhizome-md-templates/*`, `pkg/app/cli/init/templates/**`) are the boundaries most often crossed by accident.
