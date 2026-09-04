# 15 permissions / RuleEngine / deferred gateway 深挖：工具执行为什么必须先过安全边界

> 本节回答第二轮问题：ToolSearch 和 InvokeDeferredTool 为什么能减少工具滥用？`permissions.Checker` 的 allow / ask / deny 如何由 mode、rule、command/path 共同决定？

## 1. 本节结论

OnCall 的工具安全不是单点判断，而是五层叠加：prompt 只说明“应该怎么调用”；`deferred gateway` 限制业务工具默认不可直接暴露；`ToolSearch` 建立当前 session 的发现记录；`InvokeDeferredTool` 再检查目标工具是否被发现、参数是否是 object；最后 `permissions.Checker` 结合 mode、safe command、危险命令、路径沙箱、规则和 session allow 决定 allow / ask / deny。真正阻断危险调用的是 gateway + checker，不是 prompt 文本。

## 2. 第一层：业务工具默认 deferred，只暴露两个网关

`NewDeferredGatewayRegistryWithHooks` 的注释明确说它只暴露 `ToolSearch` 和 `InvokeDeferredTool`，用于 domain agents 按需选择业务工具，而不直接拿到通用文件编辑/写入能力。实现上它新建空 registry，然后调用 `registerDeferredAndGateway`；后者把传入的 Eino business tools 包装成 deferred tool 注册，再注册 `ToolSearchTool` 与 `InvokeDeferredTool`。证据在 `backend/internal/toolkit/adapter.go:99-123`。

`BuildDeferredGatewayEinoToolsWithHooks` 只把 registry 的 always tools 适配成 Eino tool 返回；因为业务工具被 `RegisterDeferred` 标记为 deferred，所以 agent 默认看到的是网关，而不是所有业务工具。证据在 `backend/internal/toolkit/adapter.go:139-151`。

```mermaid
flowchart LR
  Agent[Agent prompt says use tools] --> Visible[Visible tools only\nToolSearch + InvokeDeferredTool]
  Visible --> Search[ToolSearch(query)]
  Search --> Mark[Registry.MarkDiscovered\nper session]
  Mark --> Invoke[InvokeDeferredTool(tool_name,args)]
  Invoke --> Check[permissions.Checker.Check]
  Check -->|allow| Tool[Execute target deferred tool]
  Check -->|ask| Interrupt[ADK Interrupt\nwait resume approval]
  Check -->|deny| Block[permission denied]
```

图源文件：`docs/learning/diagrams/16-permissions-rule-engine-flow.mmd`

## 3. 第二层：ToolSearch 是 discovery gate，不只是搜索 UX

`ToolSearchTool.Execute` 支持两种查询：`select:ToolName` 精确选择，或关键词搜索。无论哪种，只要返回 schema，就会对每个 schema 的 name 调 `Registry.MarkDiscovered(ctx, name)`。证据在 `backend/internal/toolkit/gateway.go:43-66`。

`MarkDiscovered` 不是全局开关：它优先写入 ADK session value，如果没有 session value，才用 `ContextWithDeferredDiscoverySession` 提供的 scope 写入 registry 内部 map；`IsDiscovered` 也按同样 scope 检查，避免一个会话发现过的工具泄漏给另一个会话。证据在 `backend/internal/toolkit/types.go:89-135`。

所以 `ToolSearch + InvokeDeferredTool` 降低滥用的关键点是：模型不能跳过搜索直接调用业务工具。`InvokeDeferredTool.Execute` 会先确认目标存在且是 deferred，然后检查 `Registry.IsDiscovered(ctx, toolName)`；未发现会直接返回错误，提示先用 ToolSearch。证据在 `backend/internal/toolkit/gateway.go:84-104`。

## 4. 第三层：EinoAdapter 和 InvokeDeferredTool 都会过同一个 Checker

通用 `EinoAdapter.InvokableRun` 会反序列化 arguments，跑 pre-hook，然后调用 `checker.Check(a.Tool.Name(), args)`。只要不是 allow，就触发审批 requested hook，并进入 `permissionDecisionResult`；审批未完成或被拒绝都会以 tool result 返回，不执行真实工具。证据在 `backend/internal/toolkit/adapter.go:52-72`。

`InvokeDeferredTool.Execute` 对 target tool 也走同样逻辑：发现检查后，它对目标工具名和目标参数调用 checker；未 allow 则走同一套审批中断/恢复逻辑，通过后才 `target.Execute(ctx, targetArgs)`。证据在 `backend/internal/toolkit/gateway.go:104-118`。

这说明权限判断绑定的是“真实目标工具 + 真实目标参数”，不是绑定在网关自己的 `tool_name` 字符串上。否则所有 deferred 调用都会退化成检查 `InvokeDeferredTool`，无法区分 `knowledge_retrieve` 与 `execute_step`。

## 5. Checker.Check：按层级短路，不是简单 mode matrix

`DecisionEffect` 只有 `allow / deny / ask` 三种；`PermissionMode` 有 default、acceptEdits、plan、bypassPermissions；工具分类有 read、write、command。mode matrix 的默认策略是：default 允许 read、write/command 要 ask；acceptEdits 允许 read/write、command 要 ask；bypass 三类都 allow；plan 模式特殊，所有分类默认 ask。证据在 `backend/internal/permissions/permissions.go:12-60`。

但 `Checker.Check` 不是只查 mode matrix，它的短路顺序更重要：

