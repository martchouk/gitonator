import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
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

import type { WorkflowGraph as WorkflowGraphType, GraphNode, GraphEdge } from '../api/types';
import { get } from '../api/client';
import { WorkflowNode, WorkflowNodeData } from '../components/WorkflowNode';
import { StatusChip } from '../components/StatusChip';

const nodeTypes = { workflowStatus: WorkflowNode };

function layoutNodes(
  nodes: GraphNode[],
  edges: GraphEdge[],
  direction: 'TB' | 'LR' = 'TB'
): { nodes: Node<WorkflowNodeData>[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: direction, nodesep: 40, ranksep: 60 });

  const NODE_WIDTH = 180;
  const NODE_HEIGHT = 70;

  nodes.forEach((n) => {
    g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  });

  edges.forEach((e) => {
    if (e.source !== '__bootstrap__') {
      g.setEdge(e.source, e.target);
    }
  });

  dagre.layout(g);

  const rfNodes: Node<WorkflowNodeData>[] = nodes.map((n) => {
    const pos = g.node(n.id) ?? { x: 0, y: 0 };
    return {
      id: n.id,
      type: 'workflowStatus',
      position: { x: pos.x - NODE_WIDTH / 2, y: pos.y - NODE_HEIGHT / 2 },
      data: {
        label: n.label,
        role: n.role,
        category: n.category,
        terminal: n.terminal,
      },
    };
  });

  const rfEdges: Edge[] = edges
    .filter((e) => e.source !== '__bootstrap__')
    .map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      label: e.description,
      type: e.source === e.target ? 'smoothstep' : 'default',
      style: {
        stroke: 'var(--md-sys-color-outline-variant)',
        strokeWidth: 1.5,
        opacity: e.source === e.target ? 0.6 : 1,
      },
      labelStyle: { fontSize: '10px', fill: 'var(--md-sys-color-on-surface-variant)' },
      labelBgStyle: { fill: 'var(--md-sys-color-surface)', fillOpacity: 0.9 },
    }));

  return { nodes: rfNodes, edges: rfEdges };
}

// API returns the workflow object directly (not nested under a key)
type GraphApiResponse = WorkflowGraphType;

