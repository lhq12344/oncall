import React, { useEffect, useRef, useMemo } from 'react';
import { useStore } from '../store/useStore';
import { Message } from '../types';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { InterruptCard } from './InterruptCard';
import { 
  Terminal, User, Cpu, CheckCircle2, Loader2, AlertCircle, 
  Info, Sparkles, ArrowRight, ChevronRight, Activity, Shield
} from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { motion, AnimatePresence } from 'motion/react';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

const StatusBadge: React.FC<{ status: string }> = ({ status }) => {
  const lowerStatus = status.toLowerCase();
  
  const config = useMemo(() => {
    if (['running', 'ready', 'online', 'success', 'ok', 'completed'].includes(lowerStatus)) {
      return { color: 'text-green-400', bg: 'bg-green-500/10', border: 'border-green-500/20', glow: 'shadow-[0_0_8px_rgba(34,197,94,0.3)]' };
    }
    if (['error', 'failed', 'offline', 'critical', 'down'].includes(lowerStatus)) {
      return { color: 'text-red-400', bg: 'bg-red-500/10', border: 'border-red-500/20', glow: 'shadow-[0_0_8px_rgba(239,68,68,0.3)]' };
    }
    if (['warning', 'pending', 'waiting', 'busy'].includes(lowerStatus)) {
      return { color: 'text-orange-400', bg: 'bg-orange-500/10', border: 'border-orange-500/20', glow: 'shadow-[0_0_8px_rgba(249,115,22,0.3)]' };
    }
    return { color: 'text-cyber-neon', bg: 'bg-cyber-neon/10', border: 'border-cyber-neon/20', glow: 'shadow-[0_0_8px_rgba(0,243,255,0.3)]' };
  }, [lowerStatus]);

  return (
    <span className={cn(
      "inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[10px] font-bold uppercase tracking-widest border animate-pulse",
      config.color, config.bg, config.border, config.glow
    )}>
      <span className="w-1 h-1 rounded-full bg-current" />
      {status}
    </span>
  );
};

