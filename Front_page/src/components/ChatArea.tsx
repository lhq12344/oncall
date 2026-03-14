import React, { useEffect, useMemo, useRef } from 'react';
import { useStore } from '../store/useStore';
import { Message } from '../types';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { InterruptCard } from './InterruptCard';
import {
  Terminal,
  User,
  Cpu,
  CheckCircle2,
  Loader2,
  AlertCircle,
  Info,
  Sparkles,
  ArrowRight,
  ChevronRight,
  Activity,
  Shield
} from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { motion, AnimatePresence } from 'motion/react';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

type SectionKind = 'observation' | 'action' | 'diagnosis';

interface ParsedSection {
  kind: SectionKind;
  title: string;
  content: string;
}

interface ParsedMessageLayout {
  contextLine?: string;
  intro: string;
  body: string;
  sections: ParsedSection[];
}

const sectionStyles: Record<
  SectionKind,
  {
    icon: typeof Activity;
    badge: string;
    border: string;
    bg: string;
    iconColor: string;
  }
> = {
  observation: {
    icon: Activity,
    badge: 'Observation',
    border: 'border-cyan-400/30',
    bg: 'bg-cyan-400/5',
    iconColor: 'text-cyan-300'
  },
  action: {
    icon: Terminal,
    badge: 'Action Taken',
    border: 'border-amber-400/30',
    bg: 'bg-amber-400/5',
    iconColor: 'text-amber-300'
  },
  diagnosis: {
    icon: Sparkles,
    badge: 'Diagnosis',
    border: 'border-fuchsia-400/30',
    bg: 'bg-fuchsia-400/5',
    iconColor: 'text-fuchsia-300'
  }
};

// detectSectionKind 识别结构化标题对应的报告分区。
// 输入：Markdown 标题文本。
// 输出：分区类型；若不匹配则返回 undefined。
function detectSectionKind(title: string): SectionKind | undefined {
  const normalized = title.trim().toLowerCase();
  if (
    normalized.includes('观测结果') ||
    normalized.includes('observation') ||
    normalized.includes('现状')
  ) {
    return 'observation';
  }
  if (
    normalized.includes('执行操作') ||
    normalized.includes('action taken') ||
    normalized.includes('执行动作') ||
    normalized.includes('已执行操作')
  ) {
    return 'action';
  }
  if (
    normalized.includes('诊断建议') ||
    normalized.includes('diagnosis') ||
    normalized.includes('suggestion') ||
    normalized.includes('建议')
  ) {
    return 'diagnosis';
  }
  return undefined;
}

// parseMessageLayout 解析消息中的 Context 行与结构化 Markdown 分区。
// 输入：格式化后的 Markdown 文本。
// 输出：上下文、正文与分区卡片数据。
function parseMessageLayout(content: string): ParsedMessageLayout {
  const normalized = content.replace(/\r\n/g, '\n');
  const lines = normalized.split('\n');
  while (lines.length > 0 && !lines[0].trim()) {
    lines.shift();
  }

  let contextLine: string | undefined;
  if (lines.length > 0 && /^(context|上下文)\s*:/i.test(lines[0].trim())) {
    contextLine = lines.shift()?.trim();
    while (lines.length > 0 && !lines[0].trim()) {
      lines.shift();
    }
  }

  const introLines: string[] = [];
  const sections: ParsedSection[] = [];
  let currentSection: ParsedSection | undefined;

  for (const line of lines) {
    const matched = line.match(/^###\s+(.+)$/);
    if (matched) {
      const title = matched[1].trim();
      const kind = detectSectionKind(title);
      if (kind) {
        currentSection = { kind, title, content: '' };
        sections.push(currentSection);
        continue;
      }
    }

    if (currentSection) {
      currentSection.content = currentSection.content
        ? `${currentSection.content}\n${line}`
        : line;
      continue;
    }

    introLines.push(line);
  }

  const intro = introLines.join('\n').trim();
  const body = lines.join('\n').trim();
  return {
    contextLine,
    intro,
    body,
    sections: sections.filter((section) => section.content.trim())
  };
}

const StatusBadge: React.FC<{ status: string }> = ({ status }) => {
  const lowerStatus = status.toLowerCase();

  const config = useMemo(() => {
    if (['running', 'ready', 'online', 'success', 'ok', 'completed'].includes(lowerStatus)) {
      return {
        color: 'text-green-400',
        bg: 'bg-green-500/10',
        border: 'border-green-500/20',
        glow: 'shadow-[0_0_8px_rgba(34,197,94,0.3)]'
      };
    }
    if (['error', 'failed', 'offline', 'critical', 'down'].includes(lowerStatus)) {
      return {
        color: 'text-red-400',
        bg: 'bg-red-500/10',
        border: 'border-red-500/20',
        glow: 'shadow-[0_0_8px_rgba(239,68,68,0.3)]'
      };
    }
    if (['warning', 'pending', 'waiting', 'busy'].includes(lowerStatus)) {
      return {
        color: 'text-orange-400',
        bg: 'bg-orange-500/10',
        border: 'border-orange-500/20',
        glow: 'shadow-[0_0_8px_rgba(249,115,22,0.3)]'
      };
    }
    return {
      color: 'text-cyber-neon',
      bg: 'bg-cyber-neon/10',
      border: 'border-cyber-neon/20',
      glow: 'shadow-[0_0_8px_rgba(0,243,255,0.3)]'
    };
  }, [lowerStatus]);

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[10px] font-bold uppercase tracking-widest border animate-pulse',
        config.color,
        config.bg,
        config.border,
        config.glow
      )}
    >
      <span className="w-1 h-1 rounded-full bg-current" />
      {status}
    </span>
  );
};

