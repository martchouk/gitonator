import React, { useState, useEffect } from 'react';
import { Zap, RotateCcw, Loader2, ClipboardList, ChevronDown, ChevronRight } from 'lucide-react';
import useSWR from 'swr';
import type { Issue, TaskRow } from '../api/types';
import { get } from '../api/client';
import { StatusChip } from '../components/StatusChip';
import { useSSE } from '../hooks/useSSE';

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

interface IssueResponse {
  issues: Issue[];
}

interface IssueDetailResponse {
  number: number;
  tasks: TaskRow[];
}

const RUNNING_WORDS = [
  'hyperspacing…', 'bebopping…',   'determining…', 'hammering…',
  'wibbling…',     'pondering…',   'cogitating…',  'calibrating…',
  'reticulating…', 'manifolding…', 'fluxing…',     'spooling…',
  'recursing…',    'triangulating…','defragging…',  'synergising…',
  'oscillating…',  'fuzzing…',     'caffeinating…','processing…',
  'noodling…',     'wobbling…',    'crunching…',   'compiling…',
];

const WORD_COLORS = [
  'var(--color-neon-green)',
  'var(--color-neon-cyan)',
  'var(--color-neon-amber)',
  'var(--color-neon-magenta)',
  'var(--color-neon-yellow)',
  'var(--color-neon-cyan)',
  'var(--color-neon-green)',
  'var(--color-neon-amber)',
  'var(--color-neon-magenta)',
  'var(--color-neon-yellow)',
  'var(--color-neon-cyan)',
  'var(--color-neon-green)',
];

function RotatingWord() {
  const [idx, setIdx] = useState(() => Math.floor(Math.random() * RUNNING_WORDS.length));
  return (
    <span
      key={idx}
      onAnimationEnd={() => setIdx((i) => (i + 1) % RUNNING_WORDS.length)}
      style={{
        animation: 'wordCycle 2.8s ease-in-out forwards',
        display: 'inline-block',
        fontFamily: 'var(--font-mono)',
        fontSize: '0.6875rem',
        fontWeight: 600,
        color: WORD_COLORS[idx % WORD_COLORS.length],
      }}
    >
      {RUNNING_WORDS[idx]}
    </span>
  );
}

