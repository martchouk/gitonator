import { useEffect, useMemo, useState } from 'react';
import { ChevronRight, ArrowRight, GitBranch, Clock } from 'lucide-react';
import { useParams, Link } from 'react-router-dom';
import useSWR from 'swr';
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  Node,
  Edge,
} from 'reactflow';
import dagre from '@dagrejs/dagre';
import 'reactflow/dist/style.css';

import type { CompletedRunDetail, GraphNode, GraphEdge } from '../api/types';
import { get } from '../api/client';
import { StatusChip } from '../components/StatusChip';
import {
  CompletedWorkflowNode,
  CompletedWorkflowNodeData,
} from '../components/CompletedWorkflowNode';

const nodeTypes = { completedStatus: CompletedWorkflowNode };

const categoryEdgeColors: Record<string, string> = {
  intake: 'var(--status-color-intake)',
  design: 'var(--status-color-design)',
  requirements: 'var(--status-color-requirements)',
  planning: 'var(--status-color-planning)',
  implementation: 'var(--status-color-implementation)',
  review: 'var(--status-color-review)',
  acceptance: 'var(--status-color-acceptance)',
  terminal: 'var(--status-color-terminal)',
  exception: 'var(--status-color-exception)',
};

function inferCategory(status: string): string {
  if (!status) return 'terminal';
  const s = status.toLowerCase();
  if (s.includes('blocked') || s.includes('rejected')) return 'exception';
  if (s.includes('done') || s.includes('closed') || s.includes('complete')) return 'terminal';
  if (s.includes('review') || s.includes('code-review')) return 'review';
  if (s.includes('approval') || s.includes('acceptance') || s.includes('po-')) return 'acceptance';
  if (s.includes('dev') || s.includes('implementation') || s.includes('progress')) return 'implementation';
  if (s.includes('planning') || s.includes('plan')) return 'planning';
  if (s.includes('design') || s.includes('solution') || s.includes('story')) return 'design';
  if (s.includes('triage') || s.includes('new') || s.includes('intake')) return 'intake';
  if (s.includes('require') || s.includes('definition')) return 'requirements';
  return 'terminal';
}

function relativeTime(ts: string): string {
  if (!ts) return '–';
  const diff = Date.now() - new Date(ts).getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function layoutCompletedNodes(
  graphNodes: GraphNode[],
  graphEdges: GraphEdge[],
  visitedStatuses: Set<string>,
  takenEdgeKeys: Set<string>,
  edgeCounts: Map<string, number>,
  nodeActors: Map<string, string>,
  finalStatus: string,
  direction: 'TB' | 'LR'
): { nodes: Node<CompletedWorkflowNodeData>[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: direction, nodesep: 40, ranksep: 60 });

  const W = 200;
  const H = 82;

  graphNodes.forEach((n) => g.setNode(n.id, { width: W, height: H }));
  graphEdges.forEach((e) => {
    if (e.source !== '__bootstrap__') g.setEdge(e.source, e.target);
  });
  dagre.layout(g);

  const rfNodes: Node<CompletedWorkflowNodeData>[] = graphNodes.map((n) => {
    const pos = g.node(n.id) ?? { x: 0, y: 0 };
    return {
      id: n.id,
      type: 'completedStatus',
      position: { x: pos.x - W / 2, y: pos.y - H / 2 },
      data: {
        label: n.label,
        role: n.role,
        category: n.category,
        terminal: n.terminal,
        visited: visitedStatuses.has(n.id),
        isFinal: n.id === finalStatus,
        lastActor: nodeActors.get(n.id),
      },
    };
  });

  const rfEdges: Edge[] = graphEdges
    .filter((e) => e.source !== '__bootstrap__')
    .map((e) => {
      const key = `${e.source}--${e.target}`;
      const taken = takenEdgeKeys.has(key);
      const count = edgeCounts.get(key) ?? 0;
      const targetNode = graphNodes.find((n) => n.id === e.target);
      const cat = targetNode
        ? (targetNode.category || inferCategory(e.target))
        : inferCategory(e.target);
      const color = taken
        ? (categoryEdgeColors[cat] ?? 'var(--color-primary)')
        : 'var(--md-sys-color-outline-variant)';
      return {
        id: e.id,
        source: e.source,
        target: e.target,
        label: taken && count > 1 ? `×${count}` : e.description || undefined,
        type: 'smoothstep',
        animated: taken,
        style: {
          stroke: color,
          strokeWidth: taken ? 2.5 : 1,
          opacity: taken ? 1 : 0.18,
        },
        labelStyle: {
          fontSize: '10px',
          fill: taken ? color : 'var(--md-sys-color-on-surface-variant)',
          fontWeight: taken ? 600 : 400,
        },
        labelBgStyle: { fill: 'var(--md-sys-color-surface)', fillOpacity: 0.9 },
      };
    });

  return { nodes: rfNodes, edges: rfEdges };
}

