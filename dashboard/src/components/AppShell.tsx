import { NavLink } from 'react-router-dom';
import { Activity, GitBranch, CheckCircle2, BookOpen, Code2 } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { ThemeToggle } from './ThemeToggle';
import { LogoMark } from './LogoMark';

const NAV_ITEMS: { to: string; label: string; Icon: LucideIcon; exact?: boolean }[] = [
  { to: '/', label: 'Live View', Icon: Activity, exact: true },
  { to: '/workflows', label: 'Workflows', Icon: GitBranch },
  { to: '/completed', label: 'Completed', Icon: CheckCircle2 },
  { to: '/docs/setup', label: 'Setup Docs', Icon: BookOpen },
  { to: '/docs/api', label: 'API Docs', Icon: Code2 },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="app-root">
      <header className="top-nav">
        {/* Logo */}
        <div className="nav-logo">
          <LogoMark size={28} className="nav-logo-icon" />
          <span className="nav-logo-text">github.mcp</span>
        </div>

        {/* Centered nav pills */}
        <nav className="nav-links" aria-label="Main navigation">
          {NAV_ITEMS.map(({ to, label, Icon, exact }) => (
            <NavLink
              key={to}
              to={to}
              end={exact}
              className={({ isActive }) =>
                isActive ? 'nav-link nav-link--active' : 'nav-link'
              }
            >
              <Icon size={16} className="nav-link-icon" />
              {label}
            </NavLink>
          ))}
        </nav>

        {/* Right actions */}
        <div className="nav-actions">
          <ThemeToggle />
        </div>
      </header>

      <main className="page-main">{children}</main>
    </div>
  );
}
