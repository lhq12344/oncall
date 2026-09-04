import type { AIOpsStep, CommandAction, InterruptData } from '../types';
import { RUN_EVENT_SCHEMA, type AgentEventState, type RunEvent } from './types';

export function createInitialAgentEventState(): AgentEventState {
  return {
    status: 'idle',
    content: '',
    steps: [],
    interrupts: [],
    commandActions: [],
    errors: [],
    seenEventIds: new Set(),
    lastSequence: 0,
    sequenceGaps: []
  };
}

export function reduceRunEvent(state: AgentEventState, event: RunEvent): AgentEventState {
  if (event.version !== RUN_EVENT_SCHEMA || state.seenEventIds.has(event.id)) {
    return state;
  }
  const next = cloneState(state);
  next.seenEventIds.add(event.id);
  next.runId = event.run_id || next.runId;
  next.traceId = event.trace_id || next.traceId;
  if (event.sequence > next.lastSequence + 1 && next.lastSequence !== 0) {
    next.sequenceGaps.push({ expected: next.lastSequence + 1, received: event.sequence });
  }
  if (event.sequence > next.lastSequence) {
    next.lastSequence = event.sequence;
  }

  switch (event.type) {
    case 'run.started':
      next.status = 'running';
      break;
    case 'message.token':
    case 'model.delta':
      next.content += stringPayload(event.payload, 'content') || stringPayload(event.payload, 'delta');
      break;
    case 'phase.started':
    case 'phase.completed':
    case 'tool.requested':
    case 'tool.started':
    case 'tool.call':
    case 'tool.result':
    case 'workflow.state':
      appendCommandAction(next, event.payload);
      upsertStep(next.steps, stepFromEvent(event));
      break;
    case 'approval.required':
    case 'run.interrupt':
      next.status = 'waiting_for_approval';
      next.interrupts.push(interruptFromPayload(event.payload));
      break;
    case 'approval.resolved':
      next.status = 'running';
      break;
    case 'run.completed':
    case 'run.finished':
      next.status = 'completed';
      break;
    case 'run.failed':
    case 'error':
      next.status = 'failed';
      next.errors.push(stringPayload(event.payload, 'error') || stringPayload(event.payload, 'content') || 'Unknown error');
      break;
  }
  return next;
}

function cloneState(state: AgentEventState): AgentEventState {
  return {
    ...state,
    steps: [...state.steps],
    interrupts: [...state.interrupts],
    commandActions: [...state.commandActions],
    errors: [...state.errors],
    seenEventIds: new Set(state.seenEventIds),
    sequenceGaps: [...state.sequenceGaps]
  };
}

function stringPayload(payload: unknown, key: string): string {
  if (!payload || typeof payload !== 'object') return '';
  const value = (payload as Record<string, unknown>)[key];
  return typeof value === 'string' ? value : '';
}

function stepFromEvent(event: RunEvent): AIOpsStep {
  const payload = (event.payload || {}) as Record<string, unknown>;
  const step = typeof payload.step === 'number' ? payload.step : event.sequence;
  const content = stringPayload(payload, 'content') || stringPayload(payload, 'phase') || stringPayload(payload, 'tool') || event.type;
  const rawStatus = stringPayload(payload, 'status');
  const status = rawStatus === 'error' ? 'error' : rawStatus === 'pending' ? 'pending' : 'completed';
  return { step, content, status };
}

function upsertStep(steps: AIOpsStep[], incoming: AIOpsStep) {
  const index = steps.findIndex((step) => step.step === incoming.step);
  if (index >= 0) {
    steps[index] = { ...steps[index], ...incoming };
  } else {
    steps.push(incoming);
  }
  steps.sort((left, right) => left.step - right.step);
}

function appendCommandAction(state: AgentEventState, payload: unknown) {
  if (!payload || typeof payload !== 'object') return;
  const action = (payload as Record<string, unknown>).command_action as CommandAction | undefined;
  if (action?.trusted_control === true) {
    state.commandActions.push(action);
  }
}

function interruptFromPayload(payload: unknown): InterruptData {
  const value = (payload || {}) as Record<string, any>;
  return {
    checkpoint_id: typeof value.checkpoint_id === 'string' ? value.checkpoint_id : '',
    message: typeof value.message === 'string' ? value.message : '',
    interrupt_contexts: Array.isArray(value.interrupt_contexts) ? value.interrupt_contexts : [],
    bash_request: value.bash_request,
    detail_request: value.detail_request,
    workflow: value.workflow,
    resume_endpoint: value.resume_endpoint
  };
}
