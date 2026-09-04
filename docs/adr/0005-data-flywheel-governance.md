# ADR 0005: Govern the Data Flywheel Before Knowledge Write-Back

## Status

Accepted

## Context

Weak evidence, gate failures, downvotes, workflow failures, degraded tools, and high-value successes can all create useful improvement signals. Automatically turning those signals into production knowledge risks polluting retrieval with wrong, stale, duplicated, or unsafe content.

## Decision

Capture improvement signals as ReviewCases first. Each case must receive a Failure Category and Resolution Path. Only missing-knowledge and confirmed stale-knowledge cases can become KnowledgeCandidates. KnowledgeCandidates must pass review, staging index, offline evaluation, canary, and publish gates before reaching production retrieval.

## Consequences

- Retrieval quality improves through governed feedback instead of direct writes.
- Tool, workflow, prompt, intent, and environment failures go to the right repair path.
- Published knowledge remains traceable to RunEvent, TraceSpan, RetrievalSnapshot, and review decisions.
