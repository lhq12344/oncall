# AGENTS.md

适用范围：`internal/controller` 及其子目录。

## 控制器层规则

- 控制器只负责：参数校验、会话上下文构建、Agent 调用、SSE 输出、结果落库。
- 不在控制器内实现复杂业务逻辑（业务逻辑放到 agent/tool/context 组件）。
- 接口行为改动必须保持向后兼容；若不兼容，必须在回复中明确说明。

## 流式与中断

- 流式接口优先，输出格式保持稳定（`content` / `step` / `interrupt` / `error` / `done`）。
- 任何 `interrupt` 相关改动必须同时检查 `resume` 路径。
- `checkpoint_id`、`interrupt_ids` 语义不得随意变更。

## 质量要求

- 修改控制器后，至少验证编译通过并运行 `go test ./...`。
- 避免在控制器写重复代码，优先抽公共函数。
