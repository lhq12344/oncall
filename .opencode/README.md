# OpenCode 后端开发配置

## 📦 可用 Skill (内置)

| Skill | 用途 | 使用场景 |
|-------|------|----------|
| `git-master` | Git操作 | 提交、变基、blame、历史搜索 |
| `playwright` | 浏览器自动化 | 测试、爬虫、网页交互 |
| `dev-browser` | 浏览器交互 | 填表、截图、网站测试 |
| `frontend-ui-ux` | 前端UI/UX | React组件、样式、设计 |

**使用方式**:
```typescript
task(category="quick", load_skills=["git-master"], prompt="...", run_in_background=false)
```

## 🗄️ 数据库 MCP 配置

你的项目使用 MySQL，配置如下：

```json
{
  "mcpServers": {
    "mysql": {
      "command": "npx",
      "args": ["-y", "mysql-mcp-server", "-h", "localhost", "-P", "30306", "-u", "root", "-p", "123456", "-d", "orm_test"]
    }
  }
}
```

或者使用其他方案：

### 方案1: MySQL CLI (直接可用)
```bash
mysql -h localhost -P 30306 -u root -p123456 orm_test
```

### 方案2: 搭建本地 MCP 服务
创建一个简单的 Go MCP 服务连接你的 MySQL：
```bash
go install github.com/your/mysql-mcp@latest
```

### 方案3: 使用 sqlc 生成类型安全客户端
```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
```

## 🚀 快速开始

1. **Git 操作** - 使用 `git-master` skill
2. **浏览器测试** - 使用 `playwright` skill  
3. **数据库查询** - 通过 Go 代码或 CLI

## 📝 示例

```typescript
// Git操作
task(category="quick", load_skills=["git-master"], prompt="帮我检查最近的提交历史", run_in_background=false)

// 浏览器自动化
task(category="visual-engineering", load_skills=["playwright"], prompt="测试登录页面", run_in_background=false)
```
