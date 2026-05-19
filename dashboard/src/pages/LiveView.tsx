import React, { useState, useEffect } from 'react';
import { WifiOff, AlertCircle, Loader2, ClipboardList } from 'lucide-react';
import useSWR from 'swr';
import type { Issue } from '../api/types';
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

interface IssueResponse {
  issues: Issue[];
}

export function LiveView() {
  const { status: sseStatus, lastEvent } = useSSE();
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
      {/* Page header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 'var(--spacing-lg)',
        }}
      >
        <h2
          style={{
            margin: 0,
            fontFamily: 'var(--font-sans)',
            fontSize: '1.75rem',
            fontWeight: 400,
          }}
        >
          Live View
        </h2>

        {/* SSE indicator */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          {sseStatus === 'connected' ? (
            <>
              <div
                style={{
                  width: '10px',
                  height: '10px',
                  borderRadius: '50%',
                  background: 'var(--md-sys-color-tertiary)',
                }}
              />
              <span
                style={{
                  fontSize: '0.75rem',
                  color: 'var(--md-sys-color-on-surface-variant)',
                }}
                role="status"
                aria-live="polite"
              >
                Live
              </span>
            </>
          ) : sseStatus === 'disconnected' ? (
            <>
              <WifiOff size={18} style={{ color: 'var(--md-sys-color-error)' }} />
              <span style={{ fontSize: '0.75rem', color: 'var(--md-sys-color-error)' }}>
                Disconnected
              </span>
            </>
          ) : (
            <span style={{ fontSize: '0.75rem', color: 'var(--md-sys-color-on-surface-variant)' }}>
              Connecting…
            </span>
          )}
        </div>
      </div>

      {/* Error state */}
      {error && (
        <div
          style={{
            border: '1px solid var(--md-sys-color-error)',
            borderRadius: 'var(--md-shape-medium)',
            padding: 'var(--spacing-md)',
            color: 'var(--md-sys-color-error)',
            marginBottom: 'var(--spacing-lg)',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}
        >
          <AlertCircle size={20} />
          {error.message || 'Failed to load issues'}
          <button
            onClick={() => void mutate()}
            style={{
              marginLeft: 'auto',
              background: 'none',
              border: 'none',
              color: 'var(--md-sys-color-primary)',
              cursor: 'pointer',
              fontWeight: 500,
            }}
          >
            Retry
          </button>
        </div>
      )}

      {/* Empty state */}
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

      {/* Issues table */}
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
              gridTemplateColumns: '72px 1fr 180px 120px 120px 96px',
              padding: '12px 16px',
              background: 'var(--md-sys-color-surface-variant)',
              fontFamily: 'var(--font-sans)',
              fontSize: '0.875rem',
              fontWeight: 500,
              letterSpacing: '0.00625rem',
              color: 'var(--md-sys-color-on-surface-variant)',
              borderBottom: '1px solid var(--md-sys-color-outline-variant)',
            }}
          >
            <span>#</span>
            <span>Title</span>
            <span>Status</span>
            <span>Assignee</span>
            <span>Bridge</span>
            <span>Updated</span>
          </div>

          {/* Rows */}
          {issues.map((issue) => {
            const isExpanded = expandedRow === issue.number;
            const isPulsing = pulsingIssues.has(issue.number);
            return (
              <React.Fragment key={issue.number}>
                <div
                  role="row"
                  tabIndex={0}
                  onClick={() =>
                    setExpandedRow(isExpanded ? null : issue.number)
                  }
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ')
                      setExpandedRow(isExpanded ? null : issue.number);
                  }}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '72px 1fr 180px 120px 120px 96px',
                    padding: '12px 16px',
                    alignItems: 'center',
                    cursor: 'pointer',
                    borderBottom: '1px solid var(--md-sys-color-outline-variant)',
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
                  <a
                    href={issue.url}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(e) => e.stopPropagation()}
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: '0.875rem',
                      color: 'var(--md-sys-color-primary)',
                      textDecoration: 'none',
                    }}
                  >
                    #{issue.number}
                  </a>
                  <span
                    style={{
                      fontSize: '0.875rem',
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
                      fontSize: '0.75rem',
                      color: 'var(--md-sys-color-on-surface-variant)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {issue.assignees.join(', ') || '–'}
                  </span>
                  <span
                    style={{
                      fontSize: '0.6875rem',
                      color: 'var(--md-sys-color-on-surface-variant)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {issue.activeTask?.bridgeId || '–'}
                  </span>
                  <span
                    style={{
                      fontSize: '0.6875rem',
                      color: 'var(--md-sys-color-on-surface-variant)',
                    }}
                  >
                    {relativeTime(issue.updatedAt)}
                  </span>
                </div>

                {/* Expanded row */}
                {isExpanded && (
                  <div
                    style={{
                      padding: 'var(--spacing-md) var(--spacing-lg)',
                      background: 'var(--md-sys-color-surface)',
                      borderBottom: '1px solid var(--md-sys-color-outline-variant)',
                      borderLeft: '4px solid var(--md-sys-color-primary)',
                    }}
                  >
                    {issue.activeTask && (
                      <div
                        style={{
                          display: 'flex',
                          gap: 'var(--spacing-xl)',
                          flexWrap: 'wrap',
                        }}
                      >
                        <Detail label="Task ID" value={String(issue.activeTask.id)} />
                        <Detail label="Task Status" value={issue.activeTask.taskStatus} />
                        <Detail label="Role" value={issue.activeTask.role} />
                        <Detail label="Created" value={new Date(issue.activeTask.createdAt).toLocaleString()} />
                      </div>
                    )}
                  </div>
                )}
              </React.Fragment>
            );
          })}
        </div>
      )}

      <style>{`
        @keyframes pulse {
          0% { opacity: 1; }
          50% { opacity: 0.5; }
          100% { opacity: 1; }
        }
      `}</style>
    </div>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div
        style={{
          fontSize: '0.6875rem',
          fontWeight: 500,
          color: 'var(--md-sys-color-on-surface-variant)',
          marginBottom: '2px',
        }}
      >
        {label}
      </div>
      <div style={{ fontSize: '0.875rem' }}>{value}</div>
    </div>
  );
}
