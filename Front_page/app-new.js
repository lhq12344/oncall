// OnCall AI - 前端交互逻辑
// 支持六类 Agent: Supervisor, Knowledge, Ops, RCA, Strategy, Execution

class OnCallAI {
    constructor() {
        this.API_BASE = 'http://localhost:6872/api/v1';
        this.currentAgent = 'supervisor';
        this.sessionId = this.generateSessionId();
        this.messages = [];
        this.isLoading = false;

        this.init();
    }

    init() {
        this.cacheElements();
        this.bindEvents();
        this.loadHistory();
        this.setupMarkdown();
    }

    cacheElements() {
        // 主要元素
        this.welcomeScreen = document.getElementById('welcomeScreen');
        this.chatArea = document.getElementById('chatArea');
        this.messagesContainer = document.getElementById('messagesContainer');
        this.messageInput = document.getElementById('messageInput');
        this.sendBtn = document.getElementById('sendBtn');
        this.attachBtn = document.getElementById('attachBtn');
        this.fileInput = document.getElementById('fileInput');

        // Agent 选择器
        this.agentBtns = document.querySelectorAll('.agent-btn');
        this.currentAgentBadge = document.getElementById('currentAgentBadge');

        // 侧边栏
        this.historySidebar = document.getElementById('historySidebar');
        this.historyBtn = document.getElementById('historyBtn');
        this.closeSidebarBtn = document.getElementById('closeSidebarBtn');
        this.newChatBtn = document.getElementById('newChatBtn');
        this.historyList = document.getElementById('historyList');

        // 其他
        this.loadingIndicator = document.getElementById('loadingIndicator');
        this.toast = document.getElementById('toast');
        this.charCount = document.getElementById('charCount');

        // 快速操作卡片
        this.quickActionCards = document.querySelectorAll('.quick-action-card');
    }

