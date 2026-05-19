import { useState } from 'react';
import { Copy, Check } from 'lucide-react';

interface Props {
  code: string;
  language?: string;
}

export function CodeBlock({ code }: Props) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  return (
    <div
      style={{
        position: 'relative',
        background: 'var(--md-sys-color-surface-variant)',
        borderRadius: 'var(--md-shape-small)',
        padding: 'var(--spacing-md)',
        marginBottom: 'var(--spacing-md)',
      }}
    >
      <pre
        style={{
          margin: 0,
          fontFamily: 'var(--font-mono)',
          fontSize: '0.875rem',
          lineHeight: 1.5,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all',
          color: 'var(--md-sys-color-on-surface)',
          paddingRight: '40px',
        }}
      >
        {code}
      </pre>
      <button
        aria-label="Copy code"
        title="Copy to clipboard"
        onClick={handleCopy}
        style={{
          position: 'absolute',
          top: '8px',
          right: '8px',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: copied ? 'var(--color-neon-green)' : 'var(--md-sys-color-on-surface-variant)',
          padding: '4px',
          borderRadius: '4px',
          minWidth: '32px',
          minHeight: '32px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          transition: 'color 150ms ease',
        }}
      >
        {copied ? <Check size={15} /> : <Copy size={15} />}
      </button>
    </div>
  );
}