function RunningTimer({ createdAt }: { createdAt: string }) {
  const start = new Date(createdAt).getTime();
  const [elapsed, setElapsed] = useState(() => Date.now() - start);
  useEffect(() => {
    const id = setInterval(() => setElapsed(Date.now() - start), 1000);
    return () => clearInterval(id);
  }, [start]);
  const s = Math.floor(elapsed / 1000);
  const display = s < 60
    ? `${s}s`
    : s < 3600
      ? `${Math.floor(s / 60)}m ${s % 60}s`
      : `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return (
    <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-neon-green)' }}>
      {display}
    </span>
  );
}

// 8 columns: step | role | status | outcome | assigned to | bridge | created | duration
const SUB_COLS = '40px 90px 1fr 100px 120px 110px 78px 74px';

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
  const isRunning = task.status === 'queued' || task.status === 'dispatched';
  const bridge = nullStr(task.bridge_id);

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
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: isRunning ? 'var(--color-neon-green)' : 'var(--color-neon-amber)', display: 'flex', alignItems: 'center' }}>
        {isRunning
          ? <Loader2 size={13} style={{ animation: 'spin 1.2s linear infinite' }} />
          : step
        }
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-neon-cyan)' }}>
        {task.role || '–'}
      </span>
      <span>
        {task.current_status
          ? <StatusChip status={task.current_status} />
          : <span style={{ color: 'var(--color-text-muted)' }}>–</span>
        }
      </span>
      <span>
        {isRunning ? <RotatingWord /> : (
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', fontWeight: 600, color: taskOutcomeColor(task.status) }}>
            {task.status}
          </span>
        )}
      </span>
      <span style={{
        fontFamily: 'var(--font-mono)',
        fontSize: '0.6875rem',
        color: task.assignee ? 'var(--color-neon-cyan)' : 'var(--color-neon-yellow)',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
      }}>
        {task.assignee || task.role || '–'}
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', opacity: bridge !== '–' ? 1 : 0.4 }}>
        {bridge}
      </span>
      <span style={{ fontSize: '0.6875rem', color: 'var(--color-text-muted)' }}>
        {relativeTime(task.created_at)}
      </span>
      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-text-muted)' }}>
        {isRunning
          ? <RunningTimer createdAt={task.created_at} />
          : duration(task.created_at, task.finished_at)
        }
      </span>
    </div>
  );
}

function ExpandedIssueDetail({ issueNumber }: { issueNumber: number }) {
  const { data, error, mutate } = useSWR<IssueDetailResponse>(
    `/api/v1/dashboard/issues/${issueNumber}`,
    (url: string) => get<IssueDetailResponse>(url),
    { refreshInterval: 10000 }
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

export function LiveView() {
  const { lastEvent } = useSSE();
  const { data, error, mutate } = useSWR<IssueResponse>(
    '/api/v1/dashboard/issues',
    (url: string) => get<IssueResponse>(url),
    { refreshInterval: 30000 }
  );

  const [expandedRow, setExpandedRow] = useState<number | null>(null);
  const [pulsingIssues, setPulsingIssues] = useState<Set<number>>(new Set());

  // Re-fetch when an SSE event arrives.
  useEffect(() => {
    if (!lastEvent) return;
    const issueNum = lastEvent.data?.issue_number;
    if (issueNum) {
      setPulsingIssues((prev) => new Set([...prev, issueNum]));
      setTimeout(() => {
        setPulsingIssues((prev) => {
          const next = new Set(prev);
          next.delete(issueNum);
          return next;
        });
      }, 600);
    }
    void mutate();
  }, [lastEvent, mutate]);

  const issues = data?.issues ?? [];

  return (
    <div>
      <h2 className="page-title">Live View</h2>

      {error && <ErrorBanner onRetry={() => void mutate()} />}

      {!error && !data && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 'var(--spacing-2xl)',
            color: 'var(--md-sys-color-on-surface-variant)',
            gap: 'var(--spacing-sm)',
          }}
        >
          <Loader2 size={40} style={{ opacity: 0.5, animation: 'spin 1.2s linear infinite' }} />
          <span>Loading…</span>
        </div>
      )}

      {data && issues.length === 0 && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 'var(--spacing-2xl)',
            color: 'var(--md-sys-color-on-surface-variant)',
            gap: 'var(--spacing-sm)',
          }}
        >
          <ClipboardList size={40} style={{ opacity: 0.4 }} />
          <span style={{ fontWeight: 500 }}>No active workflows</span>
          <span style={{ fontSize: '0.875rem' }}>
            Issues will appear here as work packages arrive.
          </span>
        </div>
      )}

      {issues.length > 0 && (
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
              fontFamily: 'var(--font-mono)',
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
            <span>Title</span>
            <span>Status</span>
            <span>Assignee</span>
            <span>Bridge</span>
            <span>Updated</span>
          </div>

          {issues.map((issue) => {
            const isExpanded = expandedRow === issue.number;
            const isPulsing = pulsingIssues.has(issue.number);
            return (
              <React.Fragment key={issue.number}>
                <div
                  role="row"
                  tabIndex={0}
                  onClick={() => setExpandedRow(isExpanded ? null : issue.number)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ')
                      setExpandedRow(isExpanded ? null : issue.number);
                  }}
                  className="live-row"
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '28px 80px 1fr 160px 160px 80px 110px',
                    padding: '11px 16px',
                    alignItems: 'center',
                    cursor: 'pointer',
                    borderBottom: isExpanded
                      ? 'none'
                      : '1px solid var(--md-sys-color-outline-variant)',
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
                  <a
                    href={issue.url}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(e) => e.stopPropagation()}
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.75rem',
                      color: 'var(--color-neon-amber)',
                      textDecoration: 'none',
                    }}
                  >
                    #{issue.number}
                  </a>
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.75rem',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {issue.title || `Issue #${issue.number}`}
                  </span>
                  <span
                    style={{
                      animation: isPulsing ? 'pulse 600ms ease-out' : undefined,
                    }}
                  >
                    <StatusChip status={issue.currentStatus} />
                  </span>
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.75rem',
                      color: 'var(--color-neon-cyan)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {issue.assignees.join(', ') || '–'}
                  </span>
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.75rem',
                      color: 'var(--color-text-muted)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {issue.activeTask?.bridgeId || '–'}
                  </span>
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.75rem',
                      color: 'var(--color-text-muted)',
                    }}
                  >
                    {relativeTime(issue.updatedAt)}
                  </span>
                </div>

                {isExpanded && (
                  <div
                    style={{
                      borderLeft: '4px solid var(--md-sys-color-primary)',
                      borderBottom: '1px solid var(--md-sys-color-outline-variant)',
                      background: 'var(--md-sys-color-surface)',
                    }}
                  >
                    <ExpandedIssueDetail issueNumber={issue.number} />
                  </div>
                )}
              </React.Fragment>
            );
          })}
        </div>
      )}

      <style>{`
        .live-row:hover { background: var(--color-primary-subtle) !important; }
        @keyframes pulse {
          0% { opacity: 1; }
          50% { opacity: 0.5; }
          100% { opacity: 1; }
        }
      `}</style>
    </div>
  );
}

export function ErrorBanner({ onRetry }: { onRetry: () => void }) {
  return (
    <div
      style={{
        border: '1px solid var(--md-sys-color-error)',
        borderRadius: 'var(--md-shape-medium)',
        padding: 'var(--spacing-md)',
        marginBottom: 'var(--spacing-lg)',
        display: 'flex',
        alignItems: 'center',
        gap: '10px',
        color: 'var(--md-sys-color-error)',
      }}
    >
      <Zap size={18} strokeWidth={2.5} />
      <span style={{ fontSize: '0.9375rem', fontWeight: 500 }}>Service is offline</span>
      <button
        onClick={onRetry}
        aria-label="Retry"
        title="Retry"
        style={{
          marginLeft: 'auto',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--md-sys-color-error)',
          display: 'flex',
          alignItems: 'center',
          padding: '4px',
          borderRadius: 'var(--radius-sm)',
          opacity: 0.75,
          transition: 'opacity 150ms ease',
        }}
        onMouseEnter={e => (e.currentTarget.style.opacity = '1')}
        onMouseLeave={e => (e.currentTarget.style.opacity = '0.75')}
      >
        <RotateCcw size={17} />
      </button>
    </div>
  );
}
