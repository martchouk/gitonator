import { ChevronRight, GitBranch, Route, Workflow } from 'lucide-react';
import { Link } from 'react-router-dom';
import useSWR from 'swr';

import type { WorkflowSummary } from '../api/types';
import { get } from '../api/client';
import { ErrorBanner } from './LiveView';

interface WorkflowListResponse {
  workflows: WorkflowSummary[];
}

export function WorkflowList() {
  const { data, error, mutate } = useSWR<WorkflowListResponse>(
    '/api/v1/workflows',
    (url: string) => get<WorkflowListResponse>(url)
  );

  const workflows = data?.workflows ?? [];

  return (
    <div>
      <h2 className="page-title">Workflows</h2>

      {error && <ErrorBanner onRetry={() => void mutate()} />}

      {!data && !error && (
        <p style={{ color: 'var(--md-sys-color-on-surface-variant)' }}>Loading…</p>
      )}

      {data && workflows.length === 0 && (
        <p style={{ color: 'var(--md-sys-color-on-surface-variant)' }}>
          No workflow definitions found. Ensure{' '}
          <code style={{ fontFamily: 'var(--font-mono)' }}>workflows/*.yaml</code> files are
          present and the server is running.
        </p>
      )}

      {!error && (
        <div style={workflowGrid}>
          {workflows.map((wf) => (
            <Link key={wf.key} to={`/workflows/${wf.key}`} style={workflowCardLink}>
              <article style={workflowCard}>
                <header style={workflowCardHeader}>
                  <div style={workflowCardTitleBlock}>
                    <span style={workflowKeyChip}>{wf.key}</span>
                    <h3 style={workflowCardTitle}>{workflowDisplayName(wf)}</h3>
                    <p style={workflowCardSubtitle}>
                      {wf.description || prettyWorkflowID(wf.id)}
                    </p>
                  </div>
                  <div style={workflowIconShell}>
                    <WorkflowCardIcon workflowKey={wf.key} />
                  </div>
                </header>

                <section style={metricsGrid}>
                  <MetricStat value={wf.statusCount} label="Statuses" accent="var(--color-neon-green)" />
                  <MetricStat value={wf.edgeCount} label="Transitions" accent="var(--color-neon-cyan)" />
                  <MetricStat value={wf.roleCount} label="Roles" accent="var(--color-neon-amber)" />
                  <MetricStat value={wf.issueTypeCount} label="Issue types" accent="var(--color-neon-magenta)" />
                </section>

                <footer style={workflowFooter}>
                  <div style={metaRow}>
                    <MetaChip label={prettyWorkflowMode(wf.key)} />
                    <MetaChip label={prettyWorkflowID(wf.id)} mono />
                  </div>
                  <div style={openAction}>
                    <span>Open workflow</span>
                    <ChevronRight size={16} />
                  </div>
                </footer>
              </article>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

function WorkflowCardIcon({ workflowKey }: { workflowKey: string }) {
  if (workflowKey === 'lean') {
    return <Route size={20} />;
  }
  if (workflowKey === 'full') {
    return <Workflow size={20} />;
  }
  return <GitBranch size={20} />;
}

function MetricStat({
  value,
  label,
  accent,
}: {
  value: number;
  label: string;
  accent: string;
}) {
  return (
    <div style={metricCell}>
      <span style={{ ...metricValue, color: accent }}>{value}</span>
      <span style={metricLabel}>{label}</span>
    </div>
  );
}

function MetaChip({ label, mono = false }: { label: string; mono?: boolean }) {
  return (
    <span
      style={{
        ...metaChip,
        ...(mono ? { fontFamily: 'var(--font-mono)' } : null),
      }}
    >
      {label}
    </span>
  );
}

function workflowDisplayName(wf: WorkflowSummary) {
  if (wf.key === 'lean') return 'Lean Issue Workflow';
  if (wf.key === 'full') return 'Full Issue Workflow';
  return prettyWorkflowID(wf.id);
}

function prettyWorkflowMode(value: string) {
  if (value === 'lean') return 'Lean mode';
  if (value === 'full') return 'Full mode';
  return value.replace(/_/g, ' ');
}

function prettyWorkflowID(value: string) {
  return value
    .replace(/^workflow[_-]?/i, '')
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

const workflowGrid: React.CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
  gap: 'var(--spacing-lg)',
  alignItems: 'stretch',
};

const workflowCardLink: React.CSSProperties = {
  textDecoration: 'none',
  color: 'inherit',
};

const workflowCard: React.CSSProperties = {
  height: '100%',
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  padding: '18px',
  background: 'var(--md-sys-color-surface)',
  display: 'grid',
  gap: '16px',
  gridTemplateRows: 'auto auto 1fr',
  transition: 'transform 140ms ease, box-shadow 140ms ease, border-color 140ms ease, background-color 140ms ease',
  boxShadow: '0 1px 0 rgba(0, 0, 0, 0.02)',
};

const workflowCardHeader: React.CSSProperties = {
  display: 'flex',
  alignItems: 'flex-start',
  justifyContent: 'space-between',
  gap: '16px',
};

const workflowCardTitleBlock: React.CSSProperties = {
  minWidth: 0,
  display: 'grid',
  gap: '8px',
};

const workflowKeyChip: React.CSSProperties = {
  justifySelf: 'start',
  padding: '2px 7px',
  borderRadius: 'var(--radius-sm)',
  border: '1px solid var(--md-sys-color-outline-variant)',
  color: 'var(--md-sys-color-on-surface-variant)',
  fontFamily: 'var(--font-mono)',
  fontSize: '0.6875rem',
  lineHeight: 1.2,
};

const workflowCardTitle: React.CSSProperties = {
  margin: 0,
  fontSize: '1.0625rem',
  lineHeight: 1.2,
  fontWeight: 600,
  color: 'var(--md-sys-color-on-surface)',
};

const workflowCardSubtitle: React.CSSProperties = {
  margin: 0,
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.8125rem',
  lineHeight: 1.5,
};

const workflowIconShell: React.CSSProperties = {
  width: '40px',
  height: '40px',
  borderRadius: 'var(--radius-sm)',
  border: '1px solid var(--md-sys-color-outline-variant)',
  background: 'var(--md-sys-color-surface-variant)',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  color: 'var(--workflow-title-accent)',
  flexShrink: 0,
};

const metricsGrid: React.CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
  gap: '10px',
};

const metricCell: React.CSSProperties = {
  border: '1px solid var(--md-sys-color-outline-variant)',
  borderRadius: 'var(--radius-sm)',
  background: 'var(--md-sys-color-surface-variant)',
  padding: '12px 12px 10px',
  display: 'grid',
  gap: '4px',
  minHeight: '72px',
  alignContent: 'center',
};

const metricValue: React.CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontSize: '1.25rem',
  fontWeight: 700,
  lineHeight: 1,
};

const metricLabel: React.CSSProperties = {
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.6875rem',
  textTransform: 'uppercase',
  lineHeight: 1.2,
};

const workflowFooter: React.CSSProperties = {
  display: 'grid',
  gap: '14px',
  alignContent: 'end',
};

const metaRow: React.CSSProperties = {
  display: 'flex',
  gap: '8px',
  flexWrap: 'wrap',
};

const metaChip: React.CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  padding: '3px 8px',
  borderRadius: 'var(--radius-sm)',
  border: '1px solid var(--md-sys-color-outline-variant)',
  color: 'var(--md-sys-color-on-surface-variant)',
  fontSize: '0.6875rem',
  lineHeight: 1.2,
  background: 'var(--md-sys-color-surface)',
};

const openAction: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: '8px',
  paddingTop: '12px',
  borderTop: '1px solid var(--md-sys-color-outline-variant)',
  color: 'var(--workflow-title-accent)',
  fontSize: '0.8125rem',
  fontWeight: 600,
};
