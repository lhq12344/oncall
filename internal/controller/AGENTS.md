# AGENTS.md

适用范围：`internal/controller` 及其子目录。

## OVERVIEW

控制器层是协议边界：负责参数接收、Runner 调用、SSE 事件输出和结果落库，不承担运维业务决策。

## WHERE TO LOOK

| 任务 | 位置 | 说明 |
|---|---|---|
| 聊天流式接口 | `chat/chat_v1.go::ChatStream` | 会话拼装、SSE content 输出、checkpoint 建立 |
| 中断恢复接口 | `chat/chat_v1.go::ChatResumeStream` | `Resume/ResumeWithParams` 恢复逻辑 |
| AIOps 流式接口 | `chat/chat_v1.go::AIOpsStream` | `step/content/interrupt/done` 事件输出 |
| AIOps 恢复接口 | `chat/chat_v1.go::AIOpsResumeStream` | 运维流程恢复 |
| 路由契约 | `api/chat/v1/chat.go` | path/method/字段名权威来源 |

## CONVENTIONS

- 控制器只负责：参数校验、会话上下文构建、Agent 调用、SSE 输出、结果落库。
- 不在控制器写复杂业务逻辑（业务逻辑放到 `agent/tool/context` 组件）。
- 接口行为改动必须保持向后兼容；若不兼容，必须明确说明影响面。
- SSE 事件语义保持稳定：`content` / `step` / `interrupt` / `error` / `done`。
- `interrupt` 改动必须联动检查 `resume` 路径；`checkpoint_id`、`interrupt_ids` 语义不得随意变更。
- 保持 `text/event-stream` 响应不被二次包装（见 `utility/middleware/ResponseMiddleware`）。

## ANTI-PATTERNS

- 在 controller 内做策略生成、根因推理、命令拼装。
- 只改 `ChatStream` 不改 `ChatResumeStream`（或 AIOps 对应恢复接口）。
- 修改 SSE payload 字段但不更新前端 `Front_page/src/services/api.ts` 解析。

## QUALITY GATES

- 修改控制器后，至少验证编译通过并运行：`go test ./...`。
- 涉及流式协议时，至少检查：`api/chat/v1/chat.go` 契约、`chat_v1.go` 输出、前端解析三端一致。
