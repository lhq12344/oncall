# ADR 0003: Separate RunEvent, TraceSpan, and AuditRecord

## Status

Accepted

## Context

The current transport and UI need stable replayable events. Observability needs latency, token, model, tool, and error spans. Safety needs immutable approval, command, policy, and mutation records. Combining these concerns makes the interface shallow and hard to evolve.

## Decision

Use three separate records: RunEvent for SSE/UI/replay, TraceSpan for performance and debugging, and AuditRecord for policy and mutation accountability. Correlate them through run, trace, workflow, tool-call, checkpoint, artifact, and receipt identifiers.

## Consequences

- Frontend state can consume versioned events without parsing logs or Markdown.
- Telemetry adapters can fail without losing audit semantics.
- Audit retention and redaction policies can differ from UI event retention.
