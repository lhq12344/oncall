import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import { Message, Session, AIOpsStep, InterruptData, OpsStep } from '../types';

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
  setStreaming: (isStreaming: boolean) => void;
  setConnectionStatus: (status: AppState['connectionStatus']) => void;
  sendMessage: (sessionId: string, content: string) => Promise<void>;

  // Ops Actions
  setOpsPanelOpen: (isOpen: boolean) => void;
  runOps: (taskName: string) => Promise<void>;
  clearOps: () => void;
  updateOpsStep: (id: string, content?: string, status?: OpsStep['status'], interrupt?: InterruptData) => void;
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
            steps: steps || lastMessage.steps,
            interrupt: interrupt || lastMessage.interrupt,
          };
          return { ...s, messages: updatedMessages, updatedAt: Date.now() };
        }),
      })),

      setStreaming: (isStreaming) => set({ isStreaming }),
      setConnectionStatus: (status) => set({ connectionStatus: status }),

      setOpsPanelOpen: (isOpen) => set({ isOpsPanelOpen: isOpen }),
      clearOps: () => set({ opsSteps: [], currentOpsTask: '', isOpsRunning: false }),

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

      runOps: async (taskName) => {
        const { updateOpsStep, clearOps } = get();
        clearOps();
        set({ isOpsPanelOpen: true, isOpsRunning: true, currentOpsTask: taskName });

        const { streamOps } = await import('../services/api');

        let currentStepId = '';

        await streamOps({
          onStep: (step) => {
            // Mark previous step as completed if exists
            if (currentStepId) {
              updateOpsStep(currentStepId, undefined, 'completed');
            }
            
            const id = crypto.randomUUID();
            currentStepId = id;
            set((state) => ({
              opsSteps: [...state.opsSteps, {
                id,
                toolName: step.content, // backend sends tool name in content for type: step
                content: '',
                status: 'pending'
              }]
            }));
          },
          onContent: (content) => {
            if (currentStepId) {
              updateOpsStep(currentStepId, content);
            }
          },
          onInterrupt: (interrupt) => {
            if (currentStepId) {
              updateOpsStep(currentStepId, undefined, undefined, interrupt);
            }
          },
          onDone: () => {
            if (currentStepId) {
              updateOpsStep(currentStepId, undefined, 'completed');
            }
            set({ isOpsRunning: false });
          },
          onError: (err) => {
            if (currentStepId) {
              updateOpsStep(currentStepId, `\n\nError: ${err}`, 'error');
            }
            set({ isOpsRunning: false });
          }
        });
      },

      sendMessage: async (sessionId, content) => {
        const { addMessage, updateLastMessage, setStreaming, setConnectionStatus } = get();
        
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
          onInterrupt: (interrupt) => updateLastMessage(sessionId, '', undefined, interrupt),
          onDone: () => {
            setStreaming(false);
            setConnectionStatus('idle');
          },
          onError: (err) => {
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
