# ADR 0004: Keep External Systems Behind Adapters

## Status

Accepted

## Context

OnCall depends on optional systems including Redis, Elasticsearch, Milvus, Kubernetes, Prometheus, SQL stores, object storage, MCP servers, model providers, and CozeLoop. Basic chat and read-only flows should not panic when optional systems are unavailable.

## Decision

All external SDKs live behind `internal/adapters/*` modules or transitional compatibility seams. Core modules depend on OnCall interfaces and receive adapters from the composition root. Redis is optional checkpoint/cache storage; Elasticsearch stays behind a log-searcher seam; Milvus stays behind retrieval/indexing seams; CozeLoop is a telemetry adapter, not a direct business dependency.

## Consequences

- Local development can run with clear capability-unavailable behavior.
- Production storage choices can evolve without rewriting workflow logic.
- Architecture tests can catch SDK imports leaking into core modules.
