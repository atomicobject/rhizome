---
summary: Required behavior for dispatch ordering, technician completion, and reconnect conflicts.
---
# Route Assignment Specification

## Assignment ordering

Dispatch controls the order of unstarted stops. Completed stops never return to the active route.

## Offline completion

The client records a stable operation UUID before showing success. Upload retries reuse that UUID.

## Conflict review

A stale assignment version creates a review item containing both versions and does not overwrite the server route.
