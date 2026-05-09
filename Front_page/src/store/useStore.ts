import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import { Message, Session, ChatStep, InterruptData } from '../types';

function createClientId() {
  const cryptoApi = globalThis.crypto;
  if (typeof cryptoApi?.randomUUID === 'function') {
    try {
      return cryptoApi.randomUUID();
    } catch {
      // crypto.randomUUID is unavailable on some non-secure LAN origins.
    }
  }

  if (typeof cryptoApi?.getRandomValues === 'function') {
    const bytes = new Uint8Array(16);
    cryptoApi.getRandomValues(bytes);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    const hex = [...bytes].map((byte) => byte.toString(16).padStart(2, '0'));
    return [
      hex.slice(0, 4).join(''),
      hex.slice(4, 6).join(''),
      hex.slice(6, 8).join(''),
      hex.slice(8, 10).join(''),
      hex.slice(10, 16).join(''),
    ].join('-');
  }

  return `id-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

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
  streamingBySession: Record<string, boolean>;
  connectionStatusBySession: Record<string, 'idle' | 'connecting' | 'streaming' | 'error'>;
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
  isSessionStreaming: (sessionId: string | null | undefined) => boolean;
  getSessionConnectionStatus: (sessionId: string | null | undefined) => 'idle' | 'connecting' | 'streaming' | 'error';
  setStreaming: (sessionId: string, isStreaming: boolean) => void;
  setConnectionStatus: (sessionId: string, status: 'idle' | 'connecting' | 'streaming' | 'error') => void;
  sendMessage: (sessionId: string, content: string) => Promise<void>;

  setRehydrated: (val: boolean) => void;
}

export const useStore = create<AppState>()(
  persist(
    (set, get) => ({
      theme: 'dark',
      sessions: [],
      currentSessionId: null,
      streamingBySession: {},
      connectionStatusBySession: {},

      isRehydrated: false,
      isSidebarOpen: true,

      toggleTheme: () => set((state) => ({ theme: state.theme === 'dark' ? 'light' : 'dark' })),
      toggleSidebar: () => set((state) => ({ isSidebarOpen: !state.isSidebarOpen })),
      setSidebarOpen: (isOpen) => set({ isSidebarOpen: isOpen }),

      addSession: (title = 'New Session') => {
        const id = createClientId();
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
        streamingBySession: Object.fromEntries(Object.entries(state.streamingBySession).filter(([sessionId]) => sessionId !== id)),
        connectionStatusBySession: Object.fromEntries(Object.entries(state.connectionStatusBySession).filter(([sessionId]) => sessionId !== id)),
      })),

      renameSession: (id, title) => set((state) => ({
        sessions: state.sessions.map((s) => s.id === id ? { ...s, title } : s),
      })),

      setCurrentSession: (id) => set({ currentSessionId: id }),

      addMessage: (sessionId, message) => {
        const id = createClientId();
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

      isSessionStreaming: (sessionId) => {
        if (!sessionId) {
          return false;
        }
        return Boolean(get().streamingBySession[sessionId]);
      },

      getSessionConnectionStatus: (sessionId) => {
        if (!sessionId) {
          return 'idle';
        }
        return get().connectionStatusBySession[sessionId] || 'idle';
      },

      setStreaming: (sessionId, isStreaming) => set((state) => ({
        streamingBySession: {
          ...state.streamingBySession,
          [sessionId]: isStreaming
        }
      })),

      setConnectionStatus: (sessionId, status) => set((state) => ({
        connectionStatusBySession: {
          ...state.connectionStatusBySession,
          [sessionId]: status
        }
      })),

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

        setStreaming(sessionId, true);
        setConnectionStatus(sessionId, 'streaming');

        const { streamChat } = await import('../services/api');
        try {
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
              setStreaming(sessionId, false);
              setConnectionStatus(sessionId, 'idle');
            },
            onError: (err) => {
              setLastMessageStepStatus(sessionId, 'error');
              setStreaming(sessionId, false);
              setConnectionStatus(sessionId, 'error');
              updateLastMessage(sessionId, `\n\nError: ${err}`);
            }
          });
        } catch (err) {
          const errMsg = err instanceof Error ? err.message : String(err);
          setLastMessageStepStatus(sessionId, 'error');
          setStreaming(sessionId, false);
          setConnectionStatus(sessionId, 'error');
          updateLastMessage(sessionId, `\n\nError: ${errMsg}`);
        }
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
