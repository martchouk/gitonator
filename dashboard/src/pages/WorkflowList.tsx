
import { Link } from 'react-router-dom';
import useSWR from 'swr';
import type { WorkflowSummary } from '../api/types';
import { get } from '../api/client';

interface WorkflowListResponse {
  workflows: WorkflowSummary[];
}

export function WorkflowList() {
  const { data, error } = useSWR<WorkflowListResponse>(
    '/api/v1/workflows',
    (url: string) => get<WorkflowListResponse>(url)
  );

  const workflows = data?.workflows ?? [];

  return (
    <div>
      <h2 style={{ margin: '0 0 var(--spacing-lg)', fontWeight: 400 }}>Workflows</h2>

      {error && (
        <p style={{ color: 'var(--md-sys-color-error)' }}>
          Failed to load workflows: {error.message}
        </p>
      )}

      {!data && !error && (
        <p style={{ color: 'var(--md-sys-color-on-surface-variant)' }}>Loading…</p>
      )}

      {data && workflows.length === 0 && (
        <p style={{ color: 'var(--md-sys-color-on-surface-variant)' }}>
          No workflow definitions found. Ensure{' '}
          <code style={{ fontFamily: "'Roboto Mono', monospace" }}>workflows/*.yaml</code> files are
          present and the server is running.
        </p>
      )}

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
          gap: 'var(--spacing-lg)',
        }}
      >
        {workflows.map((wf) => (
          <div
            key={wf.key}
            style={{
              border: '1px solid var(--md-sys-color-outline-variant)',
              borderRadius: 'var(--md-shape-medium)',
              padding: 'var(--spacing-lg)',
              background: 'var(--md-sys-color-surface)',
              display: 'flex',
              flexDirection: 'column',
              gap: 'var(--spacing-sm)',
              transition: 'box-shadow 150ms ease',
            }}
          >
            <div
              style={{
                fontWeight: 500,
                fontSize: '1rem',
                color: 'var(--md-sys-color-on-surface)',
              }}
            >
              {wf.id}
            </div>
            <div
              style={{
                fontSize: '0.875rem',
                color: 'var(--md-sys-color-on-surface-variant)',
              }}
            >
              key: <code style={{ fontFamily: "'Roboto Mono', monospace" }}>{wf.key}</code>
            </div>
            <div style={{ display: 'flex', gap: 'var(--spacing-sm)', flexWrap: 'wrap' }}>
              <Chip label={`${wf.statusCount} statuses`} />
              <Chip label={`${wf.edgeCount} transitions`} />
            </div>
            <div style={{ marginTop: 'auto', paddingTop: 'var(--spacing-sm)' }}>
              <Link
                to={`/workflows/${wf.key}`}
                style={{
                  color: 'var(--md-sys-color-primary)',
                  textDecoration: 'none',
                  fontSize: '0.875rem',
                  fontWeight: 500,
                }}
              >
                View Graph →
              </Link>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Chip({ label }: { label: string }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 'var(--md-shape-small)',
        border: '1px solid var(--md-sys-color-outline-variant)',
        fontSize: '0.6875rem',
        fontWeight: 500,
        color: 'var(--md-sys-color-on-surface-variant)',
      }}
    >
      {label}
    </span>
  );
}
