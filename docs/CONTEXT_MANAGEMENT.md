# 历史上下文保存和召回机制

## 概述

oncall agent 使用基于 Redis 的会话管理系统，实现了智能的历史上下文保存和召回机制。系统采用 Token 预算管理，确保在 LLM 上下文窗口限制内提供最相关的历史信息。

## 核心设计

### 1. Token 预算配置

```go
type Config struct {
    MaxInputTokens         int           // 96000 - 最大输入 tokens
    ReserveOutputTokens    int           // 8192  - 预留输出 tokens
    ReserveToolsDefault    int           // 20000 - 预留工具调用 tokens
    ReserveUserTokens      int           // 4000  - 预留用户输入 tokens
    SafetyTokens           int           // 2048  - 安全缓冲 tokens
    TTL                    time.Duration // 2h    - 会话过期时间（滑动）
    KeepReasoningInContext bool          // false - 是否保留推理内容
}
```

**Token 预算计算**:
```
turnsBudget = MaxInputTokens
            - ReserveOutputTokens
            - ReserveToolsTokens
            - ReserveUserTokens
            - SafetyTokens
            - SystemTokens
```

### 2. 存储结构

**Redis Key 设计**:
- `aiagent:ctx:{session_id}:sys` - System 消息列表
- `aiagent:ctx:{session_id}:turns` - 对话轮次列表
- `aiagent:ctx:{session_id}:meta` - 元数据（token 计数等）

**数据结构**:
```go
// System 消息项
type storedSysItem struct {
    T   int             // tokens
    Msg *schema.Message // 消息内容
}

// 对话轮次
type storedTurn struct {
    T    int               // 本轮总 tokens
    TS   int64             // 最后更新时间
    Msgs []*schema.Message // 本轮消息列表（user + assistant）
}
```

### 3. 保存流程（SetMessages）

**调用时机**: 每次 Agent 响应完成后

**流程**:
1. **Token 计算**:
   - User tokens: 优先使用 DeepSeek Tokenization API 精确计算，失败则本地估算
   - Assistant tokens: 优先使用 LLM 返回的 `completionTokens`（最精确）
   - 支持基于 `promptTokens` 的校准（scale 因子）

2. **消息净化**:
   ```go
   - 移除 ResponseMeta（元数据）
   - 移除 Extra（any-map）
   - 可选移除 ReasoningContent（32k thinking）
   ```

3. **原子写入**（Lua 脚本）:
   - User 消息创建新 Turn
   - Assistant 消息追加到当前 Turn
   - 更新 `turns_tokens` 计数
   - 刷新 TTL（滑动过期）

**代码示例**:
```go
err = memory.SetMessages(
    ctx,
    userMsg,           // 用户消息
    assistantMsg,      // 助手消息
    historyMsgs,       // 本次发送的完整历史
    promptTokens,      // LLM 返回的 prompt tokens
    completionTokens,  // LLM 返回的 completion tokens
)
```

### 4. 召回流程（GetMessagesForRequest）

**调用时机**: 每次用户发送新消息前

**流程**:
1. **读取 System 消息**（永远保留）

2. **计算 Turns 预算**:
   ```go
   turnsBudget = MaxInputTokens
               - ReserveOutputTokens
               - reserveToolsTokens
               - userTokensReserve
               - SafetyTokens
               - sysTokens
   ```

3. **原子裁剪**（Lua 脚本）:
   - 按 Turn 为单位从最旧开始丢弃
   - 直到 `turns_tokens <= turnsBudget`
   - 保证原子性，避免并发问题

4. **拼装消息**:
   ```
   [System 消息] + [裁剪后的 Turns] + [当前 User 消息]
   ```

5. **刷新 TTL**（滑动过期）

**代码示例**:
```go
historyMsgs, err := mem.GetMessagesForRequest(
    ctx,
    sessionID,
    schema.UserMessage(question),
    reserveToolsTokens, // 0 表示使用默认值
)
```

## Token 计算策略

### 1. 精确计算（优先）

使用 DeepSeek Tokenization API:
```go
tokens, err := tokenizer.CountMessageTokens(ctx, msg, includeReasoning)
```

**优点**:
- 与 LLM 实际计算一致
- 支持多模态内容
- 支持 ToolCalls

### 2. 本地估算（回退）

基于字符统计的估算:
```go
- ASCII 字符: 0.30 token/char
- CJK 汉字:   0.60 token/char
- 其他 Unicode: 1.00 token/char
- 结构开销:   8 tokens/message
```

