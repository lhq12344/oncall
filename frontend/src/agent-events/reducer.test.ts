import { strict as assert } from 'node:assert';
import { parseStreamData } from './client';
import { createInitialAgentEventState, reduceRunEvent } from './reducer';
import type { RunEvent } from './types';

function event(id: string, sequence: number, type: RunEvent['type'], payload?: Record<string, unknown>): RunEvent {
  return { version: 'oncall.event/v1', id, run_id: 'run', trace_id: 'trace', sequence, type, timestamp: new Date(0).toISOString(), payload };
}

let state = createInitialAgentEventState();
const events: RunEvent[] = [];
for (let i = 1; i <= 1000; i++) {
  events.push(event(`event-${i}`, i, i % 10 === 0 ? 'tool.result' : 'message.token', i % 10 === 0 ? { step: i, content: `tool-${i}` } : { content: 'x' }));
}

for (const item of events) state = reduceRunEvent(state, item);
for (const item of events.slice().reverse()) state = reduceRunEvent(state, item);
state = reduceRunEvent(state, event('event-1002', 1002, 'run.completed'));

assert.equal(state.content.length, 900);
assert.equal(state.steps.length, 100);
assert.equal(state.seenEventIds.size, 1001);
assert.equal(state.sequenceGaps.length, 1);
assert.equal(state.status, 'completed');

let reconnectState = createInitialAgentEventState();
const toolRequested = event('tool-1-requested', 1, 'tool.started', { step: 1, content: 'k8s_monitor started' });
const toolResult = event('tool-1-result', 2, 'tool.result', { step: 1, content: 'pod api-1 ready' });
const replayedToolResult = event('tool-1-result', 2, 'tool.result', { step: 1, content: 'pod api-1 ready' });
const completionAfterReconnect = event('run-completed', 3, 'run.completed');

reconnectState = reduceRunEvent(reconnectState, toolRequested);
reconnectState = reduceRunEvent(reconnectState, toolResult);
reconnectState = reduceRunEvent(reconnectState, replayedToolResult);
reconnectState = reduceRunEvent(reconnectState, completionAfterReconnect);

assert.equal(reconnectState.steps.length, 1);
assert.equal(reconnectState.steps[0]?.content, 'pod api-1 ready');
assert.equal(reconnectState.seenEventIds.size, 3);
assert.equal(reconnectState.status, 'completed');

assert.equal(parseStreamData(JSON.stringify({ type: 'content', content: 'legacy text' })), undefined);
assert.equal(parseStreamData(JSON.stringify({ type: 'done' })), undefined);
assert.equal(parseStreamData(JSON.stringify({ type: 'error', content: 'legacy error' })), undefined);
assert.deepEqual(
  parseStreamData(JSON.stringify(event('event-versioned', 1, 'message.token', { content: 'versioned' }))),
  event('event-versioned', 1, 'message.token', { content: 'versioned' })
);