    bindEvents() {
        // 发送消息
        this.sendBtn.addEventListener('click', () => this.sendMessage());
        this.messageInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                this.sendMessage();
            }
        });

        // 输入框自动调整高度
        this.messageInput.addEventListener('input', () => {
            this.autoResizeTextarea();
            this.updateCharCount();
        });

        // Agent 切换
        this.agentBtns.forEach(btn => {
            btn.addEventListener('click', () => this.switchAgent(btn.dataset.agent));
        });

        // 侧边栏
        this.historyBtn.addEventListener('click', () => this.toggleSidebar());
        this.closeSidebarBtn.addEventListener('click', () => this.toggleSidebar());
        this.newChatBtn.addEventListener('click', () => this.newChat());

        // 文件上传
        this.attachBtn.addEventListener('click', () => this.fileInput.click());
        this.fileInput.addEventListener('change', (e) => this.handleFileUpload(e));

        // 快速操作
        this.quickActionCards.forEach(card => {
            card.addEventListener('click', () => {
                const prompt = card.dataset.prompt;
                this.messageInput.value = prompt;
                this.sendMessage();
            });
        });
    }

    setupMarkdown() {
        // 配置 marked
        marked.setOptions({
            highlight: function(code, lang) {
                if (lang && hljs.getLanguage(lang)) {
                    return hljs.highlight(code, { language: lang }).value;
                }
                return hljs.highlightAuto(code).value;
            },
            breaks: true,
            gfm: true
        });
    }

    // Agent 配置
    getAgentConfig(agent) {
        const configs = {
            supervisor: {
                name: '智能助手',
                icon: '🎯',
                endpoint: '/chat',
                description: '综合智能助手，协调所有 Agent'
            },
            knowledge: {
                name: '知识库',
                icon: '📚',
                endpoint: '/chat',
                description: '搜索和检索运维知识库'
            },
            ops: {
                name: '运维监控',
                icon: '⚙️',
                endpoint: '/ai_ops',
                description: '查询 K8s、Prometheus、ES 数据'
            },
            rca: {
                name: '根因分析',
                icon: '🔍',
                endpoint: '/chat',
                description: '分析故障根本原因'
            },
            strategy: {
                name: '策略优化',
                icon: '💡',
                endpoint: '/chat',
                description: '提供优化策略建议'
            },
            execution: {
                name: '执行引擎',
                icon: '⚡',
                endpoint: '/chat',
                description: '执行运维操作'
            }
        };
        return configs[agent] || configs.supervisor;
    }

    // 切换 Agent
    switchAgent(agent) {
        this.currentAgent = agent;
        const config = this.getAgentConfig(agent);

        // 更新 UI
        this.agentBtns.forEach(btn => {
            btn.classList.toggle('active', btn.dataset.agent === agent);
        });

        // 更新徽章
        this.currentAgentBadge.innerHTML = `
            <span class="badge-icon">${config.icon}</span>
            <span class="badge-text">${config.name}</span>
        `;

        this.showToast(`已切换到 ${config.name}`);
    }

    // 发送消息
    async sendMessage() {
        const message = this.messageInput.value.trim();
        if (!message || this.isLoading) return;

        // 隐藏欢迎屏幕，显示对话区
        if (this.welcomeScreen.style.display !== 'none') {
            this.welcomeScreen.style.display = 'none';
            this.chatArea.style.display = 'block';
        }

        // 添加用户消息
        this.addMessage('user', message);
        this.messageInput.value = '';
        this.autoResizeTextarea();
        this.updateCharCount();

        // 显示加载状态
        this.setLoading(true);

        try {
            const config = this.getAgentConfig(this.currentAgent);
            let response;
            let assistantEntry = null;

            // 根据不同 Agent 调用不同接口
            if (this.currentAgent === 'ops') {
                assistantEntry = this.addMessage('assistant', '## AI Ops 执行中...\n\n正在建立流式连接...', config);
                try {
                    response = await this.callAIOpsStream((state) => {
                        this.updateMessageContent(
                            assistantEntry,
                            this.formatAIOpsStreamOutput(state, true)
                        );
                    });
                } catch (streamError) {
                    console.warn('AIOps stream failed, fallback to non-streaming:', streamError);
                    response = await this.callAIOps();
                }
                this.updateMessageContent(assistantEntry, response);
            } else {
                response = await this.callChat(message);
                // 添加助手回复
                this.addMessage('assistant', response, config);
            }

        } catch (error) {
            console.error('Error:', error);
            this.addMessage('assistant', `抱歉，发生了错误：${error.message}`);
            this.showToast('请求失败，请重试', 'error');
        } finally {
            this.setLoading(false);
        }

        // 保存到历史
        this.saveToHistory();
    }

    // 调用聊天接口
    async callChat(message) {
        const response = await fetch(`${this.API_BASE}/chat`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                id: this.sessionId,
                question: message
            })
        });

        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }

        const data = await response.json();
        return data.data?.answer || data.answer || '无响应';
    }

    // 调用 AI Ops 接口
    async callAIOps() {
        const response = await fetch(`${this.API_BASE}/ai_ops`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            }
        });

        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }

        const data = await response.json();
        const result = data.data || data;

        // 格式化 AI Ops 结果
        let formatted = `## ${result.result}\n\n`;
        if (result.detail && result.detail.length > 0) {
            formatted += '### 详细信息\n\n';
            result.detail.forEach((item, index) => {
                formatted += `${index + 1}. ${item}\n`;
            });
        }

        return formatted;
    }

    // 调用 AI Ops 流式接口（SSE over fetch）
    async callAIOpsStream(onUpdate) {
        const response = await fetch(`${this.API_BASE}/ai_ops_stream`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            }
        });

        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }

        if (!response.body) {
            throw new Error('stream response body is empty');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder('utf-8');
        const streamState = {
            steps: [],
            contents: [],
            errors: [],
            done: false,
        };

        let buffer = '';
        while (true) {
            const { value, done } = await reader.read();
            if (done) {
                break;
            }

            buffer += decoder.decode(value, { stream: true });
            const events = buffer.split('\n\n');
            buffer = events.pop() || '';

            for (const eventBlock of events) {
                this.consumeSSEBlock(eventBlock, streamState);
                if (onUpdate) {
                    onUpdate(streamState);
                }
            }
        }

        if (buffer.trim()) {
            this.consumeSSEBlock(buffer, streamState);
            if (onUpdate) {
                onUpdate(streamState);
            }
        }

        return this.formatAIOpsStreamOutput(streamState, false);
    }

    consumeSSEBlock(eventBlock, streamState) {
        if (!eventBlock) return;

        const dataLines = eventBlock
            .split('\n')
            .filter(line => line.startsWith('data:'))
            .map(line => line.slice(5).trim());

        if (dataLines.length === 0) return;

        const rawData = dataLines.join('\n');
        if (!rawData) return;

        try {
            const payload = JSON.parse(rawData);
            const type = payload?.type;
            const content = payload?.content || '';

            if (type === 'step' && content) {
                streamState.steps.push(content);
                return;
            }
            if (type === 'content' && content) {
                streamState.contents.push(content);
                return;
            }
            if (type === 'error' && content) {
                streamState.errors.push(content);
                return;
            }
            if (type === 'done') {
                streamState.done = true;
                return;
            }
        } catch (_) {
            // 非 JSON 数据按纯文本处理。
        }

        streamState.contents.push(rawData);
    }

    formatAIOpsStreamOutput(state, inProgress = false) {
        const parts = [];

        if (inProgress && !state.done) {
            parts.push('## AI Ops 执行中...');
        } else {
            parts.push('## AI Ops 执行结果');
        }

        if (state.contents.length > 0) {
            parts.push(state.contents.join('\n\n'));
        } else {
            parts.push('暂无分析内容。');
        }

        if (state.steps.length > 0) {
            const stepLines = state.steps.map((step, index) => `${index + 1}. ${step}`).join('\n');
            parts.push(`### 执行步骤\n${stepLines}`);
        }

        if (state.errors.length > 0) {
            const errorLines = state.errors.map(item => `- ${item}`).join('\n');
            parts.push(`### 错误\n${errorLines}`);
        }

        return parts.join('\n\n').trim();
    }

    // 添加消息到界面
    addMessage(role, content, agentConfig = null) {
        const message = {
            role,
            content,
            timestamp: new Date(),
            agent: agentConfig
        };

        this.messages.push(message);

        const messageEl = document.createElement('div');
        messageEl.className = `message ${role}`;

        const avatar = role === 'user' ? '👤' : (agentConfig?.icon || '🤖');
        const roleName = role === 'user' ? '你' : (agentConfig?.name || 'AI 助手');

        const timeStr = message.timestamp.toLocaleTimeString('zh-CN', {
            hour: '2-digit',
            minute: '2-digit'
        });

        messageEl.innerHTML = `
            <div class="message-avatar">${avatar}</div>
            <div class="message-content">
                <div class="message-header">
                    <span class="message-role">${roleName}</span>
                    <span class="message-time">${timeStr}</span>
                    ${agentConfig ? `
                        <span class="message-agent-badge">
                            <span>${agentConfig.icon}</span>
                            <span>${agentConfig.name}</span>
                        </span>
                    ` : ''}
                </div>
                <div class="message-body">
                    ${role === 'user' ? this.escapeHtml(content) : marked.parse(content)}
                </div>
            </div>
        `;

        this.messagesContainer.appendChild(messageEl);
        this.scrollToBottom();

        return { message, messageEl };
    }

    updateMessageContent(entry, content) {
        if (!entry || !entry.messageEl) return;

        const bodyEl = entry.messageEl.querySelector('.message-body');
        if (!bodyEl) return;

        bodyEl.innerHTML = marked.parse(content || '');
        if (entry.message) {
            entry.message.content = content || '';
        }
        this.scrollToBottom();
    }

    // 工具函数
    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    autoResizeTextarea() {
        this.messageInput.style.height = 'auto';
        this.messageInput.style.height = this.messageInput.scrollHeight + 'px';
    }

    updateCharCount() {
        const count = this.messageInput.value.length;
        this.charCount.textContent = `${count} / 2000`;
    }

    scrollToBottom() {
        setTimeout(() => {
            this.messagesContainer.scrollTop = this.messagesContainer.scrollHeight;
        }, 100);
    }

    setLoading(loading) {
        this.isLoading = loading;
        this.sendBtn.disabled = loading;
        this.loadingIndicator.classList.toggle('show', loading);
    }

    showToast(message, type = 'info') {
        this.toast.textContent = message;
        this.toast.classList.add('show');

        setTimeout(() => {
            this.toast.classList.remove('show');
        }, 3000);
    }

    toggleSidebar() {
        this.historySidebar.classList.toggle('open');
    }

    newChat() {
        this.sessionId = this.generateSessionId();
        this.messages = [];
        this.messagesContainer.innerHTML = '';
        this.chatArea.style.display = 'none';
        this.welcomeScreen.style.display = 'block';
        this.toggleSidebar();
        this.showToast('已创建新对话');
    }

    generateSessionId() {
        return `session-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
    }

    // 历史记录管理
    saveToHistory() {
        const history = this.loadHistoryFromStorage();
        const session = {
            id: this.sessionId,
            title: this.messages[0]?.content.substring(0, 50) || '新对话',
            timestamp: new Date().toISOString(),
            messageCount: this.messages.length
        };

        history.unshift(session);
        localStorage.setItem('oncall_history', JSON.stringify(history.slice(0, 50)));
    }

    loadHistory() {
        const history = this.loadHistoryFromStorage();
        this.historyList.innerHTML = '';

        history.forEach(session => {
            const item = document.createElement('div');
            item.className = 'history-item';
            item.innerHTML = `
                <div class="history-item-title">${session.title}</div>
                <div class="history-item-time">${new Date(session.timestamp).toLocaleString('zh-CN')}</div>
            `;
            this.historyList.appendChild(item);
        });
    }

    loadHistoryFromStorage() {
        try {
            return JSON.parse(localStorage.getItem('oncall_history') || '[]');
        } catch {
            return [];
        }
    }

    // 文件上传
    async handleFileUpload(event) {
        const file = event.target.files[0];
        if (!file) return;

        const formData = new FormData();
        formData.append('file', file);

        this.setLoading(true);

        try {
            const response = await fetch(`${this.API_BASE}/upload`, {
                method: 'POST',
                body: formData
            });

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
            }

            const data = await response.json();
            this.showToast('文件上传成功');

            // 添加系统消息
            this.addMessage('assistant', `文件 "${file.name}" 已上传到知识库`);

        } catch (error) {
            console.error('Upload error:', error);
            this.showToast('文件上传失败', 'error');
        } finally {
            this.setLoading(false);
            event.target.value = '';
        }
    }
}

// 初始化应用
document.addEventListener('DOMContentLoaded', () => {
    window.oncallAI = new OnCallAI();
});
