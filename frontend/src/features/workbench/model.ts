import type { Message, OpsStep } from '../../types';

export interface WorkbenchNode {
  id: string;
  name: string;
  kind: 'workflow' | 'model' | 'tool' | 'rag' | 'message' | 'error';
  status: 'complete' | 'pending' | 'error';
  detail?: string;
}

export interface WorkbenchModel {
  traceId: string;
  runId: string;
  nodes: WorkbenchNode[];
  retrievalSnapshotId?: string;
  reviewCaseCount: number;
  knowledgeStatus: 'not_started' | 'draft' | 'canary' | 'published';
}

function messageNodes(messages: Message[]): WorkbenchNode[] {
  const nodes: WorkbenchNode[] = [];
  for (const message of messages) {
    if (message.role !== 'assistant') continue;
    if (message.content.trim()) {
      nodes.push({
        id: `message-${message.id}`,
        name: 'assistant.answer',
        kind: 'message',
        status: 'complete',
        detail: message.content.slice(0, 180),
      });
    }
    for (const step of message.steps ?? []) {
      nodes.push({
        id: `step-${message.id}-${step.step}`,
        name: step.content || `workflow.step.${step.step}`,
        kind: step.content.toLowerCase().includes('rag') ? 'rag' : 'workflow',
        status: step.status === 'error' ? 'error' : step.status === 'pending' ? 'pending' : 'complete',
      });
    }
    if (message.interrupt) {
      nodes.push({
        id: `interrupt-${message.id}`,
        name: 'approval.required',
        kind: 'workflow',
        status: 'pending',
        detail: message.interrupt.message,
      });
    }
  }
  return nodes;
}

function opsNodes(steps: OpsStep[]): WorkbenchNode[] {
  return steps.map((step) => ({
    id: `ops-${step.id}`,
    name: step.toolName || 'ops.step',
    kind: 'tool' as const,
    status: step.status === 'error' ? 'error' : step.status === 'pending' ? 'pending' : 'complete',
    detail: step.content.slice(0, 180),
  }));
}

export function buildWorkbenchModel(
  sessionId: string | null,
  messages: Message[],
  opsSteps: OpsStep[],
): WorkbenchModel {
  const nodes = [...messageNodes(messages), ...opsNodes(opsSteps)];
  const hasError = nodes.some((node) => node.status === 'error');
  const retrievalNode = nodes.find((node) => node.kind === 'rag');
  const reviewCaseCount = hasError ? 1 : 0;

  return {
    traceId: sessionId ? `ui-trace-${sessionId}` : 'ui-trace-none',
    runId: sessionId ? `ui-run-${sessionId}` : 'ui-run-none',
    nodes,
    retrievalSnapshotId: retrievalNode ? `snapshot-${sessionId ?? 'none'}` : undefined,
    reviewCaseCount,
    knowledgeStatus: hasError ? 'draft' : 'not_started',
  };
}
