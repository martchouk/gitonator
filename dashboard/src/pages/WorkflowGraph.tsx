import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { ArrowDown, Footprints, GitBranch, Info, ListFilter, Route, X } from 'lucide-react';
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

import type {
  WorkflowGraph as WorkflowGraphType,
  GraphNode,
  GraphEdge,
  WorkflowIssueType,
} from '../api/types';
import { get } from '../api/client';
import { WorkflowNode, WorkflowNodeData } from '../components/WorkflowNode';
import { StatusChip } from '../components/StatusChip';

const nodeTypes = { workflowStatus: WorkflowNode };
const SWIMLANE_STEP_COLUMN_WIDTH = 56;

const categoryColors: Record<string, string> = {
  intake: 'var(--status-color-intake)',
  design: 'var(--status-color-design)',
  requirements: 'var(--status-color-requirements)',
  planning: 'var(--status-color-planning)',
  implementation: 'var(--status-color-implementation)',
  review: 'var(--status-color-review)',
  verification: 'var(--status-color-acceptance)',
  acceptance: 'var(--status-color-intake)',
  exception: 'var(--status-color-blocked)',
  terminal: 'var(--status-color-terminal)',
};

type GraphApiResponse = WorkflowGraphType;
type ViewMode = 'path' | 'swimlane' | 'full';
type DetailSelection =
  | { kind: 'node'; node: GraphNode }
  | { kind: 'edge'; edge: GraphEdge }
  | null;

interface PathPreset {
  id: string;
  label: string;
  source: 'canonical' | 'issueType' | 'fallback';
  statuses: string[];
}

type SwimlaneConnector = {
  id: string;
  edge: GraphEdge;
  d: string;
};

function layoutNodes(
  nodes: GraphNode[],
  edges: GraphEdge[],
  direction: 'TB' | 'LR' = 'LR'
): { nodes: Node<WorkflowNodeData>[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: direction, nodesep: 44, ranksep: 78 });

  const NODE_WIDTH = 188;
  const NODE_HEIGHT = 78;

  nodes.forEach((n) => {
    g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  });

  edges.forEach((e) => {
    if (e.source !== '__bootstrap__') {
      g.setEdge(e.source, e.target);
    }
  });

  dagre.layout(g);

  return {
    nodes: nodes.map((n) => {
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
    }),
    edges: edges
      .filter((e) => e.source !== '__bootstrap__')
      .map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        label: compactTransitionName(e.transitionId || e.id),
        type: e.source === e.target ? 'smoothstep' : 'default',
        animated: isLoopOrException(e),
        style: {
          stroke: edgeColor(e),
          strokeWidth: isLoopOrException(e) ? 1.25 : 2,
          opacity: isLoopOrException(e) ? 0.72 : 0.95,
        },
        labelStyle: { fontSize: '10px', fill: 'var(--md-sys-color-on-surface-variant)' },
        labelBgStyle: { fill: 'var(--md-sys-color-surface)', fillOpacity: 0.92 },
      })),
  };
}

