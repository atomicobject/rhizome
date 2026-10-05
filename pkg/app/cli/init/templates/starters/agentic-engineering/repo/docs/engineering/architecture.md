# Architecture

Consulted when placing code, crossing a boundary, or reusing or adding a pattern. Used during planning and implementation.

## Defaults

Reuse an established pattern before proposing an abstraction. Keep a change inside the module that owns the behavior; when it must cross a boundary, name the boundary and the contract it depends on in the plan.

Treat a new schema, public contract, ownership boundary, or reusable pattern as a decision to surface, with its tension and a recommendation, rather than something to introduce quietly inside an implementation.

## Team extensions

List the subsystems and their owners, the boundaries that must not be crossed without review, the patterns to prefer, and the ones being retired. Point to the architecture records or subsystem notes that hold the reasoning.
