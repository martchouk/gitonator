import React, { useState, useEffect } from 'react';
import { Zap, RotateCcw, Loader2, ClipboardList, ChevronDown, ChevronRight } from 'lucide-react';
import useSWR from 'swr';
import type { Issue, TaskRow } from '../api/types';
import { get } from '../api/client';
import { StatusChip } from '../components/StatusChip';
import { GithubIcon } from '../components/GithubIcon';
import { relativeTime, duration, nullStr, taskOutcomeColor, truncate64 } from '../utils/format';
import { useSSE } from '../hooks/useSSE';

function repoFromUrl(url: string): string {
  const m = url.match(/github\.com\/([^/]+\/[^/]+)/);
  return m ? m[1] : '';
}

interface IssueResponse {
  issues: Issue[];
}

interface IssueDetailResponse {
  number: number;
  tasks: TaskRow[];
}

const RUNNING_WORDS = [
  // original cast
  'hyperspacing…', 'bebopping…',      'determining…',   'hammering…',
  'wibbling…',     'pondering…',      'cogitating…',    'calibrating…',
  'reticulating…', 'manifolding…',    'fluxing…',       'spooling…',
  'recursing…',    'triangulating…',  'defragging…',    'synergising…',
  'oscillating…',  'fuzzing…',        'caffeinating…',  'processing…',
  'noodling…',     'wobbling…',       'crunching…',     'compiling…',
  'nucleating…',   'crystallizing…',  'gesticulating…', 'transforming…',
  'ebbing…',       'pontificating…',  'transfiguring…', 'wrangling…',
  'enchanting…',   'grooving…',       'scampering…',    'compacting…',
  'zipping…',      'zigzagging…',     'cascading…',     'thinking…',
  'imagining…',    'transmuting…',    'computing…',     'churning…',
  // locomotion
  'perambulating…','moonwalking…',    'wandering…',     'moseying…',
  'meandering…',   'gallivanting…',   'skedaddling…',   'tiptoeing…',
  'frolicking…',   'loitering…',      'lurking…',       'skulking…',
  'sneaking…',     'duckwalking…',    'goosestepping…', 'crabwalking…',
  'penguinwaddling…','pajamawandering…',
  // social & verbal
  'hobnobbing…',   'schmoozing…',     'bantering…',     'quibbling…',
  'babbling…',     'blabbering…',     'gibbering…',     'jabbering…',
  'yodelling…',    'snickering…',     'mumbling…',      'grumbling…',
  'snorting…',
  // body noises & functions
  'hiccuping…',    'burping…',        'slurping…',      'gargling…',
  'gurgling…',     'drooling…',       'dribbling…',     'yawning…',
  'sneezing…',     'farting…',        'sniffing…',      'spitting…',
  'scratching…',
  // physical comedy
  'gobsmacking…',  'facepalming…',    'headbutting…',   'bellyflopping…',
  'somersaulting…','cartwheeling…',
  // food & cooking
  'frosting…',     'garnishing…',     'simmering…',     'sizzling…',
  'fermenting…',   'pickling…',       'marinating…',    'glazing…',
  'sprinkling…',   'dusting…',        'whisking…',      'kneading…',
  'folding…',      'stuffing…',       'basting…',       'seasoning…',
  'caramelizing…', 'schmearing…',     'toasting…',      'roasting…',
  'flambéing…',
  // eating
  'nibbling…',     'munching…',       'chomping…',      'chewing…',
  // wrangling & herding
  'herding…',      'corralling…',     'lassoing…',      'juggling…',
  'balancing…',    'haggling…',       'bargaining…',
  // scheming & faffing
  'plotting…',     'scheming…',       'meddling…',      'faffing…',
  'dithering…',    'dawdling…',       'futzing…',       'puttering…',
  'fiddling…',     'fumbling…',       'bumbling…',      'stumbling…',
  'tumbling…',     'rambling…',       'doodling…',      'canoodling…',
  // befuddlement
  'puzzling…',     'befuddling…',     'flustering…',    'bamboozling…',
  'hoodwinking…',  'shenaniganing…',  'malarkeying…',   'kerfuffling…',
  'hullabalooing…','gobbledygooking…','brouhahaing…',
  // coding & tech
  'refactoring…',  'debugging…',      'rubberducking…', 'committering…',
  'squashing…',    'orchestrating…',  'tinkering…',     'hacking…',
  'patching…',     'crashing…',       'glitching…',     'buffering…',
  'spinning…',     'freezing…',       'overengineering…','procrastinating…',
  'overthinking…', 'underthinking…',  'brainstorming…', 'daydreaming…',
  // dreamland
  'stargazing…',   'cloudbusting…',   'moonlighting…',  'improvising…',
  'ruminating…',   'perusing…',
  // sounds & sfx
  'fizzing…',      'buzzing…',        'zapping…',       'zooming…',
  'whooshing…',    'swooshing…',      'boinging…',      'bouncing…',
  'blooping…',     'blipping…',       'beeping…',       'honking…',
  'tooting…',      'plopping…',       'plunking…',      'clanking…',
  'clattering…',   'rattling…',       'rustling…',
  // textures & fluids
  'squelching…',   'squishing…',      'splattering…',   'dripping…',
  'oozing…',       'bubbling…',       'swirling…',      'spinning…',
  // wiggles
  'bobbling…',     'squiggling…',     'wiggling…',      'jiggling…',
  // cozy
  'snuggling…',    'cuddling…',       'nuzzling…',      'nestling…',
  'snoozing…',     'lounging…',       'lazing…',        'hibernating…',
  'cocooning…',    'pillowfighting…', 'teasipping…',    'biscuitdunking…',
  // animal behaviour
  'peacocking…',   'flamingoing…',    'ottering…',      'badgering…',
  'squirreling…',  'ferreting…',      'monkeying…',     'parroting…',
  'lemminging…',   'possuming…',      'hedgehogging…',  'raccooning…',
  // misc
  'bashing…',      'githubbing…',     'greenwashing…',  'quacking…',
  'squawking…',    'cackling…',       'clucking…',      'grunting…',
  'flibbering…',   'gibbeting…',      'knuddling…',
];