export function WorkflowGraph() {
  const { id } = useParams<{ id: string }>();
  const { data, error } = useSWR<GraphApiResponse>(
    id ? `/api/v1/workflows/${id}` : null,
    (url: string) => get<GraphApiResponse>(url)
  );

  const presets = useMemo(() => (data ? buildPathPresets(data) : []), [data]);
  const [selectedPresetID, setSelectedPresetID] = useState('');
  const [viewMode, setViewMode] = useState<ViewMode>('swimlane');
  const [detailMode, setDetailMode] = useState<'human' | 'engine'>('human');
  const [showLoops, setShowLoops] = useState(false);
  const [showExceptions, setShowExceptions] = useState(false);
  const [showTerminal, setShowTerminal] = useState(false);
  const [selection, setSelection] = useState<DetailSelection>(null);

  useEffect(() => {
    if (!selectedPresetID && presets.length > 0) {
      setSelectedPresetID(presets[0].id);
    }
  }, [presets, selectedPresetID]);

  const selectedPreset = presets.find((p) => p.id === selectedPresetID) ?? presets[0];
  const nodeByID = useMemo(() => new Map((data?.nodes ?? []).map((n) => [n.id, n])), [data]);
  const visibleEdges = useMemo(() => {
    if (!data) return [];
    return filterEdges(data, selectedPreset?.statuses ?? [], {
      full: viewMode === 'full',
      loops: showLoops,
      exceptions: showExceptions,
      terminal: showTerminal,
    });
  }, [data, selectedPreset, viewMode, showLoops, showExceptions, showTerminal]);
  const visibleNodeIDs = useMemo(() => {
    if (viewMode === 'full') {
      return new Set(visibleEdges.flatMap((e) => [e.source, e.target]).filter((s) => s !== '__bootstrap__'));
    }
    return new Set(selectedPreset?.statuses ?? []);
  }, [selectedPreset, viewMode, visibleEdges]);
  const visibleNodes = useMemo(
    () => (data?.nodes ?? []).filter((n) => visibleNodeIDs.has(n.id)),
    [data, visibleNodeIDs]
  );

  const { nodes: layoutedNodes, edges: layoutedEdges } = useMemo(() => {
    return layoutNodes(visibleNodes, visibleEdges, window.innerWidth <= 700 ? 'TB' : 'LR');
  }, [visibleNodes, visibleEdges]);

  const [rfNodes, setRfNodes, onNodesChange] = useNodesState<WorkflowNodeData>([]);
  const [rfEdges, setRfEdges, onEdgesChange] = useEdgesState([]);

  useEffect(() => {
    setRfNodes(layoutedNodes);
    setRfEdges(layoutedEdges);
  }, [layoutedNodes, layoutedEdges, setRfNodes, setRfEdges]);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      const graphNode = nodeByID.get(node.id);
      if (graphNode) setSelection({ kind: 'node', node: graphNode });
    },
    [nodeByID]
  );

  const onEdgeClick = useCallback(
    (_: React.MouseEvent, edge: Edge) => {
      const graphEdge = visibleEdges.find((e) => e.id === edge.id);
      if (graphEdge) setSelection({ kind: 'edge', edge: graphEdge });
    },
    [visibleEdges]
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
        Loading workflow graph...
      </div>
    );
  }

  return (
    <div
      style={{
        minHeight: 'calc(100vh - 112px)',
        display: 'grid',
        gridTemplateRows: 'auto 1fr',
        gap: 'var(--spacing-md)',
      }}
    >
      <header
        style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(260px, 1fr) auto',
          gap: 'var(--spacing-md)',
          alignItems: 'end',
          border: '1px solid var(--md-sys-color-outline-variant)',
          borderRadius: 'var(--radius-md)',
          padding: 'var(--spacing-md)',
          background: 'var(--md-sys-color-surface)',
        }}
      >
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' }}>
            <GitBranch size={18} style={{ color: 'var(--color-neon-cyan)' }} />
            <h2 className="page-title" style={{ margin: 0 }}>{workflowTitle(data)}</h2>
            <code style={smallCode}>key: {data.key}</code>
          </div>
          {data.description && (
            <p style={{ margin: '8px 0 0', color: 'var(--md-sys-color-on-surface-variant)', maxWidth: '900px' }}>
              {data.description}
            </p>
          )}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          <SelectControl
            label="Path"
            value={selectedPreset?.id ?? ''}
            onChange={setSelectedPresetID}
            options={presets.map((p) => ({ value: p.id, label: p.label }))}
          />
          <SegmentedControl
            value={viewMode}
            onChange={(v) => setViewMode(v as ViewMode)}
            options={[
              { value: 'swimlane', label: 'Swimlane' },
              { value: 'path', label: 'Main Path' },
              { value: 'full', label: 'Full Graph' },
            ]}
          />
        </div>
      </header>

      <main
        style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr) 340px',
          gridTemplateRows: 'auto 1fr',
          gap: 'var(--spacing-md)',
          minHeight: 0,
          alignItems: 'start',
        }}
      >
        <div
          style={{
            display: 'flex',
            gap: '8px',
            alignItems: 'center',
            flexWrap: 'wrap',
            padding: '0 2px',
          }}
        >
          <Toggle label="Loops" checked={showLoops} onChange={setShowLoops} />
          <Toggle label="Blocked" checked={showExceptions} onChange={setShowExceptions} />
          <Toggle label="Reject/Reopen" checked={showTerminal} onChange={setShowTerminal} />
          {data.defaultPathScope && <span style={mutedText}>default paths: {data.defaultPathScope}</span>}
        </div>

        <section
          style={{
            minWidth: 0,
            minHeight: 0,
          }}
        >
          {viewMode === 'full' ? (
            <GraphCanvas
              nodes={rfNodes}
              edges={rfEdges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onNodeClick={onNodeClick}
              onEdgeClick={onEdgeClick}
            />
          ) : viewMode === 'path' ? (
            <PathView
              data={data}
              preset={selectedPreset}
              nodeByID={nodeByID}
              onSelect={setSelection}
            />
          ) : (
            <SwimlaneView
              data={data}
              preset={selectedPreset}
              nodeByID={nodeByID}
              onSelect={setSelection}
            />
          )}
        </section>

        <div />

        <DetailsPanel
          data={data}
          selection={selection}
          detailMode={detailMode}
          setDetailMode={setDetailMode}
          onClose={() => setSelection(null)}
        />
      </main>
    </div>
  );
}

function GraphCanvas({
  nodes,
  edges,
  onNodesChange,
  onEdgesChange,
  onNodeClick,
  onEdgeClick,
}: {
  nodes: Node<WorkflowNodeData>[];
  edges: Edge[];
  onNodesChange: ReturnType<typeof useNodesState<WorkflowNodeData>>[2];
  onEdgesChange: ReturnType<typeof useEdgesState>[2];
  onNodeClick: (event: React.MouseEvent, node: Node) => void;
  onEdgeClick: (event: React.MouseEvent, edge: Edge) => void;
}) {
  return (
    <div style={canvasShell}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        onEdgeClick={onEdgeClick}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.18 }}
        panOnScroll
        zoomOnPinch
        style={{ background: 'var(--md-sys-color-background)' }}
      >
        <Background color="var(--md-sys-color-outline-variant)" gap={24} />
        <Controls />
        {window.innerWidth > 760 && <MiniMap nodeColor={() => 'var(--md-sys-color-surface-variant)'} />}
      </ReactFlow>
    </div>
  );
}

