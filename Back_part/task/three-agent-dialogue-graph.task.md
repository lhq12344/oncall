# Three-Agent Dialogue Graph Tasks

Source plan: `.claude/PRPs/plans/completed/three-agent-dialogue-graph.plan.md`

Goal: split dialogue into Gate, Answer, and Complex agents, move
interrupt/resume responsibility into middleware/graph orchestration, and
integrate graph streaming into the chat service.

Status: historical/completed plan. Use this as a regression and continuation
checklist.

## Task 1: Refactor BashApprovalTool

- Remove direct first-run interrupt and resume handling from `InvokableRun`.
- Keep argument parsing, validation, allowlist checks, and execution.
- Keep interrupt info registration and decision parsing helpers for middleware.
- Direct successful execution marks result approved and executed.

## Task 2: Refactor DetailSelectionTool

- Remove direct interrupt/resume handling from `InvokableRun`.
- Keep option normalization, selection parsing, and lookup helpers.
- Return awaiting-selection payload when called directly.
- Keep interrupt info registration.

## Task 3: Add Tool Middleware

- `ApprovalMiddleware` wraps `bash_execute_with_approval` and `request_detail_selection`.
- First tool call stores args and raises `tool.StatefulInterrupt`.
- Resume path executes, rejects, resolves, or returns selected detail.
- `SafeToolMiddleware` converts non-interrupt tool errors into string tool results.
- Interrupt rerun errors from `compose.IsInterruptRerunError` pass through unchanged.

## Task 4: Export OrchState And Agent Instructions

- Export `OrchState` for service integration.
- Include inner checkpoint id, resume data, and resume interrupt ids.
- Add Gate, Answer, and Complex instruction constants.

## Task 5: Add Agent Factories

- `newGateAgent`: intent and knowledge routing.
- `newAnswerAgent`: final response for knowledge-resolved cases.
- `newComplexAgent`: complex/tool-heavy handling with middleware stack.
- Complex Agent registers optional skill middleware from `SkillsDir`.
- Use context-aware model input when analysis should be promoted to system prompt.

## Task 6: Create Dialogue Graph

- Build Gate node, router, Answer node, and Complex node.
- Convert ADK agent events into stream output messages.
- Convert tool/action interrupts into graph-level `compose.StatefulInterrupt`.
- Preserve inner checkpoint id for Complex resume.
- Keep fallback routing conservative.

## Task 7: Wire App To Dialogue Graph

- Construct dialogue graph instead of prior single agent where appropriate.
- Pass chat model, embedder, skill dir, logger, and trace recorder dependencies.
- Keep Milvus/retriever degradation non-fatal.

## Task 8: Integrate Graph In Chat Service

- Use graph stream in `ChatStream`.
- Detect interrupts with `compose.IsInterruptRerunError`.
- Normalize interrupt payloads into SSE events with checkpoint id.
- Inject resume state in `ChatResumeStream`.
- Preserve existing SSE event types.

## Task 9: Update Service Construction

- Update constructors after graph/service dependency changes.
- Ensure controller/service receive graph-enabled dependencies.
- Remove obsolete single-agent parameters.

## Regression Checklist

- Bash approvals pause before execution and resume only with approval.
- Detail selection pauses with options and resumes with valid selection.
- Tool execution errors do not abort ReAct loops unless interrupt rerun errors.
- Gate resolved path reaches Answer.
- Gate unresolved path reaches Complex.
- Complex can load skills when `EINO_EXT_SKILLS_DIR` points to the configured directory.
- SSE interrupt payload remains frontend-compatible.
