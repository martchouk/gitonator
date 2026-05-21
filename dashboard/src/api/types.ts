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
  repo: string;
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
  claimed_by?: { String: string; Valid: boolean } | null;
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
  transitionId: string;
  source: string;
  target: string;
  allowedRoles: string[];
  guard?: string;
  queuesNextRole?: string;
  requiredOutputs?: unknown;
  closeIssue?: boolean;
  reopenIssue?: boolean;
  terminalAfterTransition?: boolean;
  description?: string;
}

export interface WorkflowGuard {
  description?: string;
  any_label?: string[];
  all_absent?: string[];
}

export interface WorkflowIssueType {
  id: string;
  name: string;
  poDefinitionOutput?: string;
  defaultPath?: string[];
}

export interface WorkflowGraph {
  id: string;
  key: string;
  description?: string;
  roles?: string[];
  supportedTypes?: string[];
  defaultPathScope?: string;
  issueTypes?: WorkflowIssueType[];
  guards?: Record<string, WorkflowGuard>;
  canonicalPaths?: Record<string, string[]>;
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export interface WorkflowSummary {
  id: string;
  key: string;
  description?: string;
  statusCount: number;
  edgeCount: number;
  roleCount: number;
  issueTypeCount: number;
}

export interface CompletedIssueSummary {
  issueNumber: number;
  title: string;
  repo: string;
  finalStatus: string;
  workflowKey: string;
  completedAt: string;
  stepCount: number;
}

export interface CompletedRunDetail {
  issueNumber: number;
  repo: string;
  workflowKey: string;
  finalStatus: string;
  completedAt: string;
  stepCount: number;
  audit: AuditRow[];
  tasks: TaskRow[];
  workflow?: WorkflowGraph;
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
