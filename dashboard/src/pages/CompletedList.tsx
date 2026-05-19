import { Link } from 'react-router-dom';
import { Loader2, CheckCircle2 } from 'lucide-react';
import useSWR from 'swr';
import type { CompletedIssueSummary } from '../api/types';
import { get } from '../api/client';
import { StatusChip } from '../components/StatusChip';

interface CompletedListResponse {
  completed: CompletedIssueSummary[];
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

export function CompletedList() {
  const { data, error } = useSWR<CompletedListResponse>(
    '/api/v1/dashboard/completed',
    (url: string) => get<CompletedListResponse>(url),
    { refreshInterval: 60000 }
  );

  const runs = data?.completed ?? [];

  return (
    <div>
      <h2
        style={{
          margin: '0 0 var(--spacing-lg)',
          fontWeight: 600,
          fontSize: '1.75rem',
          letterSpacing: '-0.02em',
        }}
      >
        Completed Runs
      </h2>

      {error && (
        <p style={{ color: 'var(--md-sys-color-error)' }}>
          Failed to load: {error.message}
        </p>
      )}

      {!data && !error && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 'var(--spacing-2xl)',
            color: 'var(--color-text-muted)',
            gap: 'var(--spacing-sm)',
          }}
        >
          <Loader2 size={40} style={{ opacity: 0.5, animation: 'spin 1.2s linear infinite' }} />
          <span>Loading…</span>
        </div>
      )}

      {data && runs.length === 0 && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 'var(--spacing-2xl)',
            color: 'var(--color-text-muted)',
            gap: 'var(--spacing-sm)',
          }}
        >
          <CheckCircle2 size={40} style={{ opacity: 0.4 }} />
          <span style={{ fontWeight: 500 }}>No completed workflows yet</span>
          <span style={{ fontSize: '0.875rem' }}>
            Issues that reach a terminal status will appear here.
          </span>
        </div>
      )}

      {runs.length > 0 && (
        <div
          style={{
            border: '1px solid var(--md-sys-color-outline-variant)',
            borderRadius: 'var(--md-shape-medium)',
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '80px 1fr 160px 160px 80px 110px',
              padding: '10px 16px',
              background: 'var(--md-sys-color-surface-variant)',
              fontSize: '0.6875rem',
              fontWeight: 600,
              letterSpacing: '0.06em',
              textTransform: 'uppercase',
              color: 'var(--color-text-muted)',
              borderBottom: '1px solid var(--md-sys-color-outline-variant)',
            }}
          >
            <span>#</span>
            <span>Repo</span>
            <span>Final Status</span>
            <span>Workflow</span>
            <span>Steps</span>
            <span>Completed</span>
          </div>

          {runs.map((run) => (
            <Link
              key={run.issueNumber}
              to={`/completed/${run.issueNumber}`}
              className="completed-row"
              style={{
                display: 'grid',
                gridTemplateColumns: '80px 1fr 160px 160px 80px 110px',
                padding: '11px 16px',
                alignItems: 'center',
                borderBottom: '1px solid var(--md-sys-color-outline-variant)',
                textDecoration: 'none',
                color: 'var(--color-text)',
                background: 'var(--md-sys-color-surface)',
                transition: 'background 150ms ease',
              }}
            >
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: '0.875rem',
                  color: 'var(--md-sys-color-primary)',
                }}
              >
                #{run.issueNumber}
              </span>
              <span
                style={{
                  fontSize: '0.875rem',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {run.repo || '–'}
              </span>
              <span>
                <StatusChip status={run.finalStatus} />
              </span>
              <span
                style={{
                  fontSize: '0.75rem',
                  color: 'var(--color-text-muted)',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  fontFamily: 'var(--font-mono)',
                }}
              >
                {run.workflowKey || '–'}
              </span>
              <span style={{ fontSize: '0.875rem', color: 'var(--color-text-muted)' }}>
                {run.stepCount}
              </span>
              <span style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                {relativeTime(run.completedAt)}
              </span>
            </Link>
          ))}
        </div>
      )}

      <style>{`
        .completed-row:last-child { border-bottom: none !important; }
        .completed-row:hover { background: var(--color-primary-subtle) !important; }
      `}</style>
    </div>
  );
}
