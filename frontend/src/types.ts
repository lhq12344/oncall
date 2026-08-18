export type MessageType = 'text' | 'step' | 'interrupt' | 'error' | 'user';
export type WorkflowKind = 'chat' | 'ops';
export type ResumeEndpoint = 'chat_resume_stream' | 'ai_ops_resume_stream';
export type SlashCommandType = 'local' | 'prompt' | 'ops_workflow' | 'client_action' | 'deferred';
export type SlashCommandSource = 'builtin' | 'project' | 'mew_compat';
export type CommandActionName = 'clear_session';

export interface InterruptContext {
  id: string;
  address: string;
  info: string;
  is_root_cause: boolean;
}

export interface BashApprovalRequest {
  command: string;
  args: string[];
  timeout: number;
  reason?: string;
  raw_command: string;
}

export interface DetailOption {
  label: string;
  value: string;
  description?: string;
}

export interface DetailRequest {
  field: string;
  question: string;
  reason?: string;
  options: DetailOption[];
}

export interface InterruptData {
  checkpoint_id: string;
  interrupt_contexts: InterruptContext[];
  message: string;
  bash_request?: BashApprovalRequest;
  detail_request?: DetailRequest;
  handled?: boolean;
  workflow?: WorkflowKind;
  resume_endpoint?: ResumeEndpoint;
}

export interface AIOpsStep {
  step: number;
  content: string;
  status: 'pending' | 'completed' | 'error';
}

export interface SlashCommandInfo {
  name: string;
  aliases?: string[];
  description: string;
  argument_hint?: string;
  type: SlashCommandType;
  source: SlashCommandSource;
}

export interface CommandAction {
  action: CommandActionName;
  trusted_control: true;
  scope?: string;
  [key: string]: unknown;
}

export interface OpsStep {
  id: string;
  toolName: string;
  content: string;
  status: 'pending' | 'completed' | 'error';
  interrupt?: InterruptData;
}

export interface Message {
  id: string;
  role: 'user' | 'assistant' | 'system';
  type: MessageType;
  content: string;
  timestamp: number;
  steps?: AIOpsStep[];
  interrupt?: InterruptData;
}

export interface Session {
  id: string;
  title: string;
  messages: Message[];
  updatedAt: number;
}
