export type FailureCategory =
  | 'missing_knowledge'
  | 'stale_knowledge'
  | 'chunking_failure'
  | 'query_rewrite_failure'
  | 'retrieval_failure'
  | 'rerank_failure'
  | 'intent_failure'
  | 'prompt_failure'
  | 'model_failure'
  | 'tool_failure'
  | 'workflow_failure'
  | 'environment_or_permission'
  | 'user_request_out_of_scope';

export interface ReviewCaseView {
  id: string;
  normalizedQuestion: string;
  failureCategory?: FailureCategory;
  retrievalSnapshotId?: string;
  runId: string;
  traceId: string;
  priority: number;
}
