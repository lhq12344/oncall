export interface IncidentStateView {
  incidentId: string;
  workflowVersion: string;
  terminal?: 'complete' | 'failed' | 'waiting_for_approval';
  evidenceCount: number;
  planRevision?: number;
}