1. `ModePlan` 写 plan file 可以直接 allow。
2. command 如果被 `IsSafeCommand` 判定为只读安全命令，直接 allow。
3. `DetectDangerous` 命中危险模式，直接 deny。
4. read/write 路径先过 `PathSandbox.Check`，受保护路径直接 deny，越界路径默认 ask，bypass 才 allow。
5. sandbox-enabled command 会按 compound command 子片段套 rule engine，不命中时返回 OS sandbox allow。
6. 再查 RuleEngine 对当前 tool/content 的规则。
7. 再查 session allow-always。
8. 最后才落到 `ModeDecide`。

源码证据在 `backend/internal/permissions/permissions.go:532-587`。

## 6. safe command、dangerous command、path sandbox 的分工

`IsSafeCommand` 先拒绝空命令和带 shell metachar 的命令，再把空白归一化后匹配安全前缀；安全前缀包括 `go test`、只读 git、只读 kubectl、只读 docker、systemctl status/show 等。证据在 `backend/internal/permissions/permissions.go:356-387`。这解释了为什么 `kubectl get pods` 可以不弹审批，而 `kubectl delete pod` 不能自动放行。

`DetectDangerous` 是更硬的 deny 层，匹配 `rm -rf /`、`mkfs`、`dd of=/dev`、`curl|wget | sh`、force push、hard reset、git clean -f、系统关机重启等。证据在 `backend/internal/permissions/permissions.go:67-90`。

`PathSandbox` 默认允许 project root 与系统 temp；同时保护 `.env*`、`manifest/config/config.yaml`、`.oncall/permissions.local.yaml`、`.codex`、`.agents` 等敏感路径。越界 read/write 不一定 deny，而是 ask；受保护路径 deny。证据在 `backend/internal/permissions/permissions.go:92-150` 与 `backend/internal/permissions/permissions.go:551-560`。

## 7. allow-always 与 resume：审批结果如何回写

`permissionDecisionResult` 对 deny 直接返回 `permission denied`。如果还没有 interrupt state，它调用 `einotool.Interrupt`，把 `ToolApprovalInterruptInfo{ToolName, Args, Reason}` 交给 ADK 暂停；如果是恢复上下文，则解析 resume data。`approved=false` 返回 rejected；`allow_always/remember/dont_ask_again=true` 时调用 `checker.AllowAlways(toolName,args)`，以后同一 tool/content 可由 session rule 直接 allow。证据在 `backend/internal/toolkit/gateway.go:174-220`。

这和前端 `approved/resolved/selection_value` 是相邻但不同的层：权限审批只关心是否批准工具调用，以及是否记住；业务流程的 resolved/comment/selection_value 会由 controller 构造成 resume target payload 后交给 ADK。前端细节见第 17 节。

## 8. 测试证据：哪些行为已经被锁住

`permissions_test.go` 已覆盖四个关键性质：

| 测试 | 已证明 | 证据 |
| --- | --- | --- |
| `TestCheckerChecksGlobGrepBasePath` | Glob/Grep 指向 project root 外路径时 default mode 返回 ask | `backend/internal/permissions/permissions_test.go:172-200` |
| `TestCheckerAllowsSafeDeferredReadToolsByDefault` | `intent_analysis / knowledge_retrieve / k8s_monitor / metrics_collector` 这类读工具 default mode allow | `backend/internal/permissions/permissions_test.go:202-216` |
| `TestCheckerLayerOrder` | 只读 kubectl allow、变更 kubectl ask、危险 compound command deny | `backend/internal/permissions/permissions_test.go:218-238` |
| `TestAllowAlwaysSessionAndLocalRule` | allow-always 会写入 local rule，并让后续同一命令 allow | `backend/internal/permissions/permissions_test.go:240-261` |

## 9. 可修改边界

如果后续要改权限系统，优先补测试而不是直接改 prompt：

- 改 `safeCommandPrefixes`：补 allow/ask/deny 三类 command regression，尤其带 shell metachar 的情况。
- 改 `PathSandbox`：补 project root、temp、protected path、outside path 四类路径测试。
- 改 deferred gateway：补 “未 ToolSearch 直接 Invoke 报错”“不同 session discovery 不串联”“target checker 使用真实 toolName” 三类测试。
- 改 approve/resume：补 allow-always key、reject、缺 resume data 的 interrupt 行为。

## 10. Evidence / Inference / Unknown

- **Evidence**：deferred registry 只暴露 ToolSearch/InvokeDeferredTool，目标工具必须先被 MarkDiscovered，Invoke 还会再次 checker.Check 目标工具参数。证据在 `backend/internal/toolkit/adapter.go:99-151`、`backend/internal/toolkit/gateway.go:84-118`、`backend/internal/toolkit/types.go:89-135`。
- **Evidence**：Checker 的短路顺序是 plan-file allow、安全命令 allow、危险命令 deny、路径沙箱、rule engine、session allow、mode matrix。证据在 `backend/internal/permissions/permissions.go:532-587`。
- **Inference**：prompt 中“不要执行变更”只能降低模型主动误用概率；真正能阻止执行的是 gateway/checker/interrupt/resume 链。
- **Unknown**：当前文档未做真实浏览器 approval 流程 e2e；第 17 节只以源码和构建验证确认 payload 形状。

## 11. 阅读检查清单

读完本节后，应该能回答：

- 为什么业务工具不能绕过 ToolSearch？
- 为什么检查的是 target tool，而不是 InvokeDeferredTool 自己？
- default / acceptEdits / bypassPermissions 的默认差异是什么？
- 哪些命令会 allow、哪些会 ask、哪些直接 deny？
- allow-always 写入后，对后续同一 session/规则有什么影响？
