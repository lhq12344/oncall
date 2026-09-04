export interface TraceNode {
  traceId: string;
  spanId: string;
  parentId?: string;
  name: string;
  latencyMs: number;
  error?: string;
}

export function buildTraceTree(nodes: TraceNode[]) {
  return nodes.map((node) => ({ ...node, children: nodes.filter((child) => child.parentId === node.spanId) }));
}
