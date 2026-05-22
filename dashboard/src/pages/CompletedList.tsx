import React, { useState } from 'react';
import { Loader2, CheckCircle2, ChevronDown, ChevronRight } from 'lucide-react';
import { ErrorBanner } from './LiveView';
import useSWR from 'swr';
import type { CompletedIssueSummary, CompletedRunDetail, TaskRow } from '../api/types';
import { get } from '../api/client';
import { StatusChip } from '../components/StatusChip';
import { GithubIcon } from '../components/GithubIcon';
import { relativeTime, duration, nullStr, taskOutcomeColor, truncate64 } from '../utils/format';

interface CompletedListResponse {
  completed: CompletedIssueSummary[];
}

// 8 columns: step | role | status | outcome | assigned to | bridge | created | duration
const SUB_COLS = '50px 90px 1fr 100px 120px 90px 78px 74px';

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
      <span className="flex-center-all"><GithubIcon size={17} color="var(--color-text-muted)" /></span>
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
// Circle links to the GitHub issue comment when commentUrl is provided.
function StepNode({ step, isFirst, isLast, commentUrl, commentId }: {
  step: number; isFirst: boolean; isLast: boolean;
  commentUrl: string | null; commentId: number | null;
}) {
  const filled = isFirst || isLast;
  const [hovered, setHovered] = useState(false);
  const [tipPos, setTipPos] = useState<{ x: number; y: number } | null>(null);

  const hoverBg = filled
    ? 'color-mix(in srgb, var(--color-neon-amber) 75%, white)'
    : 'color-mix(in srgb, var(--color-neon-amber) 22%, transparent)';

  const handleMouseEnter = (e: React.MouseEvent<HTMLElement>) => {
    setHovered(true);
    if (commentId) {
      const r = e.currentTarget.getBoundingClientRect();
      setTipPos({ x: r.right + 8, y: r.top + r.height / 2 });
    }
  };

  const handleMouseLeave = () => {
    setHovered(false);
    setTipPos(null);
  };

  const circleStyle: React.CSSProperties = {
    position: 'relative',
    zIndex: 1,
    width: '17px',
    height: '17px',
    borderRadius: '50%',
    border: '1.5px solid var(--color-neon-amber)',
    background: commentUrl && hovered ? hoverBg : (filled ? 'var(--color-neon-amber)' : 'transparent'),
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontFamily: 'var(--font-mono)',
    fontSize: '0.5rem',
    fontWeight: 700,
    color: filled ? 'var(--md-sys-color-surface)' : 'var(--color-neon-amber)',
    flexShrink: 0,
    textDecoration: 'none',
    cursor: commentUrl ? 'pointer' : 'default',
    transition: 'background 120ms ease',
  };

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
          bottom: 'calc(50% + 9.5px)',
          left: '50%',
          transform: 'translateX(-50%)',
          width: '1.5px',
          background: 'var(--color-neon-amber)',
        }} />
      )}
      {/* circle — 17px, links to GitHub comment when available */}
      {commentUrl ? (
        <a
          href={commentUrl}
          target="_blank"
          rel="noreferrer"
          onClick={(e) => e.stopPropagation()}
          style={circleStyle}
          onMouseEnter={handleMouseEnter}
          onMouseLeave={handleMouseLeave}
        >
          {step}
        </a>
      ) : (
        <div style={circleStyle}>{step}</div>
      )}
      {/* bottom connector — hidden for last node */}
      {!isLast && (
        <div style={{
          position: 'absolute',
          top: 'calc(50% + 9.5px)',
          bottom: 0,
          left: '50%',
          transform: 'translateX(-50%)',
          width: '1.5px',
          background: 'var(--color-neon-amber)',
        }} />
      )}
      {/* Custom styled tooltip — position: fixed escapes overflow clipping */}
      {hovered && commentId && tipPos && (
        <div style={{
          position: 'fixed',
          left: tipPos.x,
          top: tipPos.y,
          transform: 'translateY(-50%)',
          zIndex: 9999,
          pointerEvents: 'none',
          background: 'var(--md-sys-color-inverse-surface)',
          color: 'var(--md-sys-color-inverse-on-surface)',
          border: '1px solid var(--md-sys-color-outline-variant)',
          borderRadius: '0.5rem',
          padding: '5px 10px',
          boxShadow: '0 4px 20px rgba(0,0,0,0.4)',
          fontFamily: 'var(--font-mono)',
          fontSize: '0.75rem',
          whiteSpace: 'nowrap',
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
        }}>
          <GithubIcon size={11} color="currentColor" />
          {`issuecomment-${commentId}`}
        </div>
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
      {/* Row separator — skips the # column on all rows except the last */}
      <div style={{
        position: 'absolute',
        bottom: 0,
        left: isLast ? 0 : '66px',
        right: 0,
        height: '1px',
        background: 'var(--md-sys-color-outline-variant)',
      }} />

      <StepNode
        step={step}
        isFirst={isFirst}
        isLast={isLast}
        commentUrl={commentUrl}
        commentId={task.last_comment_id ?? null}
      />

      <div style={cell()}>
        <span className="mono-sm text-cyan">
          {task.role || '–'}
        </span>
      </div>

      <div style={cell()}>
        {task.current_status
          ? <StatusChip status={task.current_status} />
          : <span className="text-muted">–</span>
        }
      </div>

      <div style={cell()}>
        <span className="mono-xs" style={{ fontWeight: 600, color: taskOutcomeColor(task.status) }}>
          {task.status}
        </span>
      </div>

      <div style={cell({ overflow: 'hidden' })}>
        <span className={`mono-xs text-truncate ${workerLogin ? 'text-cyan' : 'text-yellow'}`}>
          {workerLogin || task.role || ''}
        </span>
      </div>

      <div style={cell({ overflow: 'hidden' })}>
        <span className="mono-xs text-muted text-truncate" style={{ opacity: bridge !== '–' ? 1 : 0.4 }}>
          {bridge}
        </span>
      </div>

      <div style={cell()}>
        <span className="text-xs text-muted">
          {relativeTime(task.created_at)}
        </span>
      </div>

      <div style={cell()}>
        <span className="mono-xs text-muted">
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
    return (
      <div className="flex-center-gap text-error text-xxs" style={{ padding: '10px 16px' }}>
        <span>Failed to load tasks.</span>
        <button onClick={() => void mutate()} className="text-error text-xxs" style={{ background: 'none', border: 'none', cursor: 'pointer', textDecoration: 'underline', padding: 0 }}>Retry</button>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="flex-center-gap text-muted text-sm" style={{ padding: '14px 16px' }}>
        <Loader2 size={16} className="spin" />
        Loading tasks…
      </div>
    );
  }

  // Tasks come DESC from API; reverse to chronological (step 1, 2, 3…)
  const tasks = [...data.tasks].reverse();

  if (tasks.length === 0) {
    return (
      <div className="text-muted text-sm" style={{ padding: '12px 16px' }}>
        No task records found.
      </div>
    );
  }

  return (
    <div className="scroll-x">
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

const PAGE_SIZE = 10;

export function CompletedList() {
  const { data, error, mutate } = useSWR<CompletedListResponse>(
    '/api/v1/dashboard/completed',
    (url: string) => get<CompletedListResponse>(url),
    { refreshInterval: 60000 }
  );

  const [expandedIssue, setExpandedIssue] = useState<number | null>(null);
  const [page, setPage] = useState(0);

  const runs = data?.completed ?? [];
  const totalPages = Math.max(1, Math.ceil(runs.length / PAGE_SIZE));
  const safePage = Math.min(page, totalPages - 1);
  const visibleRuns = runs.slice(safePage * PAGE_SIZE, (safePage + 1) * PAGE_SIZE);

  function goToPage(next: number) {
    setPage(next);
    setExpandedIssue(null);
  }

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
          <Loader2 size={40} className="spin-sm" />
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

      {!error && runs.length > 0 && (
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

          {visibleRuns.map((run) => {
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
                  <span className="flex-center text-muted">
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
                    className="mono-sm text-amber"
                    style={{ textDecoration: 'none' }}
                  >
                    {run.issueNumber}
                  </a>
                  <span className="mono-sm text-muted text-truncate">
                    {run.repo || '–'}
                  </span>
                  <span className="mono-sm text-truncate">
                    {run.title ? truncate64(run.title) : <span className="text-muted" style={{ opacity: 0.5 }}>–</span>}
                  </span>
                  <span>
                    <StatusChip status={run.finalStatus} />
                  </span>
                  <span className="mono-sm text-cyan text-truncate">
                    {run.workflowKey || '–'}
                  </span>
                  <span className="mono-sm text-amber">
                    {run.stepCount}
                  </span>
                  <span className="mono-sm text-muted">
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

      {!error && totalPages > 1 && (
        <div style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          gap: '12px',
          padding: '14px 0 4px',
        }}>
          <button
            onClick={() => goToPage(safePage - 1)}
            disabled={safePage === 0}
            style={{
              padding: '5px 14px',
              borderRadius: '6px',
              border: '1px solid var(--md-sys-color-outline-variant)',
              background: 'var(--md-sys-color-surface)',
              color: safePage === 0 ? 'var(--color-text-muted)' : 'var(--md-sys-color-on-surface)',
              cursor: safePage === 0 ? 'default' : 'pointer',
              fontFamily: 'var(--font-mono)',
              fontSize: '0.75rem',
              opacity: safePage === 0 ? 0.4 : 1,
            }}
          >
            ← Newer
          </button>
          <span className="mono-sm text-muted">
            {safePage + 1} / {totalPages}
          </span>
          <button
            onClick={() => goToPage(safePage + 1)}
            disabled={safePage === totalPages - 1}
            style={{
              padding: '5px 14px',
              borderRadius: '6px',
              border: '1px solid var(--md-sys-color-outline-variant)',
              background: 'var(--md-sys-color-surface)',
              color: safePage === totalPages - 1 ? 'var(--color-text-muted)' : 'var(--md-sys-color-on-surface)',
              cursor: safePage === totalPages - 1 ? 'default' : 'pointer',
              fontFamily: 'var(--font-mono)',
              fontSize: '0.75rem',
              opacity: safePage === totalPages - 1 ? 0.4 : 1,
            }}
          >
            Older →
          </button>
        </div>
      )}

      <style>{`
        .completed-row:hover { background: var(--color-primary-subtle) !important; }
      `}</style>
    </div>
  );
}
