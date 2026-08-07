# OnCall AgentTeams

OnCall uses AgentTeams to describe the AIOps incident workflow as a named team of specialist agents instead of wiring the full workflow directly at the ops entry point.

## Why AgentTeams

The previous incident workflow already ran multiple agents, but the orchestration was embedded directly in the ops constructor as a sequential workflow plus an execution loop. The AgentTeams layer makes that structure explicit:

- `Team` names the incident-response group.
- `Member` maps a stable team role to an ADK agent.
- `Stage` describes the order in which roles run.
- Loop stages preserve bounded re-planning for remediation execution.
- The builder compiles the declaration back into Eino ADK resumable workflows.

This keeps current runtime behavior while giving future work a clearer place to add dynamic teammates, team progress, message passing, or parallel observation.

## Current Incident Team

The incident workflow is declared in `internal/agent/ops/incident_workflow.go` and compiled through `internal/agent/agentteams`.

Current stage order:

1. `incident_observation_stage`: `observation`
2. `incident_rca_stage`: `rca`
3. `incident_execute_loop`: `ops -> execution -> gate`
4. `incident_strategy_stage`: `strategy`
5. `incident_final_report_stage`: `final_report`

The execution loop defaults to 3 iterations when no positive `MaxExecutionLoops` value is supplied.

## Compatibility Boundaries

The migration intentionally keeps these surfaces unchanged:

- HTTP and SSE API contracts.
- Frontend slash command behavior.
- Human approval interrupts and resume handling.
- Existing incident Graph State bridge.
- Existing history rewriting and compaction behavior.

The first migration does not port mewcode terminal teammates, file mailbox, tmux/iTerm spawning, transcript persistence, or SendMessage tools. Those remain follow-up extension points.

## Verification

Use these checks when changing the team declaration:

- `go test ./internal/agent/agentteams`
- `go test ./internal/agent/ops`
- `go test ./internal/controller/chat`
- `go test ./...`