**使用场景**:
- Tokenization API 不可用
- 网络超时
- 快速估算

### 3. 混合策略

```go
// User tokens: 估算 + 校准
userEst := estimateMessageTokens(userMsg)
if promptTokens > 0 {
    scale = promptTokens / estimate(promptMsgs)
    userTok = userEst * scale  // 校准
}

// Assistant tokens: 优先使用 LLM 返回值
assistantTok = completionTokens  // 最精确
if assistantTok <= 0 {
    assistantTok = estimateMessageTokens(assistantMsg)  // 回退
}
```

## 裁剪策略

### 1. 按 Turn 裁剪

**优点**:
- 保持对话完整性（user + assistant 成对）
- 避免破坏上下文连贯性
- 实现简单，性能好

**实现**:
```lua
-- Lua 脚本原子操作
while turnsTokens > budget do
    oldTurn = LPOP(turns)  -- 移除最旧的 Turn
    turnsTokens = turnsTokens - oldTurn.t
end
```

### 2. System 消息永不裁剪

System 消息包含：
- Agent 角色定义
- 工具使用说明
- 行为规范

这些信息对 Agent 正常工作至关重要，因此永远保留。

### 3. 滑动窗口

- 最新的对话优先保留
- 最旧的对话优先丢弃
- 自动适应 Token 预算

## 会话管理

### 1. Session ID

- 默认: `"default-session"`
- 用户可指定自定义 session_id
- 不同 session 完全隔离

### 2. TTL（滑动过期）

- 默认 2 小时
- 每次读写都会刷新 TTL
- 长时间不活跃自动过期

### 3. 并发安全

- 使用 Lua 脚本保证原子性
- Redis 单线程模型保证一致性
- 支持多实例并发访问

## 实际使用示例

### Controller 中的使用

```go
func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (*v1.ChatRes, error) {
    // 1. 获取历史消息
    historyMsgs, err := mem.GetMessagesForRequest(
        ctx,
        req.Id,
        schema.UserMessage(req.Question),
        0,  // 使用默认 tools tokens
    )

    // 2. 构建输入（历史 + 当前问题）
    input := buildInput(historyMsgs, req.Question)

    // 3. 调用 Agent
    output := agent.Run(ctx, input)

    // 4. 保存对话历史
    err = mem.SetMessages(
        ctx,
        req.Id,
        schema.UserMessage(req.Question),
        schema.AssistantMessage(answer),
        historyMsgs,
        promptTokens,
        completionTokens,
    )

    return &v1.ChatRes{Answer: answer}, nil
}
```

## 监控和调试

### 1. 元数据

Redis Hash `aiagent:ctx:{session_id}:meta` 存储:
```
sys_tokens: 系统消息 tokens
turns_tokens: 对话轮次 tokens
last_prompt_tokens: 最后一次 prompt tokens
last_completion_tokens: 最后一次 completion tokens
updated_at: 最后更新时间
```

### 2. 查看会话状态

```bash
# 查看元数据
redis-cli HGETALL aiagent:ctx:default-session:meta

# 查看 turns 数量
redis-cli LLEN aiagent:ctx:default-session:turns

# 查看 TTL
redis-cli TTL aiagent:ctx:default-session:turns
```

## 优化建议

### 1. Token 预算调整

根据实际使用场景调整：
- 工具调用多：增加 `ReserveToolsDefault`
- 长对话：增加 `MaxInputTokens`
- 短对话：减少 `ReserveUserTokens`

### 2. Reasoning Content

DeepSeek 的 thinking 可能占用大量 tokens（最多 32k）：
- 默认不保存到历史（`KeepReasoningInContext: false`）
- 如需保留推理过程，设置为 `true`

### 3. 性能优化

- Lua 脚本保证原子性，避免 race condition
- Redis Pipeline 批量操作
- 本地估算作为快速回退

## 总结

oncall agent 的历史上下文管理系统具有以下特点：

✅ **智能裁剪**: 基于 Token 预算自动裁剪，保持最相关的历史
✅ **精确计算**: 优先使用 DeepSeek Tokenization API
✅ **原子操作**: Lua 脚本保证并发安全
✅ **滑动窗口**: 最新对话优先保留
✅ **会话隔离**: 支持多会话并发
✅ **自动过期**: TTL 滑动刷新，长时间不活跃自动清理

这套机制确保了在 96k 输入窗口限制下，Agent 能够访问最相关的历史上下文，同时保持系统的稳定性和性能。
