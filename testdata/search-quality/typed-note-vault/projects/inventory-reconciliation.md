---
type: Project
name: Inventory Reconciliation
owner: "[[people/leo]]"
decisions:
  - "[[decisions/scan-deduplication]]"
summary: Warehouse scans are reconciled into an auditable count ledger.
---
# Inventory Reconciliation

Handheld scanners submit immutable scan events. The reconciliation service groups them by shipment and flags count disagreements for a supervisor.