export function CompletedRun() {
  const { number } = useParams<{ number: string }>();
  const { data, error } = useSWR<CompletedRunDetail>(
    number ? `/api/v1/dashboard/completed/${number}` : null,
    (url: string) => get<CompletedRunDetail>(url)
  );

  const [isMobile] = useState(() => window.innerWidth <= 600);
  const direction = isMobile ? 'LR' : 'TB';
  const [selectedStep, setSelectedStep] = useState<number | null>(null);

  // Compute which nodes/edges were actually traversed
  const { visitedStatuses, takenEdgeKeys, edgeCounts, nodeActors } = useMemo(() => {
    if (!data?.audit?.length) {
      return {
        visitedStatuses: new Set<string>(),
        takenEdgeKeys: new Set<string>(),
        edgeCounts: new Map<string, number>(),
        nodeActors: new Map<string, string>(),
      };
    }
    // Audit comes DESC from the API; reverse to chronological order
    const chrono = [...data.audit].reverse();
    const visitedStatuses = new Set<string>();
    const takenEdgeKeys = new Set<string>();
    const edgeCounts = new Map<string, number>();
    const nodeActors = new Map<string, string>();

    for (const row of chrono) {
      if (row.from_status) visitedStatuses.add(row.from_status);
      if (row.to_status) visitedStatuses.add(row.to_status);
      if (row.from_status && row.to_status) {
        const key = `${row.from_status}--${row.to_status}`;
        takenEdgeKeys.add(key);
        edgeCounts.set(key, (edgeCounts.get(key) ?? 0) + 1);
      }
      if (row.to_status && row.actor) {
        nodeActors.set(row.to_status, row.actor);
      }
    }
    return { visitedStatuses, takenEdgeKeys, edgeCounts, nodeActors };
  }, [data?.audit]);

  const { nodes: layoutedNodes, edges: layoutedEdges } = useMemo(() => {
    if (!data?.workflow?.nodes) return { nodes: [], edges: [] };
    return layoutCompletedNodes(
      data.workflow.nodes,
      data.workflow.edges ?? [],
      visitedStatuses,
      takenEdgeKeys,
      edgeCounts,
      nodeActors,
      data.finalStatus,
      direction
    );
  }, [data, visitedStatuses, takenEdgeKeys, edgeCounts, nodeActors, direction]);

  const [rfNodes, setRfNodes, onNodesChange] = useNodesState<CompletedWorkflowNodeData>([]);
  const [rfEdges, setRfEdges, onEdgesChange] = useEdgesState([]);

  useEffect(() => {
    setRfNodes(layoutedNodes);
    setRfEdges(layoutedEdges);
  }, [layoutedNodes, layoutedEdges, setRfNodes, setRfEdges]);

  if (error) {
    return (
      <div>
        <Link
          to="/completed"
          style={{ color: 'var(--md-sys-color-primary)', textDecoration: 'none', fontSize: '0.875rem' }}
        >
          ← Completed
        </Link>
        <p style={{ color: 'var(--md-sys-color-error)', marginTop: 'var(--spacing-md)' }}>
          {error.message?.startsWith('404')
            ? 'No completed run found for this issue.'
            : `Error: ${error.message}`}
        </p>
      </div>
    );
  }

  if (!data) {
    return (
      <div style={{ padding: 'var(--spacing-lg)', color: 'var(--color-text-muted)' }}>
        Loading run…
      </div>
    );
  }

  const chronAudit = [...data.audit].reverse();

  return (
    <div>
      {/* Breadcrumb */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--spacing-xs)',
          marginBottom: 'var(--spacing-md)',
          fontSize: '0.875rem',
          color: 'var(--color-text-muted)',
        }}
      >
        <Link
          to="/completed"
          style={{ color: 'var(--md-sys-color-primary)', textDecoration: 'none' }}
        >
          Completed
        </Link>
        <ChevronRight size={14} />
        <span>
          #{data.issueNumber}
          {data.repo && <span style={{ marginLeft: '6px' }}>{data.repo}</span>}
        </span>
      </div>

      {/* Page header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--spacing-md)',
          marginBottom: 'var(--spacing-lg)',
          flexWrap: 'wrap',
        }}
      >
        <h2 className="page-title" style={{ margin: 0 }}>
          Issue #{data.issueNumber}
        </h2>
        <StatusChip status={data.finalStatus} />
        {data.workflowKey && (
          <span
            style={{
              fontSize: '0.75rem',
              padding: '2px 10px',
              border: '1px solid var(--color-border)',
              borderRadius: 'var(--radius-full)',
              color: 'var(--color-text-muted)',
              fontFamily: 'var(--font-mono)',
            }}
          >
            {data.workflowKey}
          </span>
        )}
        <span
          style={{
            fontSize: '0.75rem',
            color: 'var(--color-text-muted)',
            marginLeft: 'auto',
          }}
        >
          {data.stepCount} transition{data.stepCount !== 1 ? 's' : ''} · completed{' '}
          {relativeTime(data.completedAt)}
        </span>
      </div>

      {/* Graph + timeline */}
      <div
        style={{
          display: 'flex',
          height: 'calc(100vh - 230px)',
          gap: 'var(--spacing-md)',
          minHeight: '400px',
        }}
      >
        {/* Graph panel */}
        <div
          style={{
            flex: 1,
            border: '1px solid var(--md-sys-color-outline-variant)',
            borderRadius: 'var(--md-shape-medium)',
            overflow: 'hidden',
            minHeight: 0,
          }}
        >
          {data.workflow ? (
            <ReactFlow
              nodes={rfNodes}
              edges={rfEdges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              nodeTypes={nodeTypes}
              fitView
              fitViewOptions={{ padding: 0.2 }}
              panOnScroll
              zoomOnPinch
              style={{ background: 'var(--md-sys-color-background)' }}
            >
              <Background color="var(--md-sys-color-outline-variant)" gap={24} />
              <Controls />
              {!isMobile && (
                <MiniMap nodeColor={() => 'var(--md-sys-color-surface-variant)'} />
              )}
            </ReactFlow>
          ) : (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                height: '100%',
                gap: 'var(--spacing-sm)',
                color: 'var(--color-text-muted)',
              }}
            >
              <GitBranch size={40} style={{ opacity: 0.4 }} />
              <span>No workflow graph available</span>
              <span style={{ fontSize: '0.875rem' }}>
                Workflow "{data.workflowKey || '(unknown)'}" not found in loaded definitions
              </span>
            </div>
          )}
        </div>

        {/* Timeline sidebar */}
        <div
          style={{
            width: '300px',
            flexShrink: 0,
            border: '1px solid var(--md-sys-color-outline-variant)',
            borderRadius: 'var(--md-shape-medium)',
            background: 'var(--md-sys-color-surface)',
            display: 'flex',
            flexDirection: 'column',
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              padding: '10px 14px',
              borderBottom: '1px solid var(--md-sys-color-outline-variant)',
              fontWeight: 600,
              fontSize: '0.8125rem',
              letterSpacing: '-0.01em',
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              flexShrink: 0,
            }}
          >
            <Clock size={14} style={{ color: 'var(--color-neon-green)' }} />
            Transitions
            <span
              style={{
                marginLeft: 'auto',
                fontSize: '0.6875rem',
                fontWeight: 500,
                color: 'var(--color-text-muted)',
              }}
            >
              {chronAudit.length} steps
            </span>
          </div>

          <div style={{ flex: 1, overflowY: 'auto', padding: '6px' }}>
            {chronAudit.map((row, idx) => {
              const isSelected = selectedStep === idx;
              return (
                <div
                  key={row.id ?? idx}
                  onClick={() => setSelectedStep(isSelected ? null : idx)}
                  style={{
                    padding: '8px 10px',
                    borderRadius: 'var(--radius-md)',
                    cursor: 'pointer',
                    marginBottom: '3px',
                    border: `1px solid ${isSelected ? 'var(--color-border-strong)' : 'transparent'}`,
                    background: isSelected ? 'var(--color-primary-subtle)' : 'transparent',
                    transition: 'background 120ms ease',
                  }}
                >
                  {/* Step + time */}
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      marginBottom: '5px',
                    }}
                  >
                    <span
                      style={{
                        fontSize: '0.6875rem',
                        fontWeight: 600,
                        fontFamily: 'var(--font-mono)',
                        color: 'var(--color-neon-amber)',
                      }}
                    >
                      step {idx + 1}
                    </span>
                    <span
                      style={{ fontSize: '0.6875rem', color: 'var(--color-text-muted)' }}
                    >
                      {relativeTime(row.created_at)}
                    </span>
                  </div>

                  {/* from → to */}
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '4px',
                      flexWrap: 'wrap',
                      marginBottom: '4px',
                    }}
                  >
                    {row.from_status ? (
                      <StatusChip status={row.from_status} />
                    ) : (
                      <span
                        style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}
                      >
                        start
                      </span>
                    )}
                    <ArrowRight size={12} style={{ color: 'var(--color-text-muted)', flexShrink: 0 }} />
                    <StatusChip status={row.to_status} />
                  </div>

                  {/* Actor + trigger */}
                  {row.actor && (
                    <div
                      style={{ fontSize: '0.6875rem', fontFamily: 'var(--font-mono)', color: 'var(--color-neon-cyan)' }}
                    >
                      {row.actor}
                      {row.trigger_type && (
                        <span
                          style={{
                            marginLeft: '5px',
                            padding: '1px 5px',
                            borderRadius: 'var(--radius-sm)',
                            background: 'var(--color-surface-raised)',
                            fontSize: '0.625rem',
                            letterSpacing: '0.02em',
                          }}
                        >
                          {row.trigger_type}
                        </span>
                      )}
                    </div>
                  )}

                  {/* Expanded detail */}
                  {isSelected && (
                    <div
                      style={{
                        marginTop: '8px',
                        paddingTop: '8px',
                        borderTop: '1px solid var(--color-border)',
                        fontSize: '0.75rem',
                        display: 'flex',
                        flexDirection: 'column',
                        gap: '4px',
                        color: 'var(--color-text)',
                      }}
                    >
                      {row.result && (
                        <div>
                          <span style={{ color: 'var(--color-text-muted)' }}>
                            result:{' '}
                          </span>
                          <span
                            style={{
                              color:
                                row.result === 'success' || row.result === 'allowed'
                                  ? 'var(--color-success)'
                                  : 'var(--color-error)',
                              fontWeight: 500,
                            }}
                          >
                            {row.result}
                          </span>
                        </div>
                      )}
                      {row.reason && (
                        <div>
                          <span style={{ color: 'var(--color-text-muted)' }}>
                            reason:{' '}
                          </span>
                          {row.reason}
                        </div>
                      )}
                      {row.from_assignee && (
                        <div>
                          <span style={{ color: 'var(--color-text-muted)' }}>
                            from assignee:{' '}
                          </span>
                          {row.from_assignee}
                        </div>
                      )}
                      {row.to_assignee && (
                        <div>
                          <span style={{ color: 'var(--color-text-muted)' }}>
                            to assignee:{' '}
                          </span>
                          {row.to_assignee}
                        </div>
                      )}
                      <div>
                        <span style={{ color: 'var(--color-text-muted)' }}>at: </span>
                        {new Date(row.created_at).toLocaleString()}
                      </div>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
