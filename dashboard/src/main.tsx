import React from 'react';
import ReactDOM from 'react-dom/client';
import { App } from './App';
import './theme/tokens.css';
import './theme/status-colors.css';
import './theme/typography.css';

// Apply theme before first render to avoid flash of wrong theme.
const stored = localStorage.getItem('theme');
const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
document.documentElement.dataset.theme =
  stored === 'dark' || stored === 'light' ? stored : prefersDark ? 'dark' : 'light';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
