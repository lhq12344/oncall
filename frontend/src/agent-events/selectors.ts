import type { AgentEventState } from './types';

export function selectContent(state: AgentEventState) {
  return state.content;
}

export function selectLatestInterrupt(state: AgentEventState) {
  return state.interrupts[state.interrupts.length - 1];
}

export function selectHasSequenceGaps(state: AgentEventState) {
  return state.sequenceGaps.length > 0;
}
