import React, { useState } from 'react';
import { Loader2, CheckCircle2, ChevronDown, ChevronRight } from 'lucide-react';
import { ErrorBanner } from './LiveView';
import useSWR from 'swr';
import type { CompletedIssueSummary, CompletedRunDetail, TaskRow } from '../api/types';
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

function duration(
  created: string,
  finished: { String: string; Valid: boolean } | null | undefined
): string {
  if (!finished?.Valid || !finished.String) return '–';
  const ms = new Date(finished.String).getTime() - new Date(created).getTime();
  if (ms < 0) return '–';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

function nullStr(f: { String: string; Valid: boolean } | null | undefined): string {
  if (!f) return '–';
  return f.Valid && f.String ? f.String : '–';
}

function taskOutcomeColor(status: string): string {
  if (status === 'completed') return 'var(--color-neon-green)';
  if (status === 'failed') return 'var(--color-neon-magenta)';
  if (status === 'superseded') return 'var(--color-text-muted)';
  return 'var(--color-text-muted)';
}

// 8 columns: step | role | status | outcome | assigned to | bridge | created | duration
const SUB_COLS = '40px 90px minmax(0,1fr) 100px 120px 90px 78px 74px';

function SubTableHeader() {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: SUB_COLS,
        padding: '6px 16px',
        background: 'var(--md-sys-color-surface)',
        fontSize: '0.625rem',
        fontWeight: 700,
        letterSpacing: '0.08em',
        textTransform: 'uppercase',
        color: 'var(--color-text-muted)',
        borderBottom: '1px solid var(--md-sys-color-outline-variant)',
      }}
    >
      <span>#</span>
      <span>Role</span>
      <span>Status at Dispatch</span>
      <span>Outcome</span>
      <span>Assigned to</span>
      <span>Bridge ID</span>
      <span>Created</span>
      <span>Duration</span>
    </div>
  );
}

function SubTaskRow({ task, step }: { task: TaskRow; step: number }) {
  const bridge = nullStr(task.bridge_id);
  // Prefer GitHub-authenticated commenter (claimed_by) over issue assignee over role.
  const workerLogin = (task.claimed_by?.Valid && task.claimed_by?.String)
    ? task.claimed_by.String
    : task.assignee || null;

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: SUB_COLS,
        padding: '11px 16px',
        alignItems: 'center',
        borderBottom: '1px solid var(--md-sys-color-outline-variant)',
        fontSize: '0.8125rem',
      }}
    >
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-neon-amber)' }}>
        {step}
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-neon-cyan)' }}>
        {task.role || '–'}
      </span>
      <span style={{ overflow: 'hidden' }}>
        {task.current_status
          ? <StatusChip status={task.current_status} />
          : <span style={{ color: 'var(--color-text-muted)' }}>–</span>
        }
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', fontWeight: 600, color: taskOutcomeColor(task.status) }}>
        {task.status}
      </span>
      {/* Assigned to: real worker (claimed_by) in cyan, role fallback in yellow */}
      <span style={{
        fontFamily: 'var(--font-mono)',
        fontSize: '0.6875rem',
        color: workerLogin ? 'var(--color-neon-cyan)' : 'var(--color-neon-yellow)',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
      }}>
        {workerLogin || task.role || '–'}
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', opacity: bridge !== '–' ? 1 : 0.4 }}>
        {bridge}
      </span>
      <span style={{ fontSize: '0.6875rem', color: 'var(--color-text-muted)' }}>
        {relativeTime(task.created_at)}
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-text-muted)' }}>
        {duration(task.created_at, task.finished_at)}
      </span>
    </div>
  );
}