const WORD_COLORS = [
  'var(--color-neon-green)',
  'var(--color-neon-cyan)',
  'var(--color-neon-amber)',
  'var(--color-neon-magenta)',
  'var(--color-neon-yellow)',
  'var(--color-primary)',
  'var(--md-sys-color-secondary)',
  'var(--color-accent)',
  'var(--color-neon-cyan)',
  'var(--color-neon-green)',
  'var(--color-neon-amber)',
  'var(--color-neon-magenta)',
  'var(--color-neon-yellow)',
  'var(--color-primary)',
  'var(--md-sys-color-secondary)',
  'var(--color-accent)',
];

function RotatingWord() {
  const [idx, setIdx] = useState(() => Math.floor(Math.random() * RUNNING_WORDS.length));
  return (
    <span
      key={idx}
      onAnimationEnd={() => setIdx((i) => (i + 1) % RUNNING_WORDS.length)}
      style={{
        animation: 'wordCycle 4s ease-in-out forwards',
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
      : s < 86400
        ? `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m ${s % 60}s`
        : `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h ${Math.floor((s % 3600) / 60)}m ${s % 60}s`;
  return (
    <span className="mono-xs text-green">
      {display}
    </span>
  );
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

// Roadmap node: circle with step number + connector lines.
// First/last circles are amber-filled. Running circle is a spinning arc with no inner content.
// Connectors touching a running circle (the running step's top, or the previous step's bottom) are dotted green.
function StepNode({ step, isFirst, isLast, isRunning, nextIsRunning, commentUrl, commentId }: {
  step: number; isFirst: boolean; isLast: boolean;
  isRunning: boolean; nextIsRunning?: boolean; commentUrl: string | null; commentId: number | null;
}) {
  const [hovered, setHovered] = useState(false);
  const [tipPos, setTipPos] = useState<{ x: number; y: number } | null>(null);

  const connectorBase: React.CSSProperties = {
    position: 'absolute', left: '50%', transform: 'translateX(-50%)', width: '1.5px',
  };
  const dottedGreen: React.CSSProperties = {
    width: '4px',
    backgroundImage: 'radial-gradient(circle 1.5px at 50% 50%, color-mix(in srgb, var(--color-neon-green) 40%, transparent) 100%, transparent 100%)',
    backgroundSize: '4px 5px',
    backgroundRepeat: 'repeat-y',
    backgroundPosition: '50% 0',
  };

  const topConnector = !isFirst ? (
    isRunning
      ? <div style={{ ...connectorBase, top: 0, bottom: 'calc(54% + 11px)', ...dottedGreen }} />
      : <div style={{ ...connectorBase, top: 0, bottom: 'calc(50% + 9.5px)', background: 'var(--color-neon-amber)' }} />
  ) : null;

  const bottomConnector = !isLast ? (
    (isRunning || nextIsRunning)
      ? <div style={{ ...connectorBase, top: 'calc(54% + 11px)', bottom: 0, ...dottedGreen }} />
      : <div style={{ ...connectorBase, top: 'calc(50% + 9.5px)', bottom: 0, background: 'var(--color-neon-amber)' }} />
  ) : null;

  if (isRunning) {
    return (
      <div style={{ position: 'relative', display: 'flex', alignItems: 'center', justifyContent: 'center', alignSelf: 'stretch' }}>
        {topConnector}
        <div style={{
          position: 'relative', zIndex: 1,
          width: '17px', height: '17px', borderRadius: '50%',
          borderTop: '1.5px solid var(--color-neon-green)',
          borderRight: '1.5px solid color-mix(in srgb, var(--color-neon-green) 25%, transparent)',
          borderBottom: '1.5px solid color-mix(in srgb, var(--color-neon-green) 25%, transparent)',
          borderLeft: '1.5px solid color-mix(in srgb, var(--color-neon-green) 25%, transparent)',
          background: 'transparent', flexShrink: 0,
          animation: 'spin 1.2s linear infinite',
        }} />
        {bottomConnector}
      </div>
    );
  }

  const filled = isFirst || isLast;
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
    position: 'relative', zIndex: 1,
    width: '17px', height: '17px', borderRadius: '50%',
    border: '1.5px solid var(--color-neon-amber)',
    background: commentUrl && hovered ? hoverBg : (filled ? 'var(--color-neon-amber)' : 'transparent'),
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    fontFamily: 'var(--font-mono)', fontSize: '0.5rem', fontWeight: 700,
    color: filled ? 'var(--md-sys-color-surface)' : 'var(--color-neon-amber)',
    flexShrink: 0, textDecoration: 'none',
    cursor: commentUrl ? 'pointer' : 'default',
    transition: 'background 120ms ease',
  };

  return (
    <div style={{ position: 'relative', display: 'flex', alignItems: 'center', justifyContent: 'center', alignSelf: 'stretch' }}>
      {topConnector}
      {commentUrl ? (
        <a href={commentUrl} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}
          style={circleStyle} onMouseEnter={handleMouseEnter} onMouseLeave={handleMouseLeave}>
          {step}
        </a>
      ) : (
        <div style={circleStyle}>{step}</div>
      )}
      {bottomConnector}
      {hovered && commentId && tipPos && (
        <div style={{
          position: 'fixed', left: tipPos.x, top: tipPos.y, transform: 'translateY(-50%)',
          zIndex: 9999, pointerEvents: 'none',
          background: 'var(--md-sys-color-inverse-surface)', color: 'var(--md-sys-color-inverse-on-surface)',
          border: '1px solid var(--md-sys-color-outline-variant)', borderRadius: '0.5rem',
          padding: '5px 10px', boxShadow: '0 4px 20px rgba(0,0,0,0.4)',
          fontFamily: 'var(--font-mono)', fontSize: '0.75rem', whiteSpace: 'nowrap',
          display: 'flex', alignItems: 'center', gap: '6px',
        }}>
          <GithubIcon size={11} color="currentColor" />
          {`issuecomment-${commentId}`}
        </div>
      )}
    </div>
  );
}

function SubTaskRow({ task, step, isFirst, isLast, nextIsRunning }: { task: TaskRow; step: number; isFirst: boolean; isLast: boolean; nextIsRunning?: boolean }) {
  const isRunning = task.status === 'queued' || task.status === 'dispatched';
  const bridge = nullStr(task.bridge_id);
  const workerLogin = (task.claimed_by?.Valid && task.claimed_by?.String)
    ? task.claimed_by.String
    : task.assignee || null;

  const commentUrl = !isRunning && task.last_comment_id
    ? `https://github.com/${task.repo}/issues/${task.issue_number}#issuecomment-${task.last_comment_id}`
    : null;

  const cell = (style?: React.CSSProperties): React.CSSProperties => ({
    display: 'flex', alignItems: 'center', padding: '11px 0', ...style,
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
      <div style={{ position: 'absolute', bottom: 0, left: isLast ? 0 : '66px', right: 0, height: '1px', background: 'var(--md-sys-color-outline-variant)' }} />

      <StepNode step={step} isFirst={isFirst} isLast={isLast} isRunning={isRunning} nextIsRunning={nextIsRunning}
        commentUrl={commentUrl} commentId={task.last_comment_id ?? null} />

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

      <div style={cell({ paddingRight: '16px', overflow: 'hidden' })}>
        {isRunning ? <RotatingWord /> : (
          <span className="mono-xs" style={{ fontWeight: 600, color: taskOutcomeColor(task.status) }}>
            {task.status}
          </span>
        )}
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
          {isRunning ? <RunningTimer createdAt={task.created_at} /> : duration(task.created_at, task.finished_at)}
        </span>
      </div>
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
        {tasks.map((task, idx) => {
          const nextTask = tasks[idx + 1];
          const nextIsRunning = nextTask ? (nextTask.status === 'queued' || nextTask.status === 'dispatched') : false;
          return (
            <SubTaskRow key={task.id} task={task} step={idx + 1}
              isFirst={idx === 0} isLast={idx === tasks.length - 1}
              nextIsRunning={nextIsRunning} />
          );
        })}
      </div>
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
          <Loader2 size={40} className="spin-sm" />
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

      {!error && issues.length > 0 && (
        <div
          style={{
            border: '1px solid var(--md-sys-color-outline-variant)',
            borderRadius: 'var(--md-shape-medium)',
            overflow: 'hidden',
          }}
        >
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
                    gridTemplateColumns: '28px 80px 160px 1fr 160px',
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
                  <span className="flex-center text-muted">
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
                    className="mono-sm text-amber"
                    style={{ textDecoration: 'none' }}
                  >
                    #{issue.number}
                  </a>
                  <span className="mono-sm text-muted text-truncate">
                    {issue.repo || repoFromUrl(issue.url)}
                  </span>
                  <span className="mono-sm text-truncate">
                    {truncate64(issue.title || `Issue #${issue.number}`)}
                  </span>
                  <span
                    style={{
                      animation: isPulsing ? 'pulse 600ms ease-out' : undefined,
                      justifySelf: 'end',
                    }}
                  >
                    <StatusChip status={issue.currentStatus} />
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
