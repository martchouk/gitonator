import { useCallback, useEffect, useState } from 'react';
import { Moon, Sun } from 'lucide-react';

type Theme = 'light' | 'dark';

function getInitialTheme(): Theme {
  const stored = localStorage.getItem('theme');
  if (stored === 'dark' || stored === 'light') return stored;
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(getInitialTheme);

  // Apply theme to the root element whenever it changes.
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  // React to OS-level color scheme changes when the user has not pinned a theme.
  // Per the guide: "you MUST also use addEventListener('change', fn) to react to changes."
  useEffect(() => {
    if (localStorage.getItem('theme')) return; // user has pinned — don't override
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = (e: MediaQueryListEvent) => {
      const next: Theme = e.matches ? 'dark' : 'light';
      setTheme(next);
      document.documentElement.dataset.theme = next;
    };
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

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
      className="theme-toggle"
    >
      {theme === 'light' ? <Moon size={18} /> : <Sun size={18} />}
    </button>
  );
}
