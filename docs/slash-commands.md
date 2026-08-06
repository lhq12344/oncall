# OnCall Slash Commands

OnCall supports slash commands in the chat input. A message beginning with slash is parsed by the backend before it reaches the LLM. The command layer can return local status, inject an OnCall prompt, trigger the AIOps workflow, or send a UI action to the frontend.

## Architecture

1. ChatStream receives question and calls the slash parser first.
2. Non-slash input keeps the existing SessionMemory -> chat runner -> SSE -> SaveTurn path.
3. Slash input is resolved through the registry:
   - local: returns SSE content and done without calling the LLM.
   - prompt: expands to an OnCall prompt and runs the dialogue agent.
   - ops_workflow: expands to an incident prompt and runs the AIOps workflow.
   - client_action: returns command_action for frontend-only UI changes.
4. InputArea shows up to 8 suggestions while the user types slash and has not started arguments.

## Built-in Commands

| Command | Type | Aliases | Description |
|---|---|---|---|
| /help [command] | local | /h, /? | List commands or show command details |
| /commands | local | | Show command sources and warnings |
| /status | local | /s | Show OnCall runner, agent, and observability status |
| /session | local | | Show current session summary |
| /memory [list] | local | | Show recent session memory summary |
| /review [focus] | prompt | | Review current code changes |
| /diagnose <symptom> | prompt | /diag | Diagnose an incident symptom with read-only tools |
| /ops <incident> | ops_workflow | /incident, /aiops | Run the full AI Ops incident workflow |
| /k8s [resource] [-n namespace] | prompt | /pods | Read-only Kubernetes inspection |
| /metrics <query> | prompt | /prom | Query Prometheus metrics |
| /logs [query] [time_range] | prompt | /last-error, /errors | Query recent error logs |
| /cases <query> | prompt | | Retrieve historical ops cases |
| /clear | client_action | | Clear the current frontend session messages |

## Examples

/k8s pods -n prod

Read-only Kubernetes pod inspection. The generated prompt explicitly forbids mutating kubectl operations such as delete, apply, patch, scale, or rollout restart.

/last-error payment 30m

Inspect recent payment errors in the last 30 minutes. The command prefers log query capabilities and falls back to recent session or local ops report context.

/ops checkout service 5xx

Run the complete AIOps workflow: observation, RCA, remediation proposal, plan validation, human approval interrupt when needed, and final report.

## Project Commands

Project-local commands are loaded from:

.oncall/commands/**/*.md

Mew-compatible commands are also loaded from:

.mewcode/commands/**/*.md

Example file:

---
description: Review a GitHub pull request
argument-hint: <PR_NUMBER>
aliases: [review-pr, rp]
---

Please review pull request $ARGUMENTS.

Check correctness, security, performance, and tests.

Rules:

- Subdirectories map to colon names. For example .oncall/commands/git/log.md becomes /git:log.
- $ARGUMENTS is replaced by user arguments.
- If the body has no $ARGUMENTS placeholder, arguments are appended under a User Request section.
- Project commands cannot override built-in commands or built-in aliases.

## Safety Boundaries

- /k8s, /metrics, and /logs are prompt commands in the first version and instruct the dialogue agent to use existing read-only tools.
- Mutating operations must still go through bash_execute_with_approval or the execution workflow execute_step permission path.
- /status only reports whether configuration exists. It does not print kubeconfig content, environment values, or credentials.
- Unknown commands do not fall through to the LLM. They return a deterministic error and suggest /help.
