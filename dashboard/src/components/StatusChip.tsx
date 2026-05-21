
const categoryVars: Record<string, { bg: string; color: string }> = {
  intake: {
    bg: 'var(--status-color-intake-container)',
    color: 'var(--status-color-intake-on-container)',
  },
  design: {
    bg: 'var(--status-color-design-container)',
    color: 'var(--status-color-design-on-container)',
  },
  requirements: {
    bg: 'var(--status-color-requirements-container)',
    color: 'var(--status-color-requirements-on-container)',
  },
  planning: {
    bg: 'var(--status-color-planning-container)',
    color: 'var(--status-color-planning-on-container)',
  },
  implementation: {
    bg: 'var(--status-color-implementation-container)',
    color: 'var(--status-color-implementation-on-container)',
  },
  review: {
    bg: 'var(--status-color-review-container)',
    color: 'var(--status-color-review-on-container)',
  },
  acceptance: {
    bg: 'var(--status-color-acceptance-container)',
    color: 'var(--status-color-acceptance-on-container)',
  },
  terminal: {
    bg: 'var(--status-color-terminal-container)',
    color: 'var(--status-color-terminal-on-container)',
  },
  exception: {
    bg: 'var(--status-color-exception-container)',
    color: 'var(--status-color-exception-on-container)',
  },
  blocked: {
    bg: 'var(--status-color-blocked-container)',
    color: 'var(--status-color-blocked-on-container)',
  },
};

function inferCategory(status: string): string {
  if (!status) return 'terminal';
  const s = status.toLowerCase();
  if (s.includes('blocked') || s.includes('rejected')) return 'exception';
  if (s.includes('done') || s.includes('closed') || s.includes('complete')) return 'terminal';
  if (s.includes('review') || s.includes('code-review')) return 'review';
  if (s.includes('approval') || s.includes('acceptance') || s.includes('po-')) return 'acceptance';
  if (s.includes('dev') || s.includes('implementation') || s.includes('progress')) return 'implementation';
  if (s.includes('planning') || s.includes('plan')) return 'planning';
  if (s.includes('design') || s.includes('solution') || s.includes('story')) return 'design';
  if (s.includes('triage') || s.includes('new') || s.includes('intake')) return 'intake';
  if (s.includes('require') || s.includes('definition')) return 'requirements';
  return 'terminal';
}

interface Props {
  status: string;
  category?: string;
  truncate?: boolean;
  maxWidth?: string | number;
}

export function StatusChip({ status, category, truncate = false, maxWidth }: Props) {
  const cat = category || inferCategory(status);
  const colors = categoryVars[cat] ?? categoryVars.terminal;
  const label = status.startsWith('status:') ? status.slice(7) : status;

  return (
    <span
      className="status-chip"
      data-category={cat}
      style={{
        display: 'inline-block',
        padding: '2px 10px',
        borderRadius: 'var(--radius-sm)',
        background: colors.bg,
        color: colors.color,
        fontFamily: 'var(--font-sans)',
        fontSize: '0.75rem',
        fontWeight: 500,
        letterSpacing: '0.01em',
        lineHeight: '1.33',
        whiteSpace: 'nowrap',
        maxWidth,
        overflow: truncate ? 'hidden' : undefined,
        textOverflow: truncate ? 'ellipsis' : undefined,
        verticalAlign: 'top',
      }}
      title={truncate ? label : undefined}
    >
      {label}
    </span>
  );
}
