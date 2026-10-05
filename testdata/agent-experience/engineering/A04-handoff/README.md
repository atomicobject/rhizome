# Fresh-agent handoff fixture

This repository is an approved two-phase retry change. A producer invocation
must finish phase 1 and leave durable context before a separate consumer
invocation continues phase 2.

The zero-attempt edge case is intentionally absent from the short brief. The
producer must inspect the actual behavior and document it before the consumer
changes the implementation.