export const ChatArea: React.FC = () => {
  const { sessions, currentSessionId, theme } = useStore();
  const scrollRef = useRef<HTMLDivElement>(null);

  const session = sessions.find((s) => s.id === currentSessionId);

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
            "w-20 h-20 rounded-3xl flex items-center justify-center mb-6 border-2",
            theme === 'dark' ? "border-cyber-neon/20 bg-cyber-neon/5" : "border-cyber-purple/20 bg-cyber-purple/5"
          )}
        >
          <Terminal className={cn("w-10 h-10", theme === 'dark' ? "text-cyber-neon" : "text-cyber-purple")} />
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
            { title: '分析错误日志', desc: '从 ELK 提取最近 5 分钟的异常', icon: AlertCircle },
          ].map((item, i) => (
            <div key={i} className="p-4 rounded-2xl border border-white/5 bg-white/5 hover:bg-white/10 transition-all cursor-pointer text-left group">
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
        {session.messages.map((msg, idx) => (
          <MessageItem 
            key={msg.id} 
            message={msg} 
            isLast={idx === session.messages.length - 1} 
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

  // Extract suggestions from the end of the message
  const suggestions = useMemo(() => {
    if (isUser || isSystem || isStreaming || !isLast) return [];
    
    const lines = message.content.split('\n');
    const lastFewLines = lines.slice(-5).join('\n');
    
    // Look for patterns like "建议：", "您可以：", "需要我...吗？"
    const suggestionRegex = /(?:建议|您可以|尝试|Next Steps|需要我|是否需要)：?\s*\n*((?:[-*•]\s*.*\n?)*)/gi;
    const match = suggestionRegex.exec(lastFewLines);
    
    if (match && match[1]) {
      return match[1]
        .split('\n')
        .map(s => s.replace(/^[-*•]\s*/, '').trim())
        .filter(s => s.length > 5 && s.length < 50);
    }
    
    // Fallback: look for question marks at the end
    if (message.content.trim().endsWith('？') || message.content.trim().endsWith('?')) {
      const lastSentence = message.content.trim().split(/[。！？\n]/).pop();
      if (lastSentence && lastSentence.length > 5) return [lastSentence];
    }

    return [];
  }, [message.content, isUser, isSystem, isStreaming, isLast]);

  if (isSystem) {
    return (
      <div className="flex justify-center">
        <div className="px-4 py-1.5 rounded-full bg-white/5 border border-white/10 text-[10px] font-mono uppercase tracking-widest opacity-50">
          {message.content}
        </div>
      </div>
    );
  }

  const MarkdownComponents = {
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
      const statusKeywords = ['running', 'ready', 'online', 'error', 'failed', 'offline', 'pending', 'success', 'ok', 'completed'];
      if (statusKeywords.includes(text.toLowerCase())) {
        return <StatusBadge status={text} />;
      }
      return <strong className="font-bold text-white/90">{children}</strong>;
    }
  };

  return (
    <motion.div 
      initial={{ y: 10, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      className={cn(
        "flex gap-4 max-w-4xl mx-auto group/msg",
        isUser ? "flex-row-reverse" : "flex-row"
      )}
    >
      <div className={cn(
        "w-10 h-10 rounded-xl shrink-0 flex items-center justify-center border transition-all duration-500",
        isUser 
          ? (theme === 'dark' ? "bg-cyber-neon/10 border-cyber-neon/30 text-cyber-neon" : "bg-cyber-purple/10 border-cyber-purple/30 text-cyber-purple")
          : (theme === 'dark' ? "bg-white/5 border-white/10 text-white" : "bg-black/5 border-black/10 text-black"),
        !isUser && "group-hover/msg:border-cyber-neon/50 group-hover/msg:shadow-[0_0_15px_rgba(0,243,255,0.1)]"
      )}>
        {isUser ? <User className="w-5 h-5" /> : <Cpu className="w-5 h-5" />}
      </div>

      <div className={cn(
        "flex-1 space-y-4 min-w-0",
        isUser ? "text-right" : "text-left"
      )}>
        <div className={cn(
          "relative inline-block px-6 py-4 transition-all duration-500 clip-path-corner",
          isUser 
            ? (theme === 'dark' ? "bg-cyber-neon/10 border-r-2 border-b-2 border-cyber-neon/50" : "bg-cyber-purple/10 border-r-2 border-b-2 border-cyber-purple/50")
            : (theme === 'dark' ? "bg-white/[0.05] backdrop-blur-md border-l-2 border-t-2 border-cyber-neon/30" : "bg-black/[0.05] backdrop-blur-md border-l-2 border-t-2 border-cyber-purple/30"),
          !isUser && "pl-8"
        )}>
          {!isUser && (
            <div className="absolute top-4 left-0 w-[2px] h-[calc(100%-32px)] bg-gradient-to-b from-cyber-neon via-cyber-purple to-transparent opacity-40" />
          )}
          
          <div className="prose prose-sm dark:prose-invert max-w-none font-sans">
            <ReactMarkdown 
              remarkPlugins={[remarkGfm]}
              components={MarkdownComponents as any}
            >
              {renderedContent}
            </ReactMarkdown>
            
            {isStreaming && isLast && !isUser && (
              <span className="inline-block w-2 h-4 ml-1 bg-cyber-neon animate-pulse align-middle" />
            )}
          </div>
        </div>

        {message.steps && message.steps.length > 0 && (
          <div className="space-y-3 mt-4 pl-4">
            {message.steps.map((step, i) => (
              <div key={i} className="flex items-start gap-3 group/step">
                <div className="flex flex-col items-center gap-1 mt-1">
                  <div className={cn(
                    "w-5 h-5 rounded-full flex items-center justify-center border transition-all duration-500",
                    step.status === 'completed' ? "bg-green-500/20 border-green-500 text-green-500" :
                    step.status === 'error' ? "bg-red-500/20 border-red-500 text-red-500" :
                    "bg-cyber-neon/20 border-cyber-neon text-cyber-neon animate-spin"
                  )}>
                    {step.status === 'completed' ? <CheckCircle2 className="w-3 h-3" /> :
                     step.status === 'error' ? <AlertCircle className="w-3 h-3" /> :
                     <Loader2 className="w-3 h-3" />}
                  </div>
                  {i < message.steps!.length - 1 && (
                    <div className="w-[1px] h-4 bg-white/10" />
                  )}
                </div>
                <div className="flex-1 pt-0.5">
                  <div className="text-xs font-mono opacity-60 group-hover/step:opacity-100 transition-opacity">
                    <span className="opacity-30 mr-2">STEP {step.step}</span>
                    {step.content}
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
            {suggestions.map((suggestion, i) => (
              <button
                key={i}
                onClick={() => sendMessage(currentSessionId!, suggestion)}
                className={cn(
                  "px-4 py-2 rounded-xl text-xs font-medium transition-all border flex items-center gap-2 group",
                  theme === 'dark' 
                    ? "bg-white/5 border-white/10 hover:border-cyber-neon/50 hover:bg-cyber-neon/5 text-gray-400 hover:text-cyber-neon" 
                    : "bg-black/5 border-black/10 hover:border-cyber-purple/50 hover:bg-cyber-purple/5 text-gray-600 hover:text-cyber-purple"
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
      `- 退出码：${exitCode}`,
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
