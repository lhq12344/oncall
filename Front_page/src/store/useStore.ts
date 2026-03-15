import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import { Message, Session, AIOpsStep, InterruptData, OpsStep } from '../types';

// inferOpsStepTitle 依据内容推断步骤标题，避免无 step 事件时内容丢失。
// 输入：SSE content 文本。
// 输出：步骤标题。
function inferOpsStepTitle(content: string): string {
  const text = (content || '').trim();
  if (!text) {
    return '流程输出';
  }
  if (text.includes('运维技术报告') || text.includes('最终状态') || text.includes('是否已解决')) {
    return '输出最终技术报告';
  }
  return '流程输出';
}

// mergeMessageSteps 合并消息步骤，优先按 step 编号更新已有项，避免恢复执行时覆盖历史步骤。
// 输入：existing 现有步骤、incoming 新步骤。
// 输出：合并后的步骤列表。
function mergeMessageSteps(existing: AIOpsStep[] = [], incoming: AIOpsStep[] = []): AIOpsStep[] {
  const merged = [...existing];
  for (const step of incoming) {
    const index = merged.findIndex((item) => item.step === step.step);
    if (index >= 0) {
      merged[index] = { ...merged[index], ...step };
      continue;
    }
    merged.push(step);
  }
  return merged.sort((left, right) => left.step - right.step);
}

interface AppState {
  theme: 'dark' | 'light';
  sessions: Session[];
  currentSessionId: string | null;
  isStreaming: boolean;
  connectionStatus: 'idle' | 'connecting' | 'streaming' | 'error';
  
  // Ops Panel State
  isOpsPanelOpen: boolean;
  opsSteps: OpsStep[];
  currentOpsTask: string;
  isOpsRunning: boolean;
  isRehydrated: boolean;
  isSidebarOpen: boolean;

  toggleTheme: () => void;
  toggleSidebar: () => void;
  setSidebarOpen: (isOpen: boolean) => void;
  addSession: (title?: string) => string;
  deleteSession: (id: string) => void;
  renameSession: (id: string, title: string) => void;
  setCurrentSession: (id: string) => void;
  addMessage: (sessionId: string, message: Omit<Message, 'id' | 'timestamp'>) => string;
  updateLastMessage: (sessionId: string, content: string, steps?: AIOpsStep[], interrupt?: InterruptData) => void;
  appendStepToLastMessage: (sessionId: string, step: AIOpsStep) => void;
  setLastMessageStepStatus: (sessionId: string, status: AIOpsStep['status']) => void;
  setStreaming: (isStreaming: boolean) => void;
  setConnectionStatus: (status: AppState['connectionStatus']) => void;
  sendMessage: (sessionId: string, content: string) => Promise<void>;

  // Ops Actions
  setOpsPanelOpen: (isOpen: boolean) => void;
  runOps: (taskName: string) => Promise<void>;
  clearOps: () => void;
  addOpsStep: (toolName: string, content?: string, status?: OpsStep['status'], interrupt?: InterruptData) => string;
  updateOpsStep: (id: string, content?: string, status?: OpsStep['status'], interrupt?: InterruptData) => void;
  markOpsInterruptHandled: (id: string, handled: boolean) => void;
  setOpsRunning: (isRunning: boolean) => void;
  setRehydrated: (val: boolean) => void;
}

