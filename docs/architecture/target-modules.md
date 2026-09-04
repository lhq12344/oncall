# Target Modules

This file captures the current module seams after the architecture cutover. These seams are the source of truth for implementation, tests, adapters, and future changes.

## Runtime and Composition

- `internal/bootstrap` owns application assembly and dependency injection.
- `internal/model` owns model catalog, model profiles, capability checks, timeout, retry, and cost class selection.
- `internal/events` owns the single versioned RunEvent schema.
- `internal/telemetry` owns trace, metric, and audit recording seams, with vendor adapters kept outside core runtime logic.

## Request Control

- `internal/orchestration` owns top-level request routing, deterministic control parsing, intent, risk, and clarify decisions.
- `internal/server/http` and `internal/server/sse` own transport details only.
- Controllers consume assembled runtime modules and must not construct agents, model clients, policy engines, or checkpoint stores directly.

## Prompt and Context

- `internal/prompt` owns a single PromptAssembler and section-provider model.
- `internal/context/pipeline` owns prompt context assembly.
- `internal/session/transcript` owns immutable conversation turns.
- `internal/session/checkpoint` owns resumable runtime checkpoints.
- `internal/context/compact` owns compaction policy and budget behavior.
- `internal/context/notice` owns runtime notices from tools, MCP servers, skills, and system status.
- `internal/memory` owns long-term memory extraction and storage policy.
- `internal/artifacts` owns large evidence/output spill and references.

## Tool Runtime

- `internal/tools` owns canonical tool descriptors, exposure policy, and shared tool construction helpers.
- `internal/tools/registry` owns the only tool descriptor and factory registry.
- `internal/tools/invoker` owns the only tool invocation path.
- `internal/tools/policy` owns the only policy and approval-decision engine.
- `internal/tools/eino` adapts Eino tools to OnCall descriptors and invocations.
- `internal/tools/domains` groups typed domain tools such as Kubernetes, metrics, logs, execution, RAG, and workflow control.

## Retrieval and Knowledge

- `internal/rag/ingest` owns offline document loading, chunking, enrichment, indexing, and index versions.
- `internal/rag/retrieval` owns query planning, dense/BM25 retrieval, fusion, rerank, parent expansion, and context packing.
- `internal/rag/quality` owns evidence and answer gates.
- `cmd/ragctl` owns local RAG eval and inspection commands over gold datasets, metrics, and regression thresholds.
- `internal/evidence` owns typed evidence artifacts shared by workflow, RAG, and reports.

## Workflow and Improvement

- `internal/workflow/catalog` owns workflow definitions and versions.
- `internal/workflow/incident` owns the versioned incident state machine.
- `internal/improvement` owns ReviewCase capture, triage, priority, knowledge candidate, staging, canary, and publish workflow.

## Adapters

External SDKs must live behind `internal/adapters/*` seams: model providers, Kubernetes, Prometheus, Elasticsearch/OpenSearch, Milvus/vector stores, Redis, SQL, object storage, MCP, and CozeLoop.

## Compatibility Policy

Legacy implementation modules are not allowed after Phase 13. Compatibility behavior may exist only as a named adapter behind a target seam and must be covered by architecture tests, local verification, or live verification evidence.

- `workflow/stages` is the cutover name for reusable workflow-stage composition; `workflow/agentteams` must not exist.
- `internal/commands/slash` is the cutover slash command seam; root `internal/slash` must not exist.
- `internal/tools/policy` is the policy seam; root `internal/permissions` must not exist.
- `internal/context/compact` is the compaction seam; root `internal/compact` must not exist.