const markdownComponents = {
  h2: ({ children }: any) => (
    <h2 className="text-lg font-bold mt-6 mb-3 text-white/90 tracking-tight flex items-center gap-2">
      <ChevronRight className="w-4 h-4 text-cyber-neon" />
      {children}
    </h2>
  ),
  h3: ({ children }: any) => (
    <h3 className="text-md font-bold mt-4 mb-2 text-white/80 tracking-tight">
      {children}
    </h3>
  ),
  p: ({ children }: any) => (
    <p className="mb-4 last:mb-0 leading-relaxed opacity-90">
      {children}
    </p>
  ),
  li: ({ children }: any) => (
    <li className="flex items-start gap-3 my-2 group">
      <span className="mt-1.5 shrink-0">
        <ArrowRight className="w-3 h-3 text-cyber-neon/40 group-hover:text-cyber-neon transition-colors" />
      </span>
      <span className="opacity-90">{children}</span>
    </li>
  ),
  ul: ({ children }: any) => (
    <ul className="my-4 space-y-1">
      {children}
    </ul>
  ),
  code: ({ inline, children }: any) => {
    if (inline) {
      return (
        <code className="px-1.5 py-0.5 rounded bg-white/10 text-cyber-neon font-mono text-[0.85em]">
          {children}
        </code>
      );
    }
    return (
      <pre className="p-4 rounded-xl bg-black/40 border border-white/5 font-mono text-xs overflow-auto max-h-72 my-4 custom-scrollbar whitespace-pre-wrap break-words">
        <code>{children}</code>
      </pre>
    );
  },
  blockquote: ({ children }: any) => (
    <div className="my-6 p-4 rounded-2xl bg-white/[0.03] border border-white/10 backdrop-blur-sm relative overflow-hidden group">
      <div className="absolute top-0 left-0 w-1 h-full bg-gradient-to-b from-cyber-neon to-cyber-purple opacity-40" />
      <div className="flex items-start gap-3">
        <Info className="w-4 h-4 text-cyber-neon mt-1 shrink-0 opacity-40 group-hover:opacity-100 transition-opacity" />
        <div className="text-sm italic opacity-80">{children}</div>
      </div>
    </div>
  ),
  strong: ({ children }: any) => {
    const text = String(children);
    const statusKeywords = [
      'running',
      'ready',
      'online',
      'error',
      'failed',
      'offline',
      'pending',
      'success',
      'ok',
      'completed'
    ];
    if (statusKeywords.includes(text.toLowerCase())) {
      return <StatusBadge status={text} />;
    }
    return <strong className="font-bold text-white/90">{children}</strong>;
  }
};

// MarkdownBlock 统一渲染 Markdown 内容，并支持流式光标。
// 输入：Markdown 文本及是否展示流式光标。
// 输出：渲染后的消息正文。
const MarkdownBlock: React.FC<{ content: string; showCursor?: boolean }> = ({
  content,
  showCursor = false
}) => (
  <div className="prose prose-sm dark:prose-invert max-w-none font-sans">
    <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents as any}>
      {content}
    </ReactMarkdown>
    {showCursor && (
      <span className="inline-block w-2 h-4 ml-1 bg-cyber-neon animate-pulse align-middle" />
    )}
  </div>
);