export const useStore = create<AppState>()(
  persist(
    (set, get) => ({
      theme: 'dark',
      sessions: [],
      currentSessionId: null,
      isStreaming: false,
      connectionStatus: 'idle',

      isOpsPanelOpen: false,
      opsSteps: [],
      currentOpsTask: '',
      isOpsRunning: false,
      isRehydrated: false,
      isSidebarOpen: true,

      toggleTheme: () => set((state) => ({ theme: state.theme === 'dark' ? 'light' : 'dark' })),
      toggleSidebar: () => set((state) => ({ isSidebarOpen: !state.isSidebarOpen })),
      setSidebarOpen: (isOpen) => set({ isSidebarOpen: isOpen }),

      addSession: (title = 'New Session') => {
        const id = crypto.randomUUID();
        set((state) => ({
          sessions: [
            { id, title, messages: [], updatedAt: Date.now() },
            ...state.sessions,
          ],
          currentSessionId: id,
        }));
        return id;
      },

      deleteSession: (id) => set((state) => ({
        sessions: state.sessions.filter((s) => s.id !== id),
        currentSessionId: state.currentSessionId === id ? (state.sessions.find(s => s.id !== id)?.id || null) : state.currentSessionId,
      })),

      renameSession: (id, title) => set((state) => ({
        sessions: state.sessions.map((s) => s.id === id ? { ...s, title } : s),
      })),

      setCurrentSession: (id) => set({ currentSessionId: id }),

      addMessage: (sessionId, message) => {
        const id = crypto.randomUUID();
        set((state) => ({
          sessions: state.sessions.map((s) => {
            if (s.id !== sessionId) return s;
            
            const isFirstMessage = s.messages.length === 0 && message.role === 'user';
            const title = isFirstMessage 
              ? (message.content.substring(0, 50) || '新对话') 
              : s.title;

            return { 
              ...s, 
              title,
              messages: [...s.messages, { ...message, id, timestamp: Date.now() }],
              updatedAt: Date.now()
            };
          }),
        }));
        return id;
      },

      updateLastMessage: (sessionId, content, steps, interrupt) => set((state) => ({
        sessions: state.sessions.map((s) => {
          if (s.id !== sessionId || s.messages.length === 0) return s;
          const lastMessage = s.messages[s.messages.length - 1];
          const updatedMessages = [...s.messages];
          updatedMessages[updatedMessages.length - 1] = {
            ...lastMessage,
            content: content !== undefined ? lastMessage.content + content : lastMessage.content,
            steps: steps ? mergeMessageSteps(lastMessage.steps, steps) : lastMessage.steps,
            interrupt: interrupt || lastMessage.interrupt,
          };
          return { ...s, messages: updatedMessages, updatedAt: Date.now() };
        }),
      })),

      appendStepToLastMessage: (sessionId, step) => set((state) => ({
        sessions: state.sessions.map((s) => {
          if (s.id !== sessionId || s.messages.length === 0) return s;
          const updatedMessages = [...s.messages];
          const lastIndex = updatedMessages.length - 1;
          const lastMessage = updatedMessages[lastIndex];
          const existingSteps = [...(lastMessage.steps || [])];

          if (existingSteps.length > 0) {
            const previousIndex = existingSteps.length - 1;
            if (existingSteps[previousIndex].status === 'pending') {
              existingSteps[previousIndex] = {
                ...existingSteps[previousIndex],
                status: 'completed'
              };
            }
          }

          updatedMessages[lastIndex] = {
            ...lastMessage,
            steps: mergeMessageSteps(existingSteps, [{
              ...step,
              status: step.status || 'pending'
            }])
          };

          return { ...s, messages: updatedMessages, updatedAt: Date.now() };
        }),
      })),

      setLastMessageStepStatus: (sessionId, status) => set((state) => ({
        sessions: state.sessions.map((s) => {
          if (s.id !== sessionId || s.messages.length === 0) return s;
          const updatedMessages = [...s.messages];
          const lastIndex = updatedMessages.length - 1;
          const lastMessage = updatedMessages[lastIndex];
          const existingSteps = [...(lastMessage.steps || [])];
          if (existingSteps.length === 0) {
            return s;
          }

          existingSteps[existingSteps.length - 1] = {
            ...existingSteps[existingSteps.length - 1],
            status
          };

          updatedMessages[lastIndex] = {
            ...lastMessage,
            steps: existingSteps
          };

          return { ...s, messages: updatedMessages, updatedAt: Date.now() };
        }),
      })),

      setStreaming: (isStreaming) => set({ isStreaming }),
      setConnectionStatus: (status) => set({ connectionStatus: status }),

      setOpsPanelOpen: (isOpen) => set({ isOpsPanelOpen: isOpen }),
      clearOps: () => set({ opsSteps: [], currentOpsTask: '', isOpsRunning: false }),
      addOpsStep: (toolName, content = '', status = 'pending', interrupt) => {
        const id = crypto.randomUUID();
        set((state) => ({
          opsSteps: [...state.opsSteps, {
            id,
            toolName,
            content,
            status,
            interrupt
          }]
        }));
        return id;
      },

      updateOpsStep: (id, content, status, interrupt) => set((state) => ({
        opsSteps: state.opsSteps.map((step) => 
          step.id === id 
            ? { 
                ...step, 
                content: content !== undefined ? step.content + content : step.content,
                status: status || step.status,
                interrupt: interrupt || step.interrupt
              } 
            : step
        )
      })),
      markOpsInterruptHandled: (id, handled) => set((state) => ({
        opsSteps: state.opsSteps.map((step) =>
          step.id === id
            ? {
                ...step,
                interrupt: step.interrupt ? { ...step.interrupt, handled } : step.interrupt
              }
            : step
        )
      })),
      setOpsRunning: (isRunning) => set({ isOpsRunning: isRunning }),

      runOps: async (taskName) => {
        const { updateOpsStep, clearOps, addOpsStep } = get();
        clearOps();
        set({ isOpsPanelOpen: true, isOpsRunning: true, currentOpsTask: taskName });

        const { streamOps } = await import('../services/api');

        let currentStepId = '';
        let pausedByInterrupt = false;
        const createOpsStep = (toolName: string): string => {
          const id = addOpsStep(toolName);
          currentStepId = id;
          return id;
        };

        await streamOps({
          onStep: (step) => {
            // Mark previous step as completed if exists
            if (currentStepId) {
              updateOpsStep(currentStepId, undefined, 'completed');
            }

            createOpsStep(step.content); // backend sends tool name in content for type: step
          },
          onContent: (content) => {
            const normalized = (content || '').trim();
            if (!normalized) {
              return;
            }
            if (!currentStepId) {
              createOpsStep(inferOpsStepTitle(normalized));
            }
            updateOpsStep(currentStepId, content);
          },
          onInterrupt: (interrupt) => {
            pausedByInterrupt = true;
            if (!currentStepId) {
              createOpsStep(interrupt.bash_request?.raw_command ? '执行确认' : '人工确认');
            }
            updateOpsStep(currentStepId, undefined, undefined, interrupt);
          },
          onDone: () => {
            if (currentStepId) {
              updateOpsStep(currentStepId, undefined, 'completed');
            }
            set({ isOpsRunning: pausedByInterrupt });
          },
          onError: (err) => {
            if (!currentStepId) {
              createOpsStep('流程异常');
            }
            updateOpsStep(currentStepId, `\n\nError: ${err}`, 'error');
            set({ isOpsRunning: false });
          }
        });
      },

      sendMessage: async (sessionId, content) => {
        const {
          addMessage,
          updateLastMessage,
          appendStepToLastMessage,
          setLastMessageStepStatus,
          setStreaming,
          setConnectionStatus
        } = get();
        
        addMessage(sessionId, {
          role: 'user',
          type: 'user',
          content,
        });

        addMessage(sessionId, {
          role: 'assistant',
          type: 'text',
          content: '',
        });

        setStreaming(true);
        setConnectionStatus('streaming');

        const { streamChat } = await import('../services/api');

        await streamChat(sessionId, content, {
          onContent: (chunk) => updateLastMessage(sessionId, chunk),
          onStep: (step) => {
            appendStepToLastMessage(sessionId, {
              ...step,
              status: 'pending'
            });
          },
          onInterrupt: (interrupt) => updateLastMessage(sessionId, '', undefined, interrupt),
          onDone: () => {
            setLastMessageStepStatus(sessionId, 'completed');
            setStreaming(false);
            setConnectionStatus('idle');
          },
          onError: (err) => {
            setLastMessageStepStatus(sessionId, 'error');
            setStreaming(false);
            setConnectionStatus('error');
            updateLastMessage(sessionId, `\n\nError: ${err}`);
          }
        });
      },

      setRehydrated: (val) => set({ isRehydrated: val }),
    }),
    {
      name: 'oncall_history',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({ 
        sessions: state.sessions.slice(0, 50),
        theme: state.theme,
        opsSteps: state.opsSteps,
        currentOpsTask: state.currentOpsTask,
        isSidebarOpen: state.isSidebarOpen
      }),
      onRehydrateStorage: (state) => {
        return (state, error) => {
          if (state) {
            state.setRehydrated(true);
          }
        };
      }
    }
  )
);