function PathView({
  data,
  preset,
  nodeByID,
  onSelect,
}: {
  data: WorkflowGraphType;
  preset?: PathPreset;
  nodeByID: Map<string, GraphNode>;
  onSelect: (selection: DetailSelection) => void;
}) {
  const statuses = preset?.statuses ?? [];
  return (
    <div style={canvasShell}>
      <div style={{ padding: 'var(--spacing-lg)', overflow: 'auto' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: 'var(--spacing-lg)' }}>
          <Route size={18} style={{ color: 'var(--color-neon-green)' }} />
          <div>
            <div style={{ fontWeight: 600 }}>{preset?.label ?? 'Workflow path'}</div>
            <div style={mutedText}>Normal route with exception paths hidden</div>
          </div>
        </div>
        <div style={pathStack}>
          {statuses.map((status, index) => {
            const node = nodeByID.get(status);
            const edge = index > 0 ? findEdge(data.edges, statuses[index - 1], status) : undefined;
            return (
              <React.Fragment key={status}>
                {index > 0 && (
                  <button
                    type="button"
                    onClick={() => edge && onSelect({ kind: 'edge', edge })}
                    style={pathSeparator(edge)}
                    title={edge?.description || edge?.transitionId}
                  >
                    <ArrowDown size={16} />
                    {edge?.guard && <span style={guardBadge}>{edge.guard}</span>}
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => node && onSelect({ kind: 'node', node })}
                  style={pathNode(node)}
                >
                  <div style={pathNodeHeader}>
                    <span style={{ color: node ? categoryColor(node.category) : undefined }}>
                      {prettyStatus(status)}
                    </span>
                    <span style={pathNodeMeta}>{node?.role || 'terminal'}</span>
                  </div>
                  <span style={pathNodeSummary}>{node?.category ?? 'terminal'}</span>
                </button>
              </React.Fragment>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function SwimlaneView({
  data,
  preset,
  nodeByID,
  onSelect,
}: {
  data: WorkflowGraphType;
  preset?: PathPreset;
  nodeByID: Map<string, GraphNode>;
  onSelect: (selection: DetailSelection) => void;
}) {
  const statuses = preset?.statuses ?? [];
  const roles = orderedRoles(data, statuses, nodeByID);
  const gridRef = useRef<HTMLDivElement | null>(null);
  const nodeRefs = useRef(new Map<string, HTMLButtonElement | null>());
  const [connectors, setConnectors] = useState<SwimlaneConnector[]>([]);
  const [hoveredNodeStatus, setHoveredNodeStatus] = useState<string | null>(null);
  const [hoveredEdgeID, setHoveredEdgeID] = useState<string | null>(null);

  const setNodeRef = useCallback((status: string, element: HTMLButtonElement | null) => {
    if (element) {
      nodeRefs.current.set(status, element);
    } else {
      nodeRefs.current.delete(status);
    }
  }, []);

  useLayoutEffect(() => {
    const measure = () => {
      const container = gridRef.current;
      if (!container) return;
      const containerRect = container.getBoundingClientRect();
      const next: SwimlaneConnector[] = [];
      statuses.slice(1).forEach((status, index) => {
        const previousStatus = statuses[index];
        const edge = findEdge(data.edges, previousStatus, status);
        if (!edge) return;
        const source = nodeRefs.current.get(previousStatus);
        const target = nodeRefs.current.get(status);
        if (!source || !target) return;
        const sourceNode = nodeByID.get(previousStatus);
        const targetNode = nodeByID.get(status);
        const sourceRect = source.getBoundingClientRect();
        const targetRect = target.getBoundingClientRect();
        const sourceCenterX = sourceRect.left + sourceRect.width / 2;
        const targetCenterX = targetRect.left + targetRect.width / 2;
        const sameRole = sourceNode?.role === targetNode?.role;
        const fromX = sourceCenterX - containerRect.left;
        const fromY = sourceRect.bottom - containerRect.top + 4;
        const toX = sameRole
          ? targetCenterX - containerRect.left
          : (targetCenterX >= sourceCenterX ? targetRect.left : targetRect.right) - containerRect.left + (targetCenterX >= sourceCenterX ? -4 : 4);
        const toY = sameRole ? targetRect.top - containerRect.top - 4 : targetRect.top + targetRect.height / 2 - containerRect.top;
        const deltaX = Math.abs(toX - fromX);
        const deltaY = Math.max(40, Math.abs(toY - fromY));
        const bend = Math.min(96, Math.max(28, deltaY / 2));
        const horizontalBias = Math.min(96, Math.max(24, deltaX / 2));
        const d = sameRole
          ? `M ${fromX} ${fromY} C ${fromX} ${fromY + bend}, ${toX} ${toY - bend}, ${toX} ${toY}`
          : `M ${fromX} ${fromY} C ${fromX} ${fromY + bend}, ${toX + (targetCenterX >= sourceCenterX ? -horizontalBias : horizontalBias)} ${toY - bend}, ${toX} ${toY}`;
        next.push({ id: edge.id, edge, d });
      });
      setConnectors(next);
    };

    measure();
    const handleResize = () => window.requestAnimationFrame(measure);
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, [data.edges, statuses]);

  return (
    <div style={canvasShell}>
      <div style={{ padding: 'var(--spacing-md)', overflow: 'auto' }}>
        <div
          ref={gridRef}
          style={{
            position: 'relative',
            display: 'grid',
            gridTemplateColumns: `${SWIMLANE_STEP_COLUMN_WIDTH}px repeat(${roles.length}, minmax(164px, 1fr))`,
            minWidth: `${SWIMLANE_STEP_COLUMN_WIDTH + roles.length * 176}px`,
            border: '1px solid var(--md-sys-color-outline-variant)',
            borderRadius: 'var(--radius-md)',
            overflow: 'hidden',
          }}
        >
          <svg style={swimlaneConnectorLayer} aria-hidden="true">
            {connectors.map((connector) => (
              <g key={connector.id}>
                <path
                  d={connector.d}
                  fill="none"
                  stroke="transparent"
                  strokeWidth={12}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  pointerEvents="stroke"
                  style={{ cursor: 'pointer' }}
                  onClick={() => onSelect({ kind: 'edge', edge: connector.edge })}
                  onMouseEnter={() => setHoveredEdgeID(connector.id)}
                  onMouseLeave={() => setHoveredEdgeID((current) => (current === connector.id ? null : current))}
                  aria-label={connector.edge.description || connector.edge.transitionId}
                />
                <path
                  d={connector.d}
                  fill="none"
                  stroke={edgeColor(connector.edge)}
                  strokeWidth={hoveredEdgeID === connector.id ? 6 : 4}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  opacity={hoveredEdgeID === connector.id ? 1 : 0.88}
                  filter={hoveredEdgeID === connector.id ? 'drop-shadow(0 0 5px rgba(46, 255, 140, 0.35))' : 'none'}
                  pointerEvents="none"
                />
              </g>
            ))}
          </svg>
          <div style={swimlaneHeaderIconCell}>
            <Footprints size={16} />
          </div>
          {roles.map((role) => (
            <div key={`role-${role}`} style={swimlaneRoleHeader(role)}>
              {role || 'Terminal'}
            </div>
          ))}
          {statuses.map((status, index) => (
            <React.Fragment key={`row-${status}-${index}`}>
              <div
                style={{
                  ...swimlaneStepHeader,
                  ...(index === statuses.length - 1 ? { borderBottom: '1px solid var(--md-sys-color-outline-variant)' } : null),
                }}
              >
                {index > 0 && <div style={swimlaneStepConnectorTop} />}
                <div style={swimlaneStepBadge(index === 0 || index === statuses.length - 1)}>{index + 1}</div>
                {index < statuses.length - 1 && <div style={swimlaneStepConnectorBottom} />}
              </div>
              {roles.map((role) => {
                const node = nodeByID.get(status);
                const ownsCell = (node?.role || 'terminal') === role;
                return (
                  <div key={`${role}-${status}-${index}`} style={swimlaneCell}>
                    {ownsCell && node && (
                      <button
                        ref={(element) => setNodeRef(status, element)}
                        type="button"
                        onClick={() => onSelect({ kind: 'node', node })}
                        onMouseEnter={() => setHoveredNodeStatus(status)}
                        onMouseLeave={() => setHoveredNodeStatus((current) => (current === status ? null : current))}
                        style={swimlaneNode(node, hoveredNodeStatus === status)}
                      >
                        <StatusChip status={status} truncate maxWidth="132px" />
                        <span style={swimlaneNodeMeta}>{node.category}</span>
                      </button>
                    )}
                  </div>
                );
              })}
            </React.Fragment>
          ))}
        </div>
        <TransitionStrip data={data} statuses={statuses} onSelect={onSelect} />
      </div>
    </div>
  );
}

function TransitionStrip({
  data,
  statuses,
  onSelect,
}: {
  data: WorkflowGraphType;
  statuses: string[];
  onSelect: (selection: DetailSelection) => void;
}) {
  const [hoveredEdgeID, setHoveredEdgeID] = useState<string | null>(null);
  const edges = statuses.slice(1).map((status, i) => findEdge(data.edges, statuses[i], status)).filter(Boolean) as GraphEdge[];
  return (
    <div style={{ marginTop: 'var(--spacing-md)', display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
      {edges.map((edge) => (
        <button
          key={edge.id}
          type="button"
          onClick={() => onSelect({ kind: 'edge', edge })}
          onMouseEnter={() => setHoveredEdgeID(edge.id)}
          onMouseLeave={() => setHoveredEdgeID((current) => (current === edge.id ? null : current))}
          style={transitionChip(edge, hoveredEdgeID === edge.id)}
        >
          <span>{compactTransitionName(edge.transitionId)}</span>
          {edge.guard && <span style={guardBadge}>{edge.guard}</span>}
        </button>
      ))}
    </div>
  );
}

function DetailsPanel({
  data,
  selection,
  detailMode,
  setDetailMode,
  onClose,
}: {
  data: WorkflowGraphType;
  selection: DetailSelection;
  detailMode: 'human' | 'engine';
  setDetailMode: (value: 'human' | 'engine') => void;
  onClose: () => void;
}) {
  return (
    <aside
      style={{
        border: '1px solid var(--md-sys-color-outline-variant)',
        borderRadius: 'var(--radius-md)',
        background: 'var(--md-sys-color-surface)',
        minHeight: 0,
        overflow: 'hidden',
        display: 'grid',
        gridTemplateRows: 'auto 1fr',
      }}
    >
      <div style={{ padding: 'var(--spacing-md)', borderBottom: '1px solid var(--md-sys-color-outline-variant)' }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '8px' }}>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--md-sys-color-on-surface-variant)' }}>
              <Info size={16} />
              <span style={{ fontSize: '0.75rem', textTransform: 'uppercase' }}>Details</span>
            </div>
            <div style={{ marginTop: '6px', fontWeight: 600 }}>
              {selection ? selectionTitle(selection) : 'Select a status or transition'}
            </div>
          </div>
          {selection && (
            <button type="button" aria-label="Close details" onClick={onClose} style={iconButton}>
              <X size={16} />
            </button>
          )}
        </div>
        <SegmentedControl
          value={detailMode}
          onChange={(v) => setDetailMode(v as 'human' | 'engine')}
          options={[
            { value: 'human', label: 'Human' },
            { value: 'engine', label: 'Engine' },
          ]}
        />
      </div>
      <div style={{ padding: 'var(--spacing-md)', overflowY: 'auto' }}>
        {!selection ? (
          <div style={emptyPanel}>
            Click a status, arrow, transition chip, or graph edge to inspect ownership, guards, required outputs, and YAML-derived engine fields.
          </div>
        ) : detailMode === 'engine' ? (
          <pre style={engineBlock}>{JSON.stringify(selection.kind === 'node' ? selection.node : selection.edge, null, 2)}</pre>
        ) : selection.kind === 'node' ? (
          <NodeDetails data={data} node={selection.node} />
        ) : (
          <EdgeDetails data={data} edge={selection.edge} />
        )}
      </div>
    </aside>
  );
}

function NodeDetails({ data, node }: { data: WorkflowGraphType; node: GraphNode }) {
  const incoming = data.edges.filter((e) => e.target === node.id);
  const outgoing = data.edges.filter((e) => e.source === node.id);
  return (
    <div style={detailStack}>
      <DetailRow label="Status"><StatusChip status={node.id} /></DetailRow>
      <DetailRow label="Role">{node.role || 'No worker role'}</DetailRow>
      <DetailRow label="Category">{node.category}</DetailRow>
      <DetailRow label="Terminal">{node.terminal ? 'Yes' : 'No'}</DetailRow>
      <DetailSection title="Next transitions">
        {outgoing.length === 0 ? <span style={mutedText}>None</span> : outgoing.map((e) => <TransitionLine key={e.id} edge={e} />)}
      </DetailSection>
      <DetailSection title="Incoming transitions">
        {incoming.length === 0 ? <span style={mutedText}>None</span> : incoming.map((e) => <TransitionLine key={e.id} edge={e} />)}
      </DetailSection>
    </div>
  );
}

function EdgeDetails({ data, edge }: { data: WorkflowGraphType; edge: GraphEdge }) {
  const guard = edge.guard ? data.guards?.[edge.guard] : undefined;
  return (
    <div style={detailStack}>
      <DetailRow label="Transition">{edge.transitionId}</DetailRow>
      <DetailRow label="From"><StatusChip status={edge.source} /></DetailRow>
      <DetailRow label="To"><StatusChip status={edge.target} /></DetailRow>
      <DetailRow label="Allowed roles">{edge.allowedRoles.join(', ') || 'none'}</DetailRow>
      <DetailRow label="Queues next">{edge.queuesNextRole || 'target role/default'}</DetailRow>
      {edge.description && <DetailRow label="Description">{edge.description}</DetailRow>}
      {edge.guard && (
        <DetailSection title={`Guard: ${edge.guard}`}>
          <div style={{ display: 'grid', gap: '8px' }}>
            {guard?.description && <div>{guard.description}</div>}
            {guard?.any_label && guard.any_label.length > 0 && <LabelList label="Any label" values={guard.any_label} />}
            {guard?.all_absent && guard.all_absent.length > 0 && <LabelList label="All absent" values={guard.all_absent} />}
          </div>
        </DetailSection>
      )}
      {edge.requiredOutputs !== undefined && (
        <DetailSection title="Required outputs">
          <OutputValue value={edge.requiredOutputs} />
        </DetailSection>
      )}
      {(edge.closeIssue || edge.reopenIssue || edge.terminalAfterTransition) && (
        <DetailSection title="Actions">
          <div style={{ display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
            {edge.terminalAfterTransition && <span style={actionBadge}>terminal</span>}
            {edge.closeIssue && <span style={actionBadge}>close issue</span>}
            {edge.reopenIssue && <span style={actionBadge}>reopen issue</span>}
          </div>
        </DetailSection>
      )}
    </div>
  );
}

function buildPathPresets(data: WorkflowGraphType): PathPreset[] {
  const presets: PathPreset[] = [];
  for (const [key, statuses] of Object.entries(data.canonicalPaths ?? {})) {
    presets.push({ id: `canonical:${key}`, label: prettyPresetName(key), source: 'canonical', statuses });
  }
  for (const type of data.issueTypes ?? []) {
    const statuses = normalizeIssueTypePath(data, type);
    if (statuses.length > 0) {
      presets.push({ id: `type:${type.name}`, label: `${prettyPresetName(type.name)} type`, source: 'issueType', statuses });
    }
  }
  if (presets.length === 0) {
    presets.push({ id: 'fallback:all', label: 'All statuses', source: 'fallback', statuses: data.nodes.map((n) => n.id) });
  }
  return uniquePresets(presets);
}

function normalizeIssueTypePath(data: WorkflowGraphType, type: WorkflowIssueType): string[] {
  if (!type.defaultPath || type.defaultPath.length === 0) return [];
  const path = [...type.defaultPath];
  if (data.defaultPathScope === 'post_intake' && !path.includes('status:new')) {
    path.unshift('status:new');
  }
  return path;
}

function uniquePresets(presets: PathPreset[]): PathPreset[] {
  const seen = new Set<string>();
  return presets.filter((preset) => {
    const key = preset.statuses.join('>');
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function filterEdges(
  data: WorkflowGraphType,
  path: string[],
  opts: { full: boolean; loops: boolean; exceptions: boolean; terminal: boolean }
) {
  if (opts.full) {
    return data.edges.filter((edge) => {
      if (edge.source === '__bootstrap__') return false;
      if (isLoop(edge) && !opts.loops) return false;
      if (isException(edge, data) && !opts.exceptions) return false;
      if (isTerminalControl(edge) && !opts.terminal) return false;
      return true;
    });
  }

  const pathPairs = new Set(path.slice(1).map((target, i) => `${path[i]}→${target}`));
  const edgeIDs = new Set<string>();
  const result: GraphEdge[] = [];
  for (const edge of data.edges) {
    const pair = `${edge.source}→${edge.target}`;
    const include =
      pathPairs.has(pair) ||
      (opts.loops && path.includes(edge.source) && path.includes(edge.target) && isLoop(edge)) ||
      (opts.exceptions && path.includes(edge.source) && isException(edge, data)) ||
      (opts.terminal && path.includes(edge.source) && isTerminalControl(edge));
    if (include && !edgeIDs.has(edge.id)) {
      edgeIDs.add(edge.id);
      result.push(edge);
    }
  }
  return result;
}

function findEdge(edges: GraphEdge[], source: string, target: string) {
  return edges.find((e) => e.source === source && e.target === target);
}

function orderedRoles(data: WorkflowGraphType, statuses: string[], nodeByID: Map<string, GraphNode>) {
  const statusRoles = new Set(statuses.map((s) => nodeByID.get(s)?.role || 'terminal'));
  const configured = data.roles && data.roles.length > 0 ? data.roles : ['po', 'architect', 'uidesigner', 'developer', 'reviewer', 'tester'];
  const ordered = configured.filter((role) => statusRoles.has(role));
  if (statusRoles.has('terminal')) ordered.push('terminal');
  for (const role of statusRoles) {
    if (!ordered.includes(role)) ordered.push(role);
  }
  return ordered;
}

function isLoop(edge: GraphEdge) {
  return edge.source === edge.target || edge.target === 'status:in-development' || edge.transitionId.includes('reject') || edge.transitionId.includes('request_changes') || edge.transitionId.includes('fail');
}

function isException(edge: GraphEdge, data?: WorkflowGraphType) {
  const target = data?.nodes.find((n) => n.id === edge.target);
  return target?.category === 'exception' || edge.transitionId.includes('block') || edge.transitionId.includes('resume');
}

function isTerminalControl(edge: GraphEdge) {
  return edge.target === 'status:rejected' || edge.transitionId.includes('reopen') || edge.closeIssue || edge.reopenIssue;
}

function isLoopOrException(edge: GraphEdge) {
  return isLoop(edge) || isException(edge) || isTerminalControl(edge);
}

function edgeColor(edge: GraphEdge) {
  if (edge.guard) return 'var(--color-neon-cyan)';
  if (isTerminalControl(edge)) return 'var(--status-color-terminal)';
  if (isException(edge)) return 'var(--status-color-blocked)';
  if (isLoop(edge)) return 'var(--color-neon-amber)';
  return 'var(--color-neon-green)';
}

function categoryColor(category: string) {
  return categoryColors[category] ?? 'var(--md-sys-color-on-surface)';
}

function roleColor(role?: string) {
  switch (role) {
    case 'po':
      return '#0b84c6';
    case 'architect':
      return '#8a5cf6';
    case 'uidesigner':
      return '#d9487d';
    case 'developer':
      return '#1f9d55';
    case 'reviewer':
      return '#d97706';
    case 'tester':
      return '#0f766e';
    case 'terminal':
      return 'var(--status-color-terminal)';
    default:
      return 'var(--md-sys-color-on-surface)';
  }
}

function workflowTitle(data: WorkflowGraphType) {
  if (data.key === 'lean') return 'Lean 3-role GitHub Issue Workflow';
  if (data.key === 'full') return 'Full 6-role GitHub Issue Workflow';
  return data.id.replace(/_/g, ' ');
}

function prettyStatus(status: string) {
  return status.replace(/^status:/, '').replace(/-/g, ' ');
}

function prettyPresetName(value: string) {
  return value.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

function compactTransitionName(value: string) {
  return value.replace(/^(po|developer|reviewer|tester|architect|ui)_/, '').replace(/_/g, ' ');
}

function transitionRole(edge: GraphEdge) {
  const id = edge.transitionId || '';
  if (id.startsWith('po_')) return 'po';
  if (id.startsWith('developer_')) return 'developer';
  if (id.startsWith('reviewer_')) return 'reviewer';
  if (id.startsWith('tester_')) return 'tester';
  if (id.startsWith('architect_')) return 'architect';
  if (id.startsWith('ui_') || id.startsWith('uidesigner_')) return 'uidesigner';
  if (edge.allowedRoles.length === 1) {
    return edge.allowedRoles[0];
  }
  return edge.queuesNextRole || edge.allowedRoles[0];
}

function selectionTitle(selection: DetailSelection) {
  if (!selection) return '';
  return selection.kind === 'node' ? prettyStatus(selection.node.id) : compactTransitionName(selection.edge.transitionId);
}

function OutputValue({ value }: { value: unknown }) {
  if (Array.isArray(value)) {
    return (
      <ul style={{ margin: 0, paddingLeft: '18px' }}>
        {value.map((item) => <li key={String(item)}>{String(item)}</li>)}
      </ul>
    );
  }
  if (value && typeof value === 'object') {
    return <pre style={engineBlock}>{JSON.stringify(value, null, 2)}</pre>;
  }
  return <span>{String(value)}</span>;
}

function TransitionLine({ edge }: { edge: GraphEdge }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: '8px', alignItems: 'center' }}>
      <span>{compactTransitionName(edge.transitionId)}</span>
      <StatusChip status={edge.target} />
    </div>
  );
}

function LabelList({ label, values }: { label: string; values: string[] }) {
  return (
    <div>
      <div style={{ ...mutedText, marginBottom: '4px' }}>{label}</div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
        {values.map((value) => <code key={value} style={smallCode}>{value}</code>)}
      </div>
    </div>
  );
}

function DetailSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <div style={detailSectionTitle}>{title}</div>
      <div>{children}</div>
    </section>
  );
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={detailSectionTitle}>{label}</div>
      <div>{children}</div>
    </div>
  );
}

function SelectControl({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <label style={{ display: 'grid', gap: '4px', fontSize: '0.75rem', color: 'var(--md-sys-color-on-surface-variant)' }}>
      {label}
      <select value={value} onChange={(e) => onChange(e.target.value)} style={selectStyle}>
        {options.map((opt) => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
      </select>
    </label>
  );
}

function SegmentedControl({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <div style={segmentedShell}>
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          onClick={() => onChange(opt.value)}
          style={segmentButton(value === opt.value)}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label style={toggleStyle}>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <ListFilter size={14} />
      {label}
    </label>
  );
}

const canvasShell: React.CSSProperties = {
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-md)',
  background: 'var(--md-sys-color-surface)',
  minHeight: 0,
  overflow: 'hidden',
};

const mutedText: React.CSSProperties = {
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.8125rem',
};

const smallCode: React.CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontSize: '0.75rem',
  color: 'var(--md-sys-color-on-surface-variant)',
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  padding: '2px 6px',
};

const selectStyle: React.CSSProperties = {
  height: '34px',
  minWidth: '220px',
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  background: 'var(--md-sys-color-surface)',
  color: 'var(--md-sys-color-on-surface)',
  padding: '0 10px',
};

const segmentedShell: React.CSSProperties = {
  display: 'flex',
  gap: '2px',
  padding: '3px',
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  background: 'var(--md-sys-color-surface-variant)',
};

const segmentButton = (active: boolean): React.CSSProperties => ({
  border: 0,
  borderRadius: 'var(--radius-sm)',
  padding: '7px 10px',
  color: active ? 'var(--md-sys-color-on-primary)' : 'var(--md-sys-color-on-surface-variant)',
  background: active ? 'var(--md-sys-color-primary)' : 'transparent',
  cursor: 'pointer',
  fontWeight: 600,
});

const toggleStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '6px',
  minHeight: '32px',
  padding: '0 10px',
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--md-sys-color-on-surface-variant)',
  background: 'var(--md-sys-color-surface)',
  fontSize: '0.8125rem',
};

const swimlaneHeaderCell: React.CSSProperties = {
  padding: '10px 12px',
  background: 'var(--md-sys-color-surface-variant)',
  borderRight: '1px solid var(--md-sys-color-outline-variant)',
  borderBottom: '1px solid var(--md-sys-color-outline-variant)',
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.75rem',
  fontWeight: 700,
  textTransform: 'uppercase',
};

const swimlaneHeaderIconCell: React.CSSProperties = {
  ...swimlaneHeaderCell,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
};

const swimlaneRoleHeader = (role: string): React.CSSProperties => ({
  ...swimlaneHeaderCell,
  color: roleColor(role),
  textTransform: 'none',
  fontSize: '0.875rem',
});

const swimlaneStepHeader: React.CSSProperties = {
  minHeight: '100px',
  padding: '0',
  borderRight: '1px solid var(--md-sys-color-outline-variant)',
  background: 'var(--md-sys-color-surface-variant)',
  position: 'relative',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  color: 'var(--md-sys-color-on-surface)',
  fontWeight: 600,
};

const swimlaneStepBadge = (filled: boolean): React.CSSProperties => ({
  position: 'relative',
  zIndex: 1,
  width: '17px',
  height: '17px',
  borderRadius: '50%',
  border: '1.5px solid var(--color-neon-amber)',
  background: filled ? 'var(--color-neon-amber)' : 'transparent',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  fontFamily: 'var(--font-mono)',
  fontSize: '0.5rem',
  fontWeight: 700,
  color: filled ? 'var(--md-sys-color-surface)' : 'var(--color-neon-amber)',
  flexShrink: 0,
});

const swimlaneStepConnectorTop: React.CSSProperties = {
  position: 'absolute',
  top: 0,
  bottom: 'calc(50% + 9.5px)',
  left: '50%',
  transform: 'translateX(-50%)',
  width: '1.5px',
  background: 'var(--color-neon-amber)',
};

const swimlaneStepConnectorBottom: React.CSSProperties = {
  position: 'absolute',
  top: 'calc(50% + 9.5px)',
  bottom: 0,
  left: '50%',
  transform: 'translateX(-50%)',
  width: '1.5px',
  background: 'var(--color-neon-amber)',
};

const swimlaneCell: React.CSSProperties = {
  minHeight: '100px',
  padding: '12px',
  borderRight: '1px solid var(--md-sys-color-outline-variant)',
  borderBottom: '1px solid var(--md-sys-color-outline-variant)',
  position: 'relative',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  zIndex: 5,
  pointerEvents: 'none',
};

const swimlaneNode = (node?: GraphNode, hovered = false): React.CSSProperties => ({
  width: '100%',
  minHeight: '72px',
  border: `1px solid ${node ? roleColor(node.role) : 'var(--md-sys-color-outline-variant)'}`,
  borderTop: `4px solid ${node ? roleColor(node.role) : 'var(--md-sys-color-outline-variant)'}`,
  borderRadius: 'var(--radius-sm)',
  background: 'var(--md-sys-color-surface)',
  color: 'var(--md-sys-color-on-surface)',
  display: 'grid',
  gap: '6px',
  alignContent: 'center',
  padding: '10px 12px',
  cursor: node ? 'pointer' : 'default',
  textAlign: 'left',
  position: 'relative',
  zIndex: 8,
  pointerEvents: 'auto',
  transition: 'transform 120ms ease, box-shadow 120ms ease, background-color 120ms ease, border-color 120ms ease',
  transform: hovered ? 'translateY(-1px)' : 'none',
  boxShadow: hovered ? '0 6px 16px rgba(0, 0, 0, 0.14)' : 'none',
  backgroundColor: hovered ? 'var(--md-sys-color-surface-variant)' : 'var(--md-sys-color-surface)',
});

const swimlaneNodeMeta: React.CSSProperties = {
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.75rem',
  textTransform: 'uppercase',
};

const swimlaneConnectorLayer: React.CSSProperties = {
  position: 'absolute',
  inset: 0,
  width: '100%',
  height: '100%',
  overflow: 'visible',
  zIndex: 4,
};

const pathStack: React.CSSProperties = {
  display: 'grid',
  gap: '8px',
  width: 'min(100%, 960px)',
  margin: '0 auto',
};

const pathSeparator = (edge?: GraphEdge): React.CSSProperties => ({
  width: '100%',
  border: 0,
  background: 'transparent',
  color: edge ? edgeColor(edge) : 'var(--md-sys-color-outline)',
  display: 'grid',
  alignContent: 'center',
  justifyItems: 'center',
  gap: '4px',
  cursor: edge ? 'pointer' : 'default',
});

const pathNode = (node?: GraphNode): React.CSSProperties => ({
  width: '100%',
  minHeight: '86px',
  border: `1px solid ${node ? categoryColor(node.category) : 'var(--md-sys-color-outline-variant)'}`,
  borderTop: `4px solid ${node ? categoryColor(node.category) : 'var(--md-sys-color-outline-variant)'}`,
  borderRadius: 'var(--radius-sm)',
  background: 'var(--md-sys-color-surface)',
  color: 'var(--md-sys-color-on-surface)',
  display: 'grid',
  gap: '6px',
  alignContent: 'center',
  padding: '12px 14px',
  cursor: 'pointer',
  textAlign: 'left',
});

const pathNodeHeader: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: '12px',
  fontWeight: 600,
};

const pathNodeMeta: React.CSSProperties = {
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.75rem',
};

const pathNodeSummary: React.CSSProperties = {
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.75rem',
  textTransform: 'uppercase',
  letterSpacing: 0,
};

const guardBadge: React.CSSProperties = {
  border: '1px solid rgba(0, 229, 255, 0.32)',
  borderRadius: 'var(--radius-sm)',
  padding: '1px 5px',
  color: 'var(--color-neon-cyan)',
  fontSize: '0.625rem',
  fontFamily: 'var(--font-mono)',
};

const transitionChip = (edge: GraphEdge, hovered = false): React.CSSProperties => {
  const color = roleColor(transitionRole(edge));
  return {
    border: `1px solid ${color}`,
    borderRadius: 'var(--radius-sm)',
    background: hovered ? `${color}18` : 'var(--md-sys-color-surface)',
    color,
    display: 'flex',
    alignItems: 'center',
    gap: '6px',
    padding: '7px 9px',
    cursor: 'pointer',
    transition: 'background-color 120ms ease, color 120ms ease, box-shadow 120ms ease, transform 120ms ease',
    boxShadow: hovered ? `0 6px 16px ${color}20` : 'none',
    transform: hovered ? 'translateY(-1px)' : 'none',
  };
};

const iconButton: React.CSSProperties = {
  width: '32px',
  height: '32px',
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  background: 'transparent',
  color: 'var(--md-sys-color-on-surface-variant)',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  cursor: 'pointer',
};

const emptyPanel: React.CSSProperties = {
  color: 'var(--md-sys-color-on-surface-variant)',
  lineHeight: 1.6,
};

const detailStack: React.CSSProperties = {
  display: 'grid',
  gap: 'var(--spacing-md)',
};

const detailSectionTitle: React.CSSProperties = {
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.6875rem',
  fontWeight: 700,
  textTransform: 'uppercase',
  marginBottom: '5px',
};

const engineBlock: React.CSSProperties = {
  margin: 0,
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
  fontFamily: 'var(--font-mono)',
  fontSize: '0.75rem',
  color: 'var(--md-sys-color-on-surface)',
  background: 'var(--md-sys-color-surface-variant)',
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  padding: '10px',
};

const actionBadge: React.CSSProperties = {
  border: '1px solid var(--status-color-terminal)',
  color: 'var(--status-color-terminal)',
  borderRadius: 'var(--radius-sm)',
  padding: '3px 7px',
  fontSize: '0.75rem',
};
