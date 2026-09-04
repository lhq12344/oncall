import type { AIOpsStep, CommandAction, InterruptData } from '../types';

export const RUN_EVENT_SCHEMA = 'oncall.event/v1';

export type RunEventType =
  | 'run.started'
  | 'run.completed'
  | 'phase.started'
  | 'phase.completed'
  | 'model.delta'
  | 'message.token'
  | 'tool.requested'
  | 'tool.started'
  | 'tool.result'
  | 'tool.call'
  | 'approval.required'
  | 'approval.resolved'
  | 'workflow.state'
  | 'rag.retrieval'
  | 'context.compacted'
  | 'artifact.created'
  | 'run.interrupt'
  | 'error'
  | 'run.finished'
  | 'run.failed';

export interface UIHints {
  collapse?: boolean;
  channel?: 'chat' | 'ops' | 'trace';
  label?: string;
}

export interface RunEvent<TPayload = Record<string, unknown>> {
  version: typeof RUN_EVENT_SCHEMA;
  id: string;
  run_id: string;
  trace_id?: string;
  sequence: number;
  type: RunEventType;
  timestamp: string;
  payload?: TPayload;
  ui?: UIHints;
}

export interface AgentEventState {
  runId?: string;
  traceId?: string;
  status: 'idle' | 'running' | 'waiting_for_approval' | 'completed' | 'failed';
  content: string;
  steps: AIOpsStep[];
  interrupts: InterruptData[];
  commandActions: CommandAction[];
  errors: string[];
  seenEventIds: Set<string>;
  lastSequence: number;
  sequenceGaps: Array<{ expected: number; received: number }>;
}
