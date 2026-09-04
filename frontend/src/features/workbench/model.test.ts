import { strict as assert } from 'node:assert';
import { buildWorkbenchModel } from './model';

const messages = [
  {
    id: 'm1',
    role: 'assistant' as const,
    type: 'text' as const,
    content: 'answer',
    timestamp: 0,
    steps: [{ step: 1, content: 'rag.retrieve', status: 'completed' as const }],
  },
];

const model = buildWorkbenchModel('session-1', messages, [
  { id: 'o1', toolName: 'kubectl.get', content: 'pods', status: 'completed' },
]);

assert.equal(model.traceId, 'ui-trace-session-1');
assert.equal(model.runId, 'ui-run-session-1');
assert.equal(model.nodes.length, 3);
assert.equal(model.retrievalSnapshotId, 'snapshot-session-1');
assert.equal(model.reviewCaseCount, 0);
assert.equal(model.knowledgeStatus, 'not_started');

const failed = buildWorkbenchModel('session-2', [], [
  { id: 'o2', toolName: 'redis', content: 'unavailable', status: 'error' },
]);
assert.equal(failed.knowledgeStatus, 'draft');
assert.equal(failed.reviewCaseCount, 1);

console.log('workbench model tests passed');
