# OnCall AI - 现代化前端界面

## 概述

全新设计的 OnCall AI 前端界面，参考 Google Gemini 的现代化设计风格，支持与后端七大 Agent 的完整交互。

## 功能特性

### 1. 七大 Agent 支持

| Agent | 图标 | 功能 | API 端点 |
|-------|------|------|---------|
| 智能助手 (Supervisor) | 🎯 | 综合智能助手，协调所有 Agent | `/api/v1/chat` |
| 知识库 (Knowledge) | 📚 | 搜索和检索运维知识库 | `/api/v1/chat` |
| 运维监控 (Ops) | ⚙️ | 查询 K8s、Prometheus、ES 数据 | `/api/v1/ai_ops` |
| 根因分析 (RCA) | 🔍 | 分析故障根本原因 | `/api/v1/chat` |
| 策略优化 (Strategy) | 💡 | 提供优化策略建议 | `/api/v1/chat` |
| 执行引擎 (Execution) | ⚡ | 执行运维操作 | `/api/v1/chat` |
| 自愈循环 (Healing) | 🔄 | 自动故障检测和修复 | `/api/v1/healing/trigger` |

### 2. 现代化 UI 设计

- **Gemini 风格**: 参考 Google Gemini 的设计语言
- **流畅动画**: 平滑的过渡和交互动画
- **响应式布局**: 适配桌面和移动设备
- **深色代码高亮**: 优雅的代码展示
- **Markdown 支持**: 完整的 Markdown 渲染

### 3. 核心功能

#### 智能对话
- 支持多轮对话
- 会话历史管理
- 实时消息流式输出
- Markdown 格式化

#### Agent 切换
- 顶部导航栏快速切换
- 实时显示当前 Agent
- 不同 Agent 使用不同图标

#### 快速操作
- 预设常用操作卡片
- 一键触发常见任务
- 自定义快捷指令

#### 文件上传
- 支持上传文档到知识库
- 支持 .txt, .md, .json, .log 格式
- 拖拽上传（计划中）

#### 历史记录
- 侧边栏展示对话历史
- 快速切换历史会话
- 本地存储持久化

## 文件结构

```
Front_page/
├── index-new.html      # 新版 HTML 主文件
├── styles-new.css      # 新版样式文件
├── app-new.js          # 新版 JavaScript 逻辑
├── index.html          # 旧版 HTML（保留）
├── styles.css          # 旧版样式（保留）
├── app.js              # 旧版 JS（保留）
└── README-NEW.md       # 本文档
```

## 使用方法

### 1. 启动后端服务

```bash
cd /home/lihaoqian/project/oncall
go run main.go
```

### 2. 访问前端

#### 方式一：直接打开（推荐）

```bash
cd Front_page
# 使用任意 HTTP 服务器
python3 -m http.server 8080
# 或使用 Node.js
npx http-server -p 8080
```

然后访问: http://localhost:8080/index-new.html

#### 方式二：使用现有启动脚本

```bash
cd Front_page
./start.sh
```

然后访问: http://localhost:3000/index-new.html

### 3. 开始使用

1. **选择 Agent**: 点击顶部导航栏的 Agent 按钮
2. **输入问题**: 在底部输入框输入您的问题
3. **发送消息**: 点击发送按钮或按 Enter 键
4. **查看回复**: AI 助手会根据选择的 Agent 给出回复

## API 接口对应关系

### 1. 聊天接口

**端点**: `POST /api/v1/chat`

**使用 Agent**: Supervisor, Knowledge, RCA, Strategy, Execution

**请求**:
```json
{
  "id": "session-xxx",
  "question": "用户问题"
}
```

**响应**:
```json
{
  "message": "OK",
  "data": {
    "answer": "AI 回复"
  }
}
```

### 2. AI Ops 接口

**端点**: `POST /api/v1/ai_ops`

**使用 Agent**: Ops

**请求**: 无需 body

**响应**:
```json
{
  "message": "OK",
  "data": {
    "result": "AI Ops multi-source aggregation completed",
    "detail": ["source=k8s ...", "source=prometheus ..."]
  }
}
```

### 3. 自愈循环接口

**端点**: `POST /api/v1/healing/trigger`

**使用 Agent**: Healing

**请求**:
```json
{
  "incident_id": "inc-xxx",
  "type": "pod_crash_loop",
  "severity": "high",
  "title": "故障标题",
  "description": "故障描述"
}
```

