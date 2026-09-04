export type KnowledgeStatus = 'draft' | 'reviewed' | 'validated' | 'indexed_staging' | 'evaluated' | 'canary' | 'published' | 'superseded' | 'expired';

export interface KnowledgeCandidateView {
  id: string;
  caseId: string;
  status: KnowledgeStatus;
  owner: string;
  source: string;
  scope: string;
  version: string;
  rollbackVersion: string;
}