function ExpandedDetail({ issueNumber }: { issueNumber: number }) {
  const { data, error, mutate } = useSWR<CompletedRunDetail>(
    `/api/v1/dashboard/completed/${issueNumber}`,
    (url: string) => get<CompletedRunDetail>(url)
  );

  if (error) {
    return <div style={{ padding: '8px 16px' }}><ErrorBanner onRetry={() => void mutate()} /></div>;
  }

  if (!data) {
    return (
      <div style={{ padding: '14px 16px', display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>
        <Loader2 size={16} style={{ animation: 'spin 1.2s linear infinite' }} />
        Loading tasks…
      </div>
    );
  }

  // Tasks come DESC from API; reverse to chronological (step 1, 2, 3…)
  const tasks = [...data.tasks].reverse();

  if (tasks.length === 0) {
    return (
      <div style={{ padding: '12px 16px', color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>
        No task records found.
      </div>
    );
  }

  return (
    <div>
      <SubTableHeader />
      {tasks.map((task, idx) => (
        <SubTaskRow key={task.id} task={task} step={idx + 1} />
      ))}
    </div>
  );
}

export function CompletedList() {
  const { data, error, mutate } = useSWR<CompletedListResponse>(
    '/api/v1/dashboard/completed',
    (url: string) => get<CompletedListResponse>(url),
    { refreshInterval: 60000 }
  );

  const [expandedIssue, setExpandedIssue] = useState<number | null>(null);

  const runs = data?.completed ?? [];

  return (
    <div>
      <h2 className="page-title">Completed Runs</h2>

      {error && <ErrorBanner onRetry={() => void mutate()} />}

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
          {/* Table header */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '28px 80px 1fr 160px 160px 80px 110px',
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
            <span />
            <span>Issue #</span>
            <span>Repo</span>
            <span>Final Status</span>
            <span>Workflow</span>
            <span>Steps</span>
            <span>Completed</span>
          </div>

          {runs.map((run) => {
            const isExpanded = expandedIssue === run.issueNumber;
            return (
              <React.Fragment key={run.issueNumber}>
                {/* Summary row */}
                <div
                  role="row"
                  tabIndex={0}
                  onClick={() => setExpandedIssue(isExpanded ? null : run.issueNumber)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ')
                      setExpandedIssue(isExpanded ? null : run.issueNumber);
                  }}
                  className="completed-row"
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '28px 80px 1fr 160px 160px 80px 110px',
                    padding: '11px 16px',
                    alignItems: 'center',
                    borderBottom: isExpanded
                      ? 'none'
                      : '1px solid var(--md-sys-color-outline-variant)',
                    cursor: 'pointer',
                    background: isExpanded
                      ? 'var(--md-sys-color-primary-container)'
                      : 'var(--md-sys-color-surface)',
                    borderLeft: isExpanded
                      ? '4px solid var(--md-sys-color-primary)'
                      : '4px solid transparent',
                    transition: 'background 150ms ease',
                    outline: 'none',
                  }}
                >
                  <span style={{ color: 'var(--color-text-muted)', display: 'flex', alignItems: 'center' }}>
                    {isExpanded
                      ? <ChevronDown size={14} />
                      : <ChevronRight size={14} />
                    }
                  </span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-neon-amber)' }}>
                    #{run.issueNumber}
                  </span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {run.repo || '–'}
                  </span>
                  <span>
                    <StatusChip status={run.finalStatus} />
                  </span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-neon-cyan)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {run.workflowKey || '–'}
                  </span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-neon-amber)' }}>
                    {run.stepCount}
                  </span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                    {relativeTime(run.completedAt)}
                  </span>
                </div>

                {/* Expanded sub-table */}
                {isExpanded && (
                  <div
                    style={{
                      borderLeft: '4px solid var(--md-sys-color-primary)',
                      borderBottom: '1px solid var(--md-sys-color-outline-variant)',
                      background: 'var(--md-sys-color-surface)',
                    }}
                  >
                    <ExpandedDetail issueNumber={run.issueNumber} />
                  </div>
                )}
              </React.Fragment>
            );
          })}
        </div>
      )}

      <style>{`
        .completed-row:hover { background: var(--color-primary-subtle) !important; }
      `}</style>
    </div>
  );
}
