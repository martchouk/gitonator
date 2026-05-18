import { useCallback, useEffect, useState } from 'react';

type Theme = 'light' | 'dark';

function getInitialTheme(): Theme {
  const stored = localStorage.getItem('theme');
  if (stored === 'dark' || stored === 'light') return stored;
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(getInitialTheme);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const toggle = useCallback(() => {
    const next: Theme = theme === 'light' ? 'dark' : 'light';
    setTheme(next);
    localStorage.setItem('theme', next);
  }, [theme]);

  return (
    <button
      aria-label={`Switch to ${theme === 'light' ? 'dark' : 'light'} theme`}
      title={`Switch to ${theme === 'light' ? 'dark' : 'light'} theme`}
      onClick={toggle}
      style={{
        background: 'none',
        border: 'none',
        cursor: 'pointer',
        color: 'var(--md-sys-color-on-surface)',
        padding: '8px',
        borderRadius: '50%',
        minWidth: '48px',
        minHeight: '48px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize: '24px',
      }}
    >
      <span className="material-icons">{theme === 'light' ? 'dark_mode' : 'light_mode'}</span>
    </button>
  );
}
