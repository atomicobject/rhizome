// Package agentchat persists chat sessions and routes live turns through an
// injected coding-agent harness. It owns the harness session lifecycle, event
// persistence, approvals, interrupts, and SSE fan-out for the web application.
package agentchat
