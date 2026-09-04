# ADR 0001: Keep Eino as the Runtime Kernel

## Status

Accepted

## Context

OnCall already uses Eino/ADK for Agent execution, middleware, graph-like workflows, interrupts, checkpointing, resume, and streaming. The refactor needs stronger OnCall-owned domain seams without rebuilding runtime primitives that Eino already provides.

## Decision

Keep Eino as the runtime kernel. OnCall modules own domain state, routing, policy, event contracts, audit records, retrieval snapshots, and workflow definitions. Eino types may appear at adapter and execution seams, but domain contracts should not require callers to understand Eino internals.

## Consequences

- Runtime capabilities stay incremental and testable.
- OnCall avoids a custom ReAct/Graph runtime fork.
- Adapters must isolate version-specific Eino details from core domain modules.
