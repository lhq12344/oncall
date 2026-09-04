# ADR 0002: Use Workflow for Control and Agents for Judgment

## Status

Accepted

## Context

Incident response includes deterministic safety requirements and semantic reasoning. Approval, execution, verification, retry, rollback, and resume must be auditable and repeatable. Diagnosis, plan generation, evidence summarization, and report writing benefit from Agent judgment.

## Decision

Make Workflow the control spine and use Agents as bounded judgment nodes or tools inside that spine. Agents may propose, summarize, classify, or diagnose, but they do not bypass Workflow state transitions, approval snapshots, mutation receipts, or policy checks.

## Consequences

- Mutation safety is owned by deterministic code.
- Agent quality can improve without changing approval semantics.
- Tests can characterize workflow terminal states independently from model wording.
