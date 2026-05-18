export interface ActiveTask {
  id: number;
  role: string;
  taskStatus: string;
  bridgeId?: string;
  createdAt: string;
}

export interface Issue {
  number: number;
  title: string;
  url: string;
  currentStatus: string;
  assignees: string[];
  activeTask?: ActiveTask;
  updatedAt: string;
}

export interface TaskRow {
  id: number;
  issue_number: number;
  repo: string;
  role: string;
  assignee: string;
  last_comment_id: number;
  current_status: string;
  status: string;
  dedup_key: string;
  bridge_id: { String: string; Valid: boolean } | null;
  created_at: string;
  claimed_at?: { String: string; Valid: boolean } | null;
  finished_at?: { String: string; Valid: boolean } | null;
}

export interface AuditRow {
  id: number;
  issue_number: number;
  from_status: string;
  to_status: string;
  from_assignee: string;
  to_assignee: string;
  actor: string;
  trigger_type: string;
  result: string;
  reason: string;
  created_at: string;
}

export interface GraphNode {
  id: string;
  role: string;
  category: string;
  terminal: boolean;
  label: string;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  allowedRoles: string[];
  guard?: string;
  description?: string;
}

export interface WorkflowGraph {
  id: string;
  key: string;
  description?: string;
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export interface WorkflowSummary {
  id: string;
  key: string;
  statusCount: number;
  edgeCount: number;
}

export interface SSEEventData {
  issue_number?: number;
  task_id?: number;
  role?: string;
  bridge_id?: string;
  status?: string;
  assignee?: string;
  clients?: number;
}

export interface SSEEvent {
  type: string;
  data: SSEEventData;
}
