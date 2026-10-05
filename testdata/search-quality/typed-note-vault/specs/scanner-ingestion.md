---
summary: Required behavior for scanner event acceptance and reconciliation.
---
# Scanner Ingestion Specification

Every scan includes a device event ID, barcode, shipment, and observed time. Replays with the same event ID return the original acceptance. Different event IDs remain distinct even when barcode and timestamp match.
