# OnCall Domain Glossary

## Request and Routing

- **Request**: raw user input plus session, tenant, authorization, and runtime metadata.
- **Input Guard**: deterministic validation that rejects unsafe or unsupported input before Agent reasoning.
- **Deterministic Control Parser**: slash, approval, resume, cancel, and workflow-control recognition that runs before semantic routing.
- **Intent**: classified user goal such as dialogue, knowledge query, evidence query, incident workflow, change workflow, workflow control, clarify, or refuse.
- **Risk**: execution safety classification that is independent from intent and must gate mutations.

## Incident Workflow

- **Incident Ingress**: normalization and scoping of an operations problem.
- **Evidence Collection**: typed retrieval from Kubernetes, metrics, logs, historical cases, and knowledge.
- **Diagnosis Gate**: deterministic quality check that decides whether evidence is sufficient to plan.
- **Plan Gate**: deterministic validation of canonical plan structure, risk, targets, rollback, and approval requirements.
- **Execution Receipt**: idempotency record proving whether a mutation step already completed.
- **Verification**: post-execution evidence check that decides success, replan, rollback, or manual handoff.

## Knowledge and RAG

- **Query Planner**: retrieval-specific query rewrite and decomposition module; it is not an intent classifier.
- **Chunk Profile**: document-type-specific slicing policy for parent-child chunks, propositions, semantic boundaries, and contextual retrieval.
- **Fusion**: rank merging across dense, BM25, metadata, and rewritten-query candidates.
- **Evidence Gate**: deterministic/evaluator check that evidence is sufficient and citeable before answer generation.
- **Answer Gate**: support check that generated answers remain grounded in accepted evidence.

## Runtime Records

- **RunEvent**: versioned event envelope for SSE, UI, and replay.
- **TraceSpan**: timing, model/tool/RAG/workflow latency, token, cost, and error observability record.
- **AuditRecord**: policy, approval, command, mutation, sensitive-operation, and receipt record.
- **Artifact**: persisted large output or evidence that is referenced by ID instead of embedded in prompts or events.

## Data Flywheel

- **Failure Category**: root-cause classification such as missing knowledge, stale knowledge, retrieval failure, tool failure, workflow failure, or environment/permission.
- **Resolution Path**: review outcome that routes a case to knowledge candidate, retrieval fix, intent dataset, prompt eval dataset, tool defect, workflow defect, environment issue, or expected closure.
- **Staging Index**: non-production knowledge index used for validation before canary or publish.
- **Canary Publish**: scoped traffic release by tenant, project, or percentage with rollback to the previous index version.
