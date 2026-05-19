
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AppShell } from './components/AppShell';
import { LiveView } from './pages/LiveView';
import { WorkflowList } from './pages/WorkflowList';
import { WorkflowGraph } from './pages/WorkflowGraph';
import { CompletedList } from './pages/CompletedList';
import { CompletedRun } from './pages/CompletedRun';
import { DocsSetup } from './pages/DocsSetup';
import { DocsApi } from './pages/DocsApi';

export function App() {
  return (
    <BrowserRouter>
      <AppShell>
        <Routes>
          <Route path="/" element={<LiveView />} />
          <Route path="/workflows" element={<WorkflowList />} />
          <Route path="/workflows/:id" element={<WorkflowGraph />} />
          <Route path="/completed" element={<CompletedList />} />
          <Route path="/completed/:number" element={<CompletedRun />} />
          <Route path="/docs/setup" element={<DocsSetup />} />
          <Route path="/docs/api" element={<DocsApi />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AppShell>
    </BrowserRouter>
  );
}