export const ChatArea: React.FC = () => {
  const { sessions, currentSessionId, theme } = useStore();
  const scrollRef = useRef<HTMLDivElement>(null);

  const session = sessions.find((item) => item.id === currentSessionId);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [session?.messages]);

  if (!session) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center p-8 text-center">
        <motion.div
          initial={{ scale: 0.8, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          className={cn(
            'w-20 h-20 rounded-3xl flex items-center justify-center mb-6 border-2',
            theme === 'dark' ? 'border-cyber-neon/20 bg-cyber-neon/5' : 'border-cyber-purple/20 bg-cyber-purple/5'
          )}
        >
          <Terminal className={cn('w-10 h-10', theme === 'dark' ? 'text-cyber-neon' : 'text-cyber-purple')} />
        </motion.div>
        <motion.h2
          initial={{ y: 20, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          transition={{ delay: 0.1 }}
          className="text-2xl font-display font-black mb-2 tracking-tight uppercase glitch-text"
          data-text="CyberOps AI"
        >
          欢迎使用 <span className="text-cyber-neon glow-neon">CyberOps AI</span>
        </motion.h2>
        <motion.p
          initial={{ y: 20, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          transition={{ delay: 0.2 }}
          className="max-w-md opacity-60 text-sm leading-relaxed"
        >
          您的全自动 AI 运维助手。支持流式诊断、自动化脚本执行及知识库关联。
          开始一个新会话或从左侧选择历史记录。
        </motion.p>

        <motion.div
          initial={{ y: 20, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          transition={{ delay: 0.3 }}
          className="grid grid-cols-2 gap-4 mt-12 max-w-2xl w-full"
        >
          {[
            { title: '诊断系统延迟', desc: '分析当前集群 QPS 与延迟波动', icon: Activity },
            { title: '检查磁盘空间', desc: '扫描所有节点的存储占用情况', icon: Shield },
            { title: '重启 Nginx', desc: '执行滚动重启并验证健康检查', icon: Cpu },
            { title: '分析错误日志', desc: '从 ELK 提取最近 5 分钟的异常', icon: AlertCircle }
          ].map((item, index) => (
            <div
              key={index}
              className="p-4 rounded-2xl border border-white/5 bg-white/5 hover:bg-white/10 transition-all cursor-pointer text-left group"
            >
              <div className="flex items-center gap-3 mb-1">
                <item.icon className="w-4 h-4 text-cyber-neon opacity-40 group-hover:opacity-100 transition-opacity" />
                <div className="text-xs font-bold opacity-80">{item.title}</div>
              </div>
              <div className="text-[10px] opacity-40 ml-7">{item.desc}</div>
            </div>
          ))}
        </motion.div>
      </div>
    );
  }

  return (
    <div ref={scrollRef} className="flex-1 overflow-y-auto p-6 space-y-8 scroll-smooth no-scrollbar">
      <AnimatePresence initial={false}>
        {session.messages.map((message, index) => (
          <MessageItem
            key={message.id}
            message={message}
            isLast={index === session.messages.length - 1}
          />
        ))}
      </AnimatePresence>
    </div>
  );
};

const MessageItem: React.FC<{ message: Message; isLast: boolean }> = ({ message, isLast }) => {
  const { theme, isStreaming, sendMessage, currentSessionId } = useStore();
  const isUser = message.role === 'user';
  const isSystem = message.role === 'system';
  const renderedContent = useMemo(() => formatMessageContent(message.content), [message.content]);
  const parsedLayout = useMemo(() => parseMessageLayout(renderedContent), [renderedContent]);

  const suggestions = useMemo(() => {
    if (isUser || isSystem || isStreaming || !isLast) {
      return [];
    }

    const source = parsedLayout.body || renderedContent;
    const lines = source.split('\n');
    const lastFewLines = lines.slice(-5).join('\n');
    const suggestionRegex = /(?:建议|您可以|尝试|Next Steps|需要我|是否需要)：?\s*\n*((?:[-*•]\s*.*\n?)*)/gi;
    const match = suggestionRegex.exec(lastFewLines);

    if (match && match[1]) {
      return match[1]
        .split('\n')
        .map((item) => item.replace(/^[-*•]\s*/, '').trim())
        .filter((item) => item.length > 5 && item.length < 50);
    }

    if (source.trim().endsWith('？') || source.trim().endsWith('?')) {
      const lastSentence = source.trim().split(/[。！？\n]/).pop();
      if (lastSentence && lastSentence.length > 5) {
        return [lastSentence];
      }
    }

    return [];
  }, [isLast, isStreaming, isSystem, isUser, parsedLayout.body, renderedContent]);

  if (isSystem) {
    return (
      <div className="flex justify-center">
        <div className="px-4 py-1.5 rounded-full bg-white/5 border border-white/10 text-[10px] font-mono uppercase tracking-widest opacity-50">
          {message.content}
        </div>
      </div>
    );
  }

  const contentForDefaultRender = parsedLayout.body || renderedContent;

  return (
    <motion.div
      initial={{ y: 10, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      className={cn(
        'flex gap-4 max-w-4xl mx-auto group/msg',
        isUser ? 'flex-row-reverse' : 'flex-row'
      )}
    >
      <div
        className={cn(
          'w-10 h-10 rounded-xl shrink-0 flex items-center justify-center border transition-all duration-500',
          isUser
            ? theme === 'dark'
              ? 'bg-cyber-neon/10 border-cyber-neon/30 text-cyber-neon'
              : 'bg-cyber-purple/10 border-cyber-purple/30 text-cyber-purple'
            : theme === 'dark'
              ? 'bg-white/5 border-white/10 text-white'
              : 'bg-black/5 border-black/10 text-black',
          !isUser && 'group-hover/msg:border-cyber-neon/50 group-hover/msg:shadow-[0_0_15px_rgba(0,243,255,0.1)]'
        )}
      >
        {isUser ? <User className="w-5 h-5" /> : <Cpu className="w-5 h-5" />}
      </div>

      <div className={cn('flex-1 space-y-4 min-w-0', isUser ? 'text-right' : 'text-left')}>
        <div
          className={cn(
            'relative inline-block px-6 py-4 transition-all duration-500 clip-path-corner',
            isUser
              ? theme === 'dark'
                ? 'bg-cyber-neon/10 border-r-2 border-b-2 border-cyber-neon/50'
                : 'bg-cyber-purple/10 border-r-2 border-b-2 border-cyber-purple/50'
              : theme === 'dark'
                ? 'bg-white/[0.05] backdrop-blur-md border-l-2 border-t-2 border-cyber-neon/30'
                : 'bg-black/[0.05] backdrop-blur-md border-l-2 border-t-2 border-cyber-purple/30',
            !isUser && 'pl-8'
          )}
        >
          {!isUser && (
            <div className="absolute top-4 left-0 w-[2px] h-[calc(100%-32px)] bg-gradient-to-b from-cyber-neon via-cyber-purple to-transparent opacity-40" />
          )}

          {!isUser && parsedLayout.contextLine && (
            <div className="mb-4 inline-flex items-center gap-2 rounded-full border border-cyber-neon/20 bg-cyber-neon/5 px-3 py-1 text-[10px] font-mono uppercase tracking-[0.18em] text-cyber-neon/90">
              <Terminal className="w-3 h-3" />
              {parsedLayout.contextLine}
            </div>
          )}

          {!isUser && parsedLayout.sections.length > 0 ? (
            <div className="space-y-4">
              {parsedLayout.intro && <MarkdownBlock content={parsedLayout.intro} />}
              {parsedLayout.sections.map((section, index) => {
                const style = sectionStyles[section.kind];
                const Icon = style.icon;
                return (
                  <div
                    key={`${section.kind}-${index}`}
                    className={cn(
                      'rounded-2xl border p-4 backdrop-blur-sm',
                      style.border,
                      style.bg
                    )}
                  >
                    <div className="flex items-center gap-3 mb-3">
                      <div className={cn('rounded-lg p-2 bg-black/20 border border-white/10', style.iconColor)}>
                        <Icon className="w-4 h-4" />
                      </div>
                      <div className="min-w-0">
                        <div className="text-[10px] uppercase tracking-[0.22em] text-white/40 font-mono">
                          {style.badge}
                        </div>
                        <div className="text-sm font-semibold text-white/90 truncate">
                          {section.title}
                        </div>
                      </div>
                    </div>
                    <MarkdownBlock content={section.content} />
                  </div>
                );
              })}
              {isStreaming && isLast && (
                <span className="inline-block w-2 h-4 ml-1 bg-cyber-neon animate-pulse align-middle" />
              )}
            </div>
          ) : (
            <MarkdownBlock
              content={contentForDefaultRender}
              showCursor={isStreaming && isLast && !isUser}
            />
          )}
        </div>

        {message.steps && message.steps.length > 0 && (
          <div className="space-y-3">
            <div className="flex items-center gap-2 text-[10px] font-mono uppercase tracking-[0.22em] text-white/40">
              <Terminal className="w-3 h-3 text-cyber-neon" />
              执行轨迹
            </div>
            {message.steps.map((step, index) => (
              <div
                key={`${step.step}-${index}`}
                className={cn(
                  'rounded-2xl border px-4 py-3 backdrop-blur-sm',
                  step.status === 'completed'
                    ? 'border-green-500/20 bg-green-500/5'
                    : step.status === 'error'
                      ? 'border-red-500/20 bg-red-500/5'
                      : 'border-cyber-neon/20 bg-cyber-neon/5'
                )}
              >
                <div className="flex items-start gap-3">
                  <div
                    className={cn(
                      'mt-0.5 w-7 h-7 rounded-full flex items-center justify-center border transition-all duration-500',
                      step.status === 'completed'
                        ? 'bg-green-500/20 border-green-500 text-green-400'
                        : step.status === 'error'
                          ? 'bg-red-500/20 border-red-500 text-red-400'
                          : 'bg-cyber-neon/20 border-cyber-neon text-cyber-neon'
                    )}
                  >
                    {step.status === 'completed' ? (
                      <CheckCircle2 className="w-4 h-4" />
                    ) : step.status === 'error' ? (
                      <AlertCircle className="w-4 h-4" />
                    ) : (
                      <Loader2 className="w-4 h-4 animate-spin" />
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="text-[10px] font-mono uppercase tracking-[0.18em] text-white/40">
                        Step {step.step}
                      </span>
                      <StatusBadge
                        status={
                          step.status === 'completed'
                            ? 'completed'
                            : step.status === 'error'
                              ? 'error'
                              : 'pending'
                        }
                      />
                    </div>
                    <div className="text-sm font-mono break-words text-white/80">
                      {step.content}
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}

        {message.interrupt && (
          <InterruptCard
            messageId={message.id}
            interrupt={message.interrupt}
            isOps={message.type === 'step'}
          />
        )}

        {suggestions.length > 0 && (
          <div className="flex flex-wrap gap-2 mt-4 animate-in fade-in slide-in-from-bottom-2 duration-700">
            <div className="w-full flex items-center gap-2 mb-1 opacity-40">
              <Sparkles className="w-3 h-3 text-cyber-neon" />
              <span className="text-[10px] font-bold uppercase tracking-widest">建议操作</span>
            </div>
            {suggestions.map((suggestion, index) => (
              <button
                key={index}
                onClick={() => currentSessionId && sendMessage(currentSessionId, suggestion)}
                className={cn(
                  'px-4 py-2 rounded-xl text-xs font-medium transition-all border flex items-center gap-2 group',
                  theme === 'dark'
                    ? 'bg-white/5 border-white/10 hover:border-cyber-neon/50 hover:bg-cyber-neon/5 text-gray-400 hover:text-cyber-neon'
                    : 'bg-black/5 border-black/10 hover:border-cyber-purple/50 hover:bg-cyber-purple/5 text-gray-600 hover:text-cyber-purple'
                )}
              >
                {suggestion}
                <ArrowRight className="w-3 h-3 opacity-0 -translate-x-2 group-hover:opacity-100 group-hover:translate-x-0 transition-all" />
              </button>
            ))}
          </div>
        )}
      </div>
    </motion.div>
  );
};

// formatMessageContent 将 Bash 执行结果 JSON 转换为可读 Markdown，其余内容保持原样。
// 输入：原始消息文本。
// 输出：适合 ReactMarkdown 渲染的 Markdown 文本。
function formatMessageContent(content: string): string {
  const trimmed = content.trim();
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) {
    return content;
  }

  try {
    const json = JSON.parse(trimmed);
    const looksLikeBashResult =
      typeof json?.command === 'string' &&
      typeof json?.success === 'boolean' &&
      ('executed' in json || 'output' in json || 'error' in json);
    if (!looksLikeBashResult) {
      return content;
    }

    const args = Array.isArray(json.args) ? json.args.map((arg: any) => String(arg)) : [];
    const fullCommand = [json.command, ...args].join(' ').trim();
    const executed = json.executed ? '已执行' : '未执行';
    const status = json.success ? '成功' : '失败';
    const timeout = Number.isFinite(json.timeout) ? `${json.timeout}s` : '-';
    const exitCode = Number.isFinite(json.exit_code) ? String(json.exit_code) : '-';
    const output = typeof json.output === 'string' ? json.output.trim() : '';
    const error = typeof json.error === 'string' ? json.error.trim() : '';
    const comment = typeof json.comment === 'string' ? json.comment.trim() : '';

    const lines = [
      '### Bash 执行结果',
      `- 执行状态：${executed} / ${status}`,
      `- 命令：\`${fullCommand || json.command}\``,
      `- 超时：${timeout}`,
      `- 退出码：${exitCode}`
    ];

    if (comment) {
      lines.push(`- 备注：${comment}`);
    }
    if (output) {
      lines.push('', '#### 输出', '```bash', output, '```');
    }
    if (error) {
      lines.push('', '#### 错误', '```text', error, '```');
    }
    return lines.join('\n');
  } catch (_) {
    return content;
  }
}
