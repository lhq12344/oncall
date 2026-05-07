import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import { Message, Session, ChatStep, InterruptData } from '../types';

// mergeMessageSteps 合并消息步骤，优先按 step 编号更新已有项，避免恢复执行时覆盖历史步骤。
// 输入：existing 现有步骤、incoming 新步骤。
// 输出：合并后的步骤列表。
function mergeMessageSteps(existing: ChatStep[] = [], incoming: ChatStep[] = []): ChatStep[] {
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
  updateLastMessage: (sessionId: string, content: string, steps?: ChatStep[], interrupt?: InterruptData) => void;
  appendStepToLastMessage: (sessionId: string, step: ChatStep) => void;
  setLastMessageStepStatus: (sessionId: string, status: ChatStep['status']) => void;
  setStreaming: (isStreaming: boolean) => void;
  setConnectionStatus: (status: AppState['connectionStatus']) => void;
  sendMessage: (sessionId: string, content: string) => Promise<void>;

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
