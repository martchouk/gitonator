
import { Handle, Position } from 'reactflow';

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

export interface WorkflowNodeData {
  label: string;
  role: string;
  category: string;
  terminal: boolean;
}

export function WorkflowNode({ data, selected }: { data: WorkflowNodeData; selected?: boolean }) {
  const borderColor = selected
    ? 'var(--md-sys-color-primary)'
    : (categoryColors[data.category] ?? categoryColors.terminal);

  const label = data.label.startsWith('status:') ? data.label.slice(7) : data.label;

  return (
    <div
      style={{
        minWidth: '160px',
        padding: '8px 16px',
        borderRadius: 'var(--md-shape-small)',
        background: 'var(--md-sys-color-surface-variant)',
        border: `2px solid ${borderColor}`,
        borderLeft: `4px solid ${borderColor}`,
        boxShadow: selected ? `0 0 0 2px var(--md-sys-color-primary)` : undefined,
      }}
    >
      {/* Role chip */}
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
            lineHeight: 1.45,
          }}
        >
          {data.role}
        </div>
      )}
      {/* Status label */}
      <div
        style={{
          fontFamily: 'var(--font-sans)',
          fontSize: '0.875rem',
          fontWeight: 500,
          color: 'var(--md-sys-color-on-surface)',
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          maxWidth: '180px',
        }}
        title={data.label}
      >
        {label}
      </div>
      {data.terminal && (
        <div
          style={{
            fontSize: '0.6875rem',
            color: 'var(--md-sys-color-on-surface-variant)',
            marginTop: '2px',
          }}
        >
          terminal
        </div>
      )}
      <Handle
        type="target"
        position={Position.Top}
        style={{ background: 'var(--md-sys-color-outline)' }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        style={{ background: 'var(--md-sys-color-outline)' }}
      />
    </div>
  );
}
