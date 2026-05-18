import React from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { ThemeToggle } from './ThemeToggle';

const NAV_ITEMS = [
  { to: '/', label: 'Live View', icon: 'dashboard', exact: true },
  { to: '/workflows', label: 'Workflows', icon: 'account_tree' },
  { to: '/docs/setup', label: 'Setup Docs', icon: 'menu_book' },
  { to: '/docs/api', label: 'API Docs', icon: 'api' },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const location = useLocation();

  return (
    <div style={{ display: 'flex', flexDirection: 'column', minHeight: '100vh' }}>
      {/* Top App Bar */}
      <header
        style={{
          height: '64px',
          background: 'var(--md-sys-color-surface)',
          borderBottom: '1px solid var(--md-sys-color-outline-variant)',
          display: 'flex',
          alignItems: 'center',
          padding: '0 var(--spacing-md)',
          gap: 'var(--spacing-md)',
          position: 'sticky',
          top: 0,
          zIndex: 100,
        }}
      >
        <span
          className="material-icons"
          style={{ color: 'var(--md-sys-color-primary)', fontSize: '28px' }}
        >
          hub
        </span>
        <h1
          style={{
            flex: 1,
            margin: 0,
            fontFamily: 'Roboto, sans-serif',
            fontSize: '1.375rem',
            fontWeight: 500,
            color: 'var(--md-sys-color-on-surface)',
          }}
        >
          github.mcp Dashboard
        </h1>
        <ThemeToggle />
      </header>

      <div style={{ display: 'flex', flex: 1 }}>
        {/* Navigation Drawer (desktop) */}
        <nav
          aria-label="Main navigation"
          style={{
            width: 'var(--nav-drawer-width)',
            background: 'var(--md-sys-color-surface)',
            borderRight: '1px solid var(--md-sys-color-outline-variant)',
            padding: 'var(--spacing-sm) var(--spacing-sm)',
            flexShrink: 0,
          }}
          className="nav-drawer"
        >
          {NAV_ITEMS.map((item) => {
            const active = item.exact
              ? location.pathname === item.to
              : location.pathname.startsWith(item.to);
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.exact}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--spacing-md)',
                  padding: '12px var(--spacing-md)',
                  borderRadius: 'var(--md-shape-full)',
                  color: active
                    ? 'var(--md-sys-color-on-primary-container)'
                    : 'var(--md-sys-color-on-surface-variant)',
                  background: active ? 'var(--md-sys-color-primary-container)' : 'transparent',
                  textDecoration: 'none',
                  fontFamily: 'Roboto, sans-serif',
                  fontSize: '0.875rem',
                  fontWeight: 500,
                  letterSpacing: '0.00625rem',
                  transition: 'background 150ms ease',
                  marginBottom: '4px',
                }}
              >
                <span className="material-icons" style={{ fontSize: '24px' }}>
                  {item.icon}
                </span>
                {item.label}
              </NavLink>
            );
          })}
        </nav>

        {/* Page content */}
        <main
          style={{
            flex: 1,
            padding: 'var(--spacing-lg)',
            maxWidth: '1200px',
            overflowX: 'auto',
          }}
        >
          {children}
        </main>
      </div>

      <style>{`
        @media (max-width: 600px) {
          .nav-drawer {
            display: none;
          }
        }
      `}</style>
    </div>
  );
}
