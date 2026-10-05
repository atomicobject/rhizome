---
type: Decision
name: Scan Deduplication
status: accepted
tags: [type/decision]
summary: Deduplicate scans by device event ID, not timestamp or barcode.
---
# Scan Deduplication

The same barcode can be scanned more than once legitimately. A device event ID identifies one submission across retries, so timestamps and barcode values are never deduplication keys.
