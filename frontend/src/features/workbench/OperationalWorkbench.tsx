import React from 'react';
import { Activity, AlertTriangle, BrainCircuit, CheckCircle2, Database, GitBranch, Wrench, X } from 'lucide-react';
import { useStore } from '../../store/useStore';
import { buildWorkbenchModel, type WorkbenchNode } from './model';

function nodeIcon(node: WorkbenchNode) {
  if (node.status === 'error') return <AlertTriangle className="w-3.5 h-3.5 text-red-400" />;
  if (node.kind === 'tool') return <Wrench className="w-3.5 h-3.5 text-cyber-orange" />;
  if (node.kind === 'rag') return <Database className="w-3.5 h-3.5 text-cyber-purple" />;
  if (node.kind === 'workflow') return <GitBranch className="w-3.5 h-3.5 text-cyber-neon" />;
  return <BrainCircuit className="w-3.5 h-3.5 text-cyber-green" />;
}

export const OperationalWorkbench: React.FC = () => {
  const { theme, isWorkbenchOpen, setWorkbenchOpen, sessions, currentSessionId, opsSteps } = useStore();
  if (!isWorkbenchOpen) return null;

  const session = sessions.find((item) => item.id === currentSessionId);
  const model = buildWorkbenchModel(currentSessionId, session?.messages ?? [], opsSteps);

  return (
    <aside className={`fixed right-4 top-20 bottom-4 z-[90] w-[min(30rem,calc(100vw-2rem))] overflow-hidden border clip-path-corner shadow-2xl ${theme === 'dark' ? 'border-cyber-neon/30 bg-black/90' : 'border-cyber-purple/30 bg-white/95'}`}>
      <div className="flex items-center justify-between border-b border-white/10 px-5 py-4">
        <div className="flex items-center gap-2">
          <Activity className="h-4 w-4 text-cyber-neon" />
          <div>
            <div className="text-xs font-display font-black uppercase tracking-widest">Operational Workbench</div>
            <div className="text-[9px] font-mono opacity-50">TRACE / REVIEW / KNOWLEDGE</div>
          </div>
        </div>
        <button onClick={() => setWorkbenchOpen(false)} className="rounded-lg p-2 opacity-60 hover:bg-white/10 hover:opacity-100" aria-label="关闭工作台">
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="grid grid-cols-2 gap-2 border-b border-white/10 p-4 text-[10px] font-mono">
        <div className="rounded-lg border border-cyber-neon/20 bg-cyber-neon/5 p-3"><span className="opacity-50">TRACE</span><div className="mt-1 truncate text-cyber-neon">{model.traceId}</div></div>
        <div className="rounded-lg border border-cyber-purple/20 bg-cyber-purple/5 p-3"><span className="opacity-50">RUN</span><div className="mt-1 truncate text-cyber-purple">{model.runId}</div></div>
        <div className="rounded-lg border border-white/10 p-3"><span className="opacity-50">REVIEW CASE</span><div className="mt-1 text-lg font-bold">{model.reviewCaseCount}</div></div>
        <div className="rounded-lg border border-white/10 p-3"><span className="opacity-50">KNOWLEDGE</span><div className="mt-1 uppercase text-cyber-green">{model.knowledgeStatus}</div></div>
      </div>

      <div className="max-h-[calc(100%-11rem)] overflow-y-auto p-4">
        <div className="mb-3 flex items-center gap-2 text-[10px] font-display font-bold uppercase tracking-widest opacity-60">
          <GitBranch className="h-3.5 w-3.5" /> Trace Tree / Workflow State / Tool Timing
        </div>
        <div className="mb-3 rounded-lg border border-yellow-400/20 bg-yellow-400/5 px-3 py-2 text-[10px] font-mono text-yellow-200/80">
          当前为客户端派生观测快照；真实 CozeLoop Trace、ReviewCase 和知识发布状态需以后端/平台证据为准。
        </div>
        {model.nodes.length === 0 ? (
          <div className="rounded-xl border border-dashed border-white/15 p-8 text-center text-xs opacity-50">当前会话还没有可观测事件</div>
        ) : (
          <div className="space-y-2">
            {model.nodes.map((node, index) => (
              <div key={node.id} className="flex gap-3 rounded-xl border border-white/10 bg-white/[0.03] p-3">
                <div className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-md border border-white/10">{nodeIcon(node)}</div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-xs font-bold">{index + 1}. {node.name}</span>
                    {node.status === 'complete' ? <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-cyber-green" /> : <span className={`text-[9px] uppercase ${node.status === 'error' ? 'text-red-400' : 'text-cyber-orange'}`}>{node.status}</span>}
                  </div>
                  {node.detail && <div className="mt-1 line-clamp-3 text-[10px] font-mono opacity-55">{node.detail}</div>}
                </div>
              </div>
            ))}
          </div>
        )}
        <div className="mt-4 rounded-xl border border-cyber-purple/20 bg-cyber-purple/5 p-3 text-[10px] font-mono">
          <div className="flex items-center gap-2 text-cyber-purple"><Database className="h-3.5 w-3.5" /> RetrievalSnapshot</div>
          <div className="mt-2 opacity-70">{model.retrievalSnapshotId ?? '暂无检索快照；当前回答未触发 RAG'}</div>
        </div>
      </div>
    </aside>
  );
};
