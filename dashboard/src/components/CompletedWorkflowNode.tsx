import { Handle, Position } from 'reactflow';
import type { WorkflowNodeData } from './WorkflowNode';

const categoryColors: Record<string, string> = {
  intake: 'var(--status-color-intake)',
  design: 'var(--status-color-design)',
  requirements: 'var(--status-color-requirements)',
  planning: 'var(--status-color-planning)',
  implementation: 'var(--status-color-implementation)',
  review: 'var(--status-color-review)',
  acceptance: 'var(--status-color-acceptance)',
  terminal: 'var(--status-color-terminal)',
  exception: 'var(--status-color-blocked)',
};

export interface CompletedWorkflowNodeData extends WorkflowNodeData {
  visited: boolean;
  isFinal: boolean;
  lastActor?: string;
}

export function CompletedWorkflowNode({
  data,
  selected,
}: {
  data: CompletedWorkflowNodeData;
  selected?: boolean;
}) {
  const color = categoryColors[data.category] ?? categoryColors.terminal;
  const visited = data.visited || data.isFinal;

  const borderColor = selected
    ? 'var(--md-sys-color-primary)'
    : visited
    ? color
    : 'var(--md-sys-color-outline-variant)';

  const boxShadow = data.isFinal
    ? `0 0 18px ${color}90, 0 0 6px ${color}60`
    : visited
    ? `0 0 10px ${color}55`
    : 'none';

  const label = data.label.startsWith('status:') ? data.label.slice(7) : data.label;

  return (
    <div
      style={{
        minWidth: '180px',
        padding: '8px 14px',
        borderRadius: 'var(--md-shape-small)',
        background: data.isFinal
          ? 'var(--md-sys-color-primary-container)'
          : 'var(--md-sys-color-surface-variant)',
        border: `2px solid ${borderColor}`,
        borderLeft: `4px solid ${borderColor}`,
        boxShadow,
        opacity: visited ? 1 : 0.28,
        transition: 'opacity 200ms ease, box-shadow 200ms ease',
      }}
    >
      {data.role && (
        <div
          style={{
            display: 'inline-block',
            marginBottom: '4px',
            padding: '1px 8px',
            borderRadius: 'var(--md-shape-small)',
            background: 'var(--md-sys-color-secondary-container)',
            color: 'var(--md-sys-color-on-secondary-container)',
            fontSize: '0.6875rem',
            fontWeight: 500,
          }}
        >
          {data.role}
        </div>
      )}

      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontSize: '0.875rem',
          fontWeight: 500,
          color: visited
            ? 'var(--md-sys-color-on-surface)'
            : 'var(--md-sys-color-on-surface-variant)',
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          maxWidth: '200px',
        }}
        title={data.label}
      >
        {label}
      </div>

      {data.isFinal && (
        <div style={{ fontSize: '0.6875rem', color, fontWeight: 600, marginTop: '2px' }}>
          ✓ final
        </div>
      )}

      {visited && !data.isFinal && data.lastActor && (
        <div
          style={{
            fontSize: '0.6875rem',
            color: 'var(--md-sys-color-on-surface-variant)',
            marginTop: '2px',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {data.lastActor}
        </div>
      )}

      <Handle
        type="target"
        position={Position.Top}
        style={{ background: visited ? color : 'var(--md-sys-color-outline)' }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        style={{ background: visited ? color : 'var(--md-sys-color-outline)' }}
      />
    </div>
  );
}
