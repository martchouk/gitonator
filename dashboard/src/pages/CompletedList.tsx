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

function GithubIcon({ size = 14, color = 'currentColor' }: { size?: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" fill={color} xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z"/>
    </svg>
  );
}

function truncate64(str: string): string {
  return str.length > 64 ? str.slice(0, 64) + '…' : str;
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

// 9 columns: step | gh-comment | role | status | outcome | assigned to | bridge | created | duration
const SUB_COLS = '40px 32px 90px 1fr 100px 120px 90px 78px 74px';

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
      <span style={{ display: 'flex', alignItems: 'center' }}><GithubIcon size={12} color="var(--color-text-muted)" /></span>
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

// Roadmap node: circle with step number + vertical connector lines.
// isFirst/isLast circles are color-filled; middle circles are hollow.
function StepNode({ step, isFirst, isLast }: { step: number; isFirst: boolean; isLast: boolean }) {
  const filled = isFirst || isLast;
  return (
    <div style={{
      position: 'relative',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      alignSelf: 'stretch',
    }}>
      {/* top connector — hidden for first node */}
      {!isFirst && (
        <div style={{
          position: 'absolute',
          top: 0,
          bottom: 'calc(50% + 11px)',
          left: '50%',
          transform: 'translateX(-50%)',
          width: '1.5px',
          background: 'var(--color-neon-amber)',
          opacity: 0.35,
        }} />
      )}
      {/* circle */}
      <div style={{
        position: 'relative',
        zIndex: 1,
        width: '22px',
        height: '22px',
        borderRadius: '50%',
        border: '1.5px solid var(--color-neon-amber)',
        background: filled ? 'var(--color-neon-amber)' : 'transparent',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontFamily: 'var(--font-mono)',
        fontSize: '0.625rem',
        fontWeight: 700,
        color: filled ? 'var(--md-sys-color-surface)' : 'var(--color-neon-amber)',
        flexShrink: 0,
      }}>
        {step}
      </div>
      {/* bottom connector — hidden for last node */}
      {!isLast && (
        <div style={{
          position: 'absolute',
          top: 'calc(50% + 11px)',
          bottom: 0,
          left: '50%',
          transform: 'translateX(-50%)',
          width: '1.5px',
          background: 'var(--color-neon-amber)',
          opacity: 0.35,
        }} />
      )}
    </div>
  );
}

function SubTaskRow({ task, step, isFirst, isLast }: { task: TaskRow; step: number; isFirst: boolean; isLast: boolean }) {
  const bridge = nullStr(task.bridge_id);
  const workerLogin = (task.claimed_by?.Valid && task.claimed_by?.String)
    ? task.claimed_by.String
    : task.assignee || null;

  const commentUrl = task.last_comment_id
    ? `https://github.com/${task.repo}/issues/${task.issue_number}#issuecomment-${task.last_comment_id}`
    : null;

  // Cell wrapper: vertically centers content within the stretched row.
  const cell = (style?: React.CSSProperties): React.CSSProperties => ({
    display: 'flex',
    alignItems: 'center',
    padding: '11px 0',
    ...style,
  });

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: SUB_COLS,
        padding: '0 16px',
        minHeight: '44px',
        alignItems: 'stretch',
        position: 'relative',
        fontSize: '0.8125rem',
      }}
    >
      {/* Row separator that skips the # column (16px left-padding + 40px # column = 56px) */}
      <div style={{
        position: 'absolute',
        bottom: 0,
        left: '56px',
        right: 0,
        height: '1px',
        background: 'var(--md-sys-color-outline-variant)',
      }} />

      <StepNode step={step} isFirst={isFirst} isLast={isLast} />

      <div style={cell()}>
        {commentUrl ? (
          <a
            href={commentUrl}
            target="_blank"
            rel="noreferrer"
            onClick={(e) => e.stopPropagation()}
            title="View GitHub comment"
            style={{ display: 'flex', alignItems: 'center', opacity: 0.7, transition: 'opacity 150ms ease' }}
            onMouseEnter={e => (e.currentTarget.style.opacity = '1')}
            onMouseLeave={e => (e.currentTarget.style.opacity = '0.7')}
          >
            <GithubIcon size={13} color="var(--color-neon-cyan)" />
          </a>
        ) : (
          <GithubIcon size={13} color="var(--color-text-muted)" />
        )}
      </div>

      <div style={cell()}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: 'var(--color-neon-cyan)' }}>
          {task.role || '–'}
        </span>
      </div>

      <div style={cell()}>
        {task.current_status
          ? <StatusChip status={task.current_status} />
          : <span style={{ color: 'var(--color-text-muted)' }}>–</span>
        }
      </div>

      <div style={cell()}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', fontWeight: 600, color: taskOutcomeColor(task.status) }}>
          {task.status}
        </span>
      </div>

      <div style={cell({ overflow: 'hidden' })}>
        <span style={{
          fontFamily: 'var(--font-mono)',
          fontSize: '0.6875rem',
          color: workerLogin ? 'var(--color-neon-cyan)' : 'var(--color-neon-yellow)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}>
          {workerLogin || task.role || ''}
        </span>
      </div>

      <div style={cell({ overflow: 'hidden' })}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', opacity: bridge !== '–' ? 1 : 0.4 }}>
          {bridge}
        </span>
      </div>

      <div style={cell()}>
        <span style={{ fontSize: '0.6875rem', color: 'var(--color-text-muted)' }}>
          {relativeTime(task.created_at)}
        </span>
      </div>

      <div style={cell()}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-text-muted)' }}>
          {duration(task.created_at, task.finished_at)}
        </span>
      </div>
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
    <div style={{ overflowX: 'auto' }}>
      <div style={{ minWidth: '780px' }}>
        <SubTableHeader />
        {tasks.map((task, idx) => (
          <SubTaskRow
            key={task.id}
            task={task}
            step={idx + 1}
            isFirst={idx === 0}
            isLast={idx === tasks.length - 1}
          />
        ))}
      </div>
    </div>
  );
}

// 8 columns: arrow | issue | repo | issue-name | final-status | workflow | steps | completed
const MAIN_COLS = '28px 80px 160px 1fr 160px 160px 80px 110px';

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
              gridTemplateColumns: MAIN_COLS,
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
            <span>Issue</span>
            <span>Repo</span>
            <span>Issue name</span>
            <span>Final Status</span>
            <span>Workflow</span>
            <span>Steps</span>
            <span>Completed</span>
          </div>

          {runs.map((run) => {
            const isExpanded = expandedIssue === run.issueNumber;
            const issueUrl = `https://github.com/${run.repo}/issues/${run.issueNumber}`;
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
                    gridTemplateColumns: MAIN_COLS,
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
                  <a
                    href={issueUrl}
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
                    {run.issueNumber}
                  </a>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', color: 'var(--color-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {run.repo || '–'}
                  </span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {run.title ? truncate64(run.title) : <span style={{ color: 'var(--color-text-muted)', opacity: 0.5 }}>–</span>}
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