**响应**:
```json
{
  "message": "OK",
  "data": {
    "session_id": "session-xxx",
    "message": "Healing triggered successfully"
  }
}
```

### 4. 文件上传接口

**端点**: `POST /api/v1/upload`

**请求**: multipart/form-data

**响应**:
```json
{
  "message": "OK",
  "data": {
    "fileName": "file.txt",
    "filePath": "/path/to/file",
    "fileSize": 1024
  }
}
```

### 5. 监控接口

**端点**: `GET /api/v1/monitoring`

**响应**:
```json
{
  "message": "OK",
  "data": {
    "cache_hit_rate": 0.75,
    "cache_hits": 150,
    "cache_misses": 50,
    "circuit_breakers": []
  }
}
```

## 设计特点

### 1. 颜色系统

```css
--primary-color: #1a73e8;      /* Google Blue */
--bg-primary: #ffffff;          /* 白色背景 */
--bg-secondary: #f8f9fa;        /* 浅灰背景 */
--text-primary: #202124;        /* 深色文字 */
--text-secondary: #5f6368;      /* 次要文字 */
```

### 2. 圆角系统

```css
--radius-sm: 8px;
--radius-md: 12px;
--radius-lg: 16px;
--radius-xl: 24px;
```

### 3. 阴影系统

```css
--shadow-sm: 轻微阴影
--shadow-md: 中等阴影
--shadow-lg: 大阴影
```

### 4. 动画效果

- **淡入**: 页面加载时的淡入效果
- **滑入**: 消息添加时的滑入效果
- **悬停**: 按钮和卡片的悬停效果
- **过渡**: 平滑的状态转换

## 快捷键

| 快捷键 | 功能 |
|--------|------|
| Enter | 发送消息 |
| Shift + Enter | 换行 |
| Ctrl/Cmd + K | 新建对话 |
| Ctrl/Cmd + H | 打开历史记录 |

## 浏览器兼容性

- ✅ Chrome 90+
- ✅ Firefox 88+
- ✅ Safari 14+
- ✅ Edge 90+

## 性能优化

1. **懒加载**: 历史记录按需加载
2. **防抖**: 输入框自动调整高度使用防抖
3. **缓存**: 本地存储历史记录
4. **异步**: 所有 API 调用使用异步
5. **动画优化**: 使用 CSS transform 和 opacity

## 未来计划

### 短期（1-2周）

- [ ] 流式输出支持
- [ ] 拖拽上传文件
- [ ] 代码复制按钮
- [ ] 消息搜索功能
- [ ] 导出对话记录

### 中期（1个月）

- [ ] 可视化图表展示（ECharts）
- [ ] 依赖图可视化
- [ ] 执行计划流程图
- [ ] 根因分析时间线
- [ ] 实时监控面板

### 长期（3个月）

- [ ] 多语言支持
- [ ] 主题切换（深色模式）
- [ ] 语音输入
- [ ] 协作功能
- [ ] 移动端 App

## 故障排查

### 问题 1: 无法连接后端

**症状**: 发送消息后提示连接失败

**解决方案**:
1. 检查后端服务是否运行: `curl http://localhost:6872/api/v1/monitoring`
2. 检查 CORS 配置
3. 查看浏览器控制台错误信息

### 问题 2: Agent 切换无效

**症状**: 切换 Agent 后行为没有变化

**解决方案**:
1. 检查 `app-new.js` 中的 `getAgentConfig` 方法
2. 确认 API 端点配置正确
3. 查看网络请求是否发送到正确的端点

### 问题 3: 样式显示异常

**症状**: 页面布局错乱

**解决方案**:
1. 清除浏览器缓存
2. 确认 `styles-new.css` 正确加载
3. 检查浏览器兼容性

## 贡献指南

欢迎贡献代码和提出建议！

### 开发流程

1. Fork 项目
2. 创建特性分支
3. 提交更改
4. 推送到分支
5. 创建 Pull Request

### 代码规范

- 使用 ES6+ 语法
- 遵循 Google JavaScript Style Guide
- 添加必要的注释
- 保持代码简洁

## 许可证

MIT License

## 联系方式

- 项目地址: /home/lihaoqian/project/oncall
- 文档: docs/
- 问题反馈: GitHub Issues

---

**最后更新**: 2026-03-07
**版本**: 2.0.0
**作者**: Claude Code
