# OnCall Architecture Context

This document is the architecture source of truth for the OnCall refactor. It records the domain language that implementation modules, tests, ADRs, and migration plans must use consistently.

## Target Outcome

OnCall is an intelligent operations system built around a deterministic Workflow spine and semantic Agent nodes. Eino remains the runtime kernel for Agent execution, middleware, Graph, Workflow, interrupt, checkpoint, and resume capabilities, while OnCall owns the domain state, safety policy, event contract, and data governance.

## Core Domain Concepts

- **Run**: one user-visible execution from request intake through terminal response, refusal, approval interrupt, workflow completion, or failure.
- **Workflow**: deterministic control over routing, state transitions, approval, execution, verification, retry, rollback, and resume.
- **Agent**: a semantic judgment node that rewrites queries, diagnoses incidents, generates plans, summarizes evidence, or drafts reports without bypassing Workflow control.
- **Incident**: an operations event that needs scoped evidence, diagnosis, a validated plan, optional human approval, execution, verification, and a final report.
- **Canonical Plan**: the workflow-owned execution plan snapshot consumed by approval and execution stages.
- **Approval Snapshot**: the plan revision, snapshot hash, target resources, and tool arguments bound to a human approval decision.
- **Tool Runtime**: the single path for tool discovery, invocation, policy evaluation, approval, execution, output handling, trace, event, and audit.
- **Evidence**: typed observations from knowledge, logs, metrics, Kubernetes, historical cases, or user context that can be cited and replayed.
- **Retrieval Snapshot**: the versioned record of query variants, retrieval candidates, fusion, rerank, final evidence, degraded reasons, and latency.
- **Review Case**: a captured quality, safety, workflow, tool, or knowledge improvement opportunity that must be triaged before any knowledge write-back.
- **Knowledge Candidate**: reviewed knowledge content eligible for staging, evaluation, canary, and publish only after root-cause classification permits it.

## Architecture Vocabulary

- **Module**: anything with an interface and an implementation.
- **Interface**: everything a caller must know to use a module correctly, including ordering, invariants, error modes, configuration, and performance expectations.
- **Seam**: where a module's interface lives and behavior can vary without editing callers.
- **Adapter**: a concrete implementation at a seam for an external system or alternate storage/runtime.
- **Depth**: leverage at the interface: broad behavior behind a small, stable surface.
- **Locality**: the benefit of concentrating knowledge, change, bugs, and verification in one place.

## Current Cutover Policy

- Runtime, Prompt, Tool Runtime, Context, Session, RAG, Workflow, Data Flywheel, Team, MCP, and Frontend event handling now use the target Module seams as the source of truth.
- Compatibility layers may remain only as named adapters at a target seam, with tests proving they cannot become a second Registry, Event schema, PolicyEngine, or workflow control path.
- Phase 13 removes old implementation directories and string-protocol fallbacks after equivalent tests, replay fixtures, and verification gates are in place.
- Production mutations still require Workflow-owned planning, approval snapshots, idempotency receipts, audit records, and post-execution verification.
- Redis, Elasticsearch, Milvus, Kubernetes, Prometheus, and CozeLoop remain optional or degradable adapters for local startup; live verification proves production readiness separately.

## Phase Completion Criteria

- Each phase has a concrete implementation seam, regression tests, verification command, and code-review evidence.
- Architecture tests identify concrete file and line violations for forbidden duplicate modules, external SDK leakage, transport imports, and legacy protocol paths.
- Local verification evidence is machine-readable and fail-closed; environment-limited gates remain explicit until proven in a live or CI-capable environment.
- The final completion claim requires Go, frontend, RAG, Intent, Workflow replay, security, failure-injection, approval, Trace, and old-code deletion evidence.
