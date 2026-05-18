import React from 'react';
import { CodeBlock } from '../components/CodeBlock';

export function DocsSetup() {
  return (
    <div style={{ maxWidth: '768px' }}>
      <h2 style={{ fontWeight: 400, marginBottom: 'var(--spacing-lg)' }}>Setup Guide</h2>
      <p style={{ color: 'var(--md-sys-color-on-surface-variant)', marginBottom: 'var(--spacing-xl)' }}>
        Step-by-step instructions for configuring a GitHub repository to work with
        github.mcp.
      </p>

      <Section title="1. Prerequisites">
        <ul>
          <li>Go 1.21+ installed on the server machine</li>
          <li>A GitHub account with permission to create Personal Access Tokens</li>
          <li>SQLite3 available (typically pre-installed on Linux/macOS)</li>
        </ul>
      </Section>

      <Section title="2. Create a GitHub Personal Access Token">
        <p>
          Create a PAT with the following scopes:{' '}
          <code className="inline-code">repo</code>,{' '}
          <code className="inline-code">write:discussion</code>.
        </p>
        <p>
          Go to <strong>GitHub → Settings → Developer settings → Personal access tokens →
          Fine-grained tokens</strong> (or classic tokens with <code className="inline-code">repo</code> scope).
        </p>
      </Section>

      <Section title="3. Clone the Repository">
        <CodeBlock
          code={`git clone https://github.com/martchouk/github.mcp.git
cd github.mcp`}
        />
      </Section>

      <Section title="4. Configure Environment Variables">
        <p>
          Copy the service template and set your values:
        </p>
        <CodeBlock
          code={`cp deploy/github.mcp.service_TEMPLATE deploy/github.mcp.service`}
        />
        <p>Required environment variables:</p>
        <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 'var(--spacing-md)' }}>
          <thead>
            <tr style={{ background: 'var(--md-sys-color-surface-variant)' }}>
              <Th>Variable</Th>
              <Th>Required</Th>
              <Th>Description</Th>
            </tr>
          </thead>
          <tbody>
            <Tr><Td><code>GITHUB_TOKEN</code></Td><Td>Yes</Td><Td>PAT with repo scope</Td></Tr>
            <Tr><Td><code>GITHUB_OWNER</code></Td><Td>Yes</Td><Td>Repository owner (user or org)</Td></Tr>
            <Tr><Td><code>GITHUB_REPO</code></Td><Td>Yes</Td><Td>Repository name</Td></Tr>
            <Tr><Td><code>HTTP_ADDR</code></Td><Td>No</Td><Td>Webhook server address (default: 127.0.0.1:7777)</Td></Tr>
            <Tr><Td><code>DASHBOARD_ADDR</code></Td><Td>No</Td><Td>Dashboard API address (default: disabled; use 127.0.0.1:6666)</Td></Tr>
            <Tr><Td><code>WEBHOOK_SECRET</code></Td><Td>Recommended</Td><Td>GitHub webhook secret for HMAC verification</Td></Tr>
            <Tr><Td><code>AGENT_SHARED_TOKEN</code></Td><Td>Yes</Td><Td>Bearer token used by bridge agents</Td></Tr>
            <Tr><Td><code>SQLITE_PATH</code></Td><Td>No</Td><Td>SQLite file path (default: orchestrator.db)</Td></Tr>
            <Tr><Td><code>WORKFLOWS_DIR</code></Td><Td>No</Td><Td>Workflow YAML directory (default: workflows/)</Td></Tr>
          </tbody>
        </table>
      </Section>

      <Section title="5. Initialise Repository Labels">
        <p>
          The orchestrator uses GitHub labels to track workflow state. Run the
          initialisation script to create all required labels:
        </p>
        <CodeBlock
          code={`# For the lean (3-role) workflow:
GITHUB_TOKEN=ghp_... GITHUB_OWNER=myorg GITHUB_REPO=myrepo \\
  bash deploy/init_repo_lean.sh

# For the full (6-role) workflow:
GITHUB_TOKEN=ghp_... GITHUB_OWNER=myorg GITHUB_REPO=myrepo \\
  bash deploy/init_repo_full.sh`}
        />
      </Section>

      <Section title="6. Configure the GitHub Webhook">
        <p>
          In your repository: <strong>Settings → Webhooks → Add webhook</strong>.
        </p>
        <ul>
          <li>
            Payload URL:{' '}
            <code className="inline-code">https://your-server.example.com/webhook/github</code>
          </li>
          <li>Content type: <code className="inline-code">application/json</code></li>
          <li>Secret: the value of <code className="inline-code">WEBHOOK_SECRET</code></li>
          <li>
            Events: <strong>Issues</strong> and <strong>Issue comments</strong>
          </li>
        </ul>
        <p>
          To select the workflow, append <code className="inline-code">?workflow=lean</code> or{' '}
          <code className="inline-code">?workflow=full</code> to the Payload URL.
        </p>
      </Section>

      <Section title="7. Build and Start the Server">
        <CodeBlock
          code={`go build -o github-issue-orchestrator .

# With environment variables:
GITHUB_TOKEN=ghp_... \\
GITHUB_OWNER=myorg \\
GITHUB_REPO=myrepo \\
AGENT_SHARED_TOKEN=my-secret-token \\
DASHBOARD_ADDR=127.0.0.1:6666 \\
./github-issue-orchestrator`}
        />
        <p>
          Or use the systemd service unit in <code className="inline-code">deploy/github.mcp.service</code>.
        </p>
      </Section>

      <Section title="8. Connect the Dashboard">
        <p>
          With <code className="inline-code">DASHBOARD_ADDR=127.0.0.1:6666</code> set, the REST API
          is available at <code className="inline-code">http://127.0.0.1:6666</code>.
        </p>
        <p>
          Deploy the built frontend (<code className="inline-code">dashboard/dist/</code>) to your web
          server (e.g., nginx on singularia.de). The frontend connects to{' '}
          <code className="inline-code">http://127.0.0.1:6666</code> directly from the browser — this
          works because the frontend and backend run on the same machine (no CORS issue).
        </p>
        <CodeBlock
          code={`# Build the dashboard frontend:
cd dashboard
npm install
npm run build
# dist/ is ready to deploy`}
        />
      </Section>

      <style>{`
        .inline-code {
          font-family: 'Roboto Mono', monospace;
          font-size: 0.85em;
          background: var(--md-sys-color-surface-variant);
          padding: 1px 6px;
          border-radius: 4px;
        }
      `}</style>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section style={{ marginBottom: 'var(--spacing-xl)' }}>
      <h3
        style={{
          fontFamily: 'Roboto, sans-serif',
          fontSize: '1.375rem',
          fontWeight: 500,
          marginBottom: 'var(--spacing-md)',
          paddingBottom: 'var(--spacing-sm)',
          borderBottom: '1px solid var(--md-sys-color-outline-variant)',
        }}
      >
        {title}
      </h3>
      {children}
    </section>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th
      style={{
        padding: '8px 12px',
        textAlign: 'left',
        fontSize: '0.875rem',
        fontWeight: 500,
        color: 'var(--md-sys-color-on-surface-variant)',
        borderBottom: '1px solid var(--md-sys-color-outline-variant)',
      }}
    >
      {children}
    </th>
  );
}

function Td({ children }: { children: React.ReactNode }) {
  return (
    <td
      style={{
        padding: '8px 12px',
        fontSize: '0.875rem',
        borderBottom: '1px solid var(--md-sys-color-outline-variant)',
      }}
    >
      {children}
    </td>
  );
}

function Tr({ children }: { children: React.ReactNode }) {
  return <tr>{children}</tr>;
}