export function WorkflowGraph() {
  const { id } = useParams<{ id: string }>();
  const { data, error } = useSWR<GraphApiResponse>(
    id ? `/api/v1/workflows/${id}` : null,
    (url: string) => get<GraphApiResponse>(url)
  );

  const [isMobile] = useState(() => window.innerWidth <= 600);
  const direction = isMobile ? 'LR' : 'TB';

  const { nodes: layoutedNodes, edges: layoutedEdges } = useMemo(() => {
    if (!data?.nodes) return { nodes: [], edges: [] };
    return layoutNodes(data.nodes, data.edges ?? [], direction);
  }, [data, direction]);

  const [rfNodes, setRfNodes, onNodesChange] = useNodesState<WorkflowNodeData>([]);
  const [rfEdges, setRfEdges, onEdgesChange] = useEdgesState([]);

  useEffect(() => {
    setRfNodes(layoutedNodes);
    setRfEdges(layoutedEdges);
  }, [layoutedNodes, layoutedEdges, setRfNodes, setRfEdges]);

  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      const gn = data?.nodes.find((n) => n.id === node.id) ?? null;
      setSelectedNode((prev) => (prev?.id === gn?.id ? null : gn));
    },
    [data]
  );

  if (error) {
    return (
      <div style={{ color: 'var(--md-sys-color-error)', padding: 'var(--spacing-lg)' }}>
        Failed to load workflow: {error.message}
      </div>
    );
  }

  if (!data) {
    return (
      <div style={{ padding: 'var(--spacing-lg)', color: 'var(--md-sys-color-on-surface-variant)' }}>
        Loading workflow graph…
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 140px)', gap: 'var(--spacing-md)' }}>
      {/* Graph */}
      <div style={{ flex: 1, border: '1px solid var(--md-sys-color-outline-variant)', borderRadius: 'var(--md-shape-medium)', overflow: 'hidden' }}>
        <div
          style={{
            padding: '12px 16px',
            borderBottom: '1px solid var(--md-sys-color-outline-variant)',
            background: 'var(--md-sys-color-surface)',
            fontWeight: 500,
          }}
        >
          {data.id}
          <span
            style={{
              marginLeft: 'var(--spacing-sm)',
              fontSize: '0.75rem',
              color: 'var(--md-sys-color-on-surface-variant)',
            }}
          >
            key: {data.key}
          </span>
        </div>
        <ReactFlow
          nodes={rfNodes}
          edges={rfEdges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          panOnScroll
          zoomOnPinch
          style={{ background: 'var(--md-sys-color-background)' }}
        >
          <Background color="var(--md-sys-color-outline-variant)" gap={24} />
          <Controls />
          {!isMobile && <MiniMap nodeColor={() => 'var(--md-sys-color-surface-variant)'} />}
        </ReactFlow>
      </div>

      {/* Sidebar panel (shown when a node is selected) */}
      {selectedNode && (
        <div
          style={{
            width: '300px',
            border: '1px solid var(--md-sys-color-outline-variant)',
            borderRadius: 'var(--md-shape-medium)',
            background: 'var(--md-sys-color-surface)',
            padding: 'var(--spacing-lg)',
            flexShrink: 0,
            overflowY: 'auto',
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
            <h3 style={{ margin: 0, fontSize: '1.375rem', fontWeight: 400 }}>
              {selectedNode.label.startsWith('status:')
                ? selectedNode.label.slice(7)
                : selectedNode.label}
            </h3>
            <button
              aria-label="Close panel"
              onClick={() => setSelectedNode(null)}
              style={{
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                color: 'var(--md-sys-color-on-surface-variant)',
                minWidth: '48px',
                minHeight: '48px',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <span className="material-icons">close</span>
            </button>
          </div>

          <div style={{ marginTop: 'var(--spacing-md)', display: 'flex', flexDirection: 'column', gap: 'var(--spacing-sm)' }}>
            {selectedNode.role && (
              <SidebarDetail label="Role">
                <StatusChip status={selectedNode.role} />
              </SidebarDetail>
            )}
            <SidebarDetail label="Category">
              <code style={{ fontFamily: "'Roboto Mono', monospace", fontSize: '0.875rem' }}>
                {selectedNode.category}
              </code>
            </SidebarDetail>
            <SidebarDetail label="Terminal">
              {selectedNode.terminal ? 'Yes' : 'No'}
            </SidebarDetail>

            <div style={{ marginTop: 'var(--spacing-sm)' }}>
              <div
                style={{
                  fontSize: '0.75rem',
                  fontWeight: 500,
                  color: 'var(--md-sys-color-on-surface-variant)',
                  marginBottom: '8px',
                  textTransform: 'uppercase',
                  letterSpacing: '0.08em',
                }}
              >
                Transitions from here
              </div>
              {(data?.edges ?? [])
                .filter((e) => e.source === selectedNode.id)
                .map((e) => (
                  <div
                    key={e.id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '8px',
                      padding: '6px 0',
                      borderBottom: '1px solid var(--md-sys-color-outline-variant)',
                      fontSize: '0.875rem',
                    }}
                  >
                    <span className="material-icons" style={{ fontSize: '16px', color: 'var(--md-sys-color-on-surface-variant)' }}>
                      arrow_forward
                    </span>
                    <StatusChip status={e.target} />
                  </div>
                ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function SidebarDetail({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <div
        style={{
          fontSize: '0.6875rem',
          fontWeight: 500,
          color: 'var(--md-sys-color-on-surface-variant)',
          marginBottom: '2px',
        }}
      >
        {label}
      </div>
      <div style={{ fontSize: '0.875rem' }}>{children}</div>
    </div>
  );
}
