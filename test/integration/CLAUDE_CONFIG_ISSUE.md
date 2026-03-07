# Claude 模型配置说明

## 当前状态

❌ **配置失败**: 403 Forbidden - Request not allowed

## 问题分析

### 错误信息
```
status code: 403, status: 403 Forbidden, message: Request not allowed
```

### 可能原因

1. **API 兼容性问题** ⚠️
   - Anthropic Claude API 不是 OpenAI 兼容的
   - 当前代码使用 OpenAI 客户端
   - 需要专门的 Claude/Anthropic 客户端

2. **API Key 问题**
   - Key 可能无效或已过期
   - Key 可能没有权限访问指定模型

3. **端点格式问题**
   - Anthropic API 端点: `https://api.anthropic.com/v1/messages`
   - 不是 OpenAI 格式的 `/chat/completions`

## 当前配置

```yaml
ds_think_chat_model:
  api_key: "sk-ImoWDGMu3iS7V3cpr9C3eOZEOWZsqJ6aTkAMiSAEvgfvOmqM"
  base_url: "https://api.anthropic.com/v1"
  model: "claude-opus-4-20250514"

ds_quick_chat_model:
  api_key: "sk-ImoWDGMu3iS7V3cpr9C3eOZEOWZsqJ6aTkAMiSAEvgfvOmqM"
  base_url: "https://api.anthropic.com/v1"
  model: "claude-sonnet-4-20250514"
```

## 代码兼容性检查

### 当前实现
项目使用的是 OpenAI 兼容的客户端：
```go
// internal/ai/models/open_ai.go
func OpenAIForDeepSeekV3Quick(ctx context.Context) (cm model.ToolCallingChatModel, err error) {
    // 使用 OpenAI 格式的配置
    config := &openai.ChatModelConfig{
        Model:   model.String(),
        APIKey:  api_key.String(),
        BaseURL: base_url.String(),
    }
    cm, err = openai.NewChatModel(ctx, config)
    // ...
}
```

### 需要的改动

要支持 Claude，需要：

1. **添加 Anthropic 客户端支持**
   ```go
   // 需要添加新的文件: internal/ai/models/anthropic.go
   func AnthropicForClaude(ctx context.Context) (cm model.ToolCallingChatModel, err error) {
       // 使用 Anthropic SDK
   }
   ```

2. **修改配置读取逻辑**
   ```go
   // 根据 base_url 判断使用哪个客户端
   if strings.Contains(base_url, "anthropic.com") {
       return AnthropicForClaude(ctx)
   } else {
       return OpenAIForDeepSeekV3Quick(ctx)
   }
   ```

3. **安装 Anthropic SDK**
   ```bash
   go get github.com/anthropics/anthropic-sdk-go
   ```

## 解决方案

### 方案 1: 使用 OpenAI 兼容的 Claude 代理（推荐）

某些服务提供 OpenAI 兼容的 Claude API：

```yaml
ds_quick_chat_model:
  api_key: "your-key"
  base_url: "https://openrouter.ai/api/v1"  # 或其他兼容服务
  model: "anthropic/claude-sonnet-4"
```

### 方案 2: 继续使用 DeepSeek（推荐）

DeepSeek V3.2 已经验证可用，性能优秀：

```yaml
ds_think_chat_model:
  api_key: "c3b9d277-0f82-4ef0-970a-1f3b5607c861"
  base_url: "https://ark.cn-beijing.volces.com/api/v3"
  model: "deepseek-v3-2-251201"

ds_quick_chat_model:
  api_key: "c3b9d277-0f82-4ef0-970a-1f3b5607c861"
  base_url: "https://ark.cn-beijing.volces.com/api/v3"
  model: "deepseek-v3-2-251201"
```

**优势**:
- ✅ 已验证可用
- ✅ 性能优秀
- ✅ 成本更低
- ✅ 无需代码修改

### 方案 3: 实现 Anthropic 客户端支持

需要开发工作：

1. 添加 Anthropic SDK 依赖
2. 实现 Anthropic 客户端适配器
3. 修改模型初始化逻辑
4. 测试验证

**工作量**: 中等（2-4 小时）

## Claude 模型信息

### 可用模型
- `claude-opus-4-20250514` - 最强大，最慢，最贵
- `claude-sonnet-4-20250514` - 平衡性能和成本
- `claude-haiku-4-20250514` - 最快，最便宜

### API 格式差异

**OpenAI 格式**:
```json
POST /v1/chat/completions
{
  "model": "gpt-4",
  "messages": [{"role": "user", "content": "Hello"}]
}
```

**Anthropic 格式**:
```json
POST /v1/messages
{
  "model": "claude-opus-4-20250514",
  "max_tokens": 1024,
  "messages": [{"role": "user", "content": "Hello"}]
}
Headers: {
  "x-api-key": "your-key",
  "anthropic-version": "2023-06-01"
}
```

## 建议

### 短期（立即可用）
**继续使用 DeepSeek V3.2** ✅
- 已验证可用
- 性能优秀
- 成本效益高
- 无需修改代码

### 中期（如需 Claude）
**使用 OpenAI 兼容的 Claude 代理**
- OpenRouter
- 其他兼容服务

### 长期（完整支持）
**实现原生 Anthropic 客户端**
- 完整的 Claude 功能支持
- 更好的性能
- 直接访问 Anthropic API

## 下一步

请选择：

1. **恢复 DeepSeek 配置**（推荐）
   ```bash
   # 我可以立即恢复配置
   ```

2. **尝试 OpenAI 兼容代理**
   - 需要提供兼容服务的 URL

3. **实现 Anthropic 客户端**
   - 需要开发时间

---

**当前状态**: 配置已更新但不兼容
**建议操作**: 恢复 DeepSeek 配置
