import React, { useState } from 'react';
import { ChevronUp, ChevronDown } from 'lucide-react';
import { CodeBlock } from '../components/CodeBlock';

const BASE = 'http://127.0.0.1:6666';

interface Endpoint {
  method: string;
  path: string;
  description: string;
  requestBody?: string;
  responseSchema: string;
  curlExample: string;
}

const ENDPOINTS: Endpoint[] = [
  {
    method: 'GET',
    path: '/api/v1/dashboard/issues',
    description: 'List all active GitHub issues with their current status, assignee, and task-queue state.',
    responseSchema: JSON.stringify({
      issues: [
        {
          number: 59,
          title: 'Web dashboard for github.mcp server',
          url: 'https://github.com/martchouk/github.mcp/issues/59',
          currentStatus: 'status:in-development',
          assignees: ['bud-dev'],
          activeTask: {
            id: 42,
            role: 'developer',
            taskStatus: 'dispatched',
            bridgeId: 'bridge-1',
            createdAt: '2026-05-18T10:00:00Z',
          },
          updatedAt: '2026-05-18T10:00:00Z',
        },
      ],
    }, null, 2),
    curlExample: `curl ${BASE}/api/v1/dashboard/issues`,
  },
  {
    method: 'GET',
    path: '/api/v1/dashboard/issues/{number}',
    description: 'Single issue detail — all tasks and recent transition audit entries.',
    responseSchema: JSON.stringify({
      number: 59,
      tasks: [{ id: 42, role: 'developer', status: 'dispatched' }],
      audit: [
        {
          id: 1,
          issue_number: 59,
          from_status: 'status:ready-for-dev',
          to_status: 'status:in-development',
          actor: 'bud-dev',
          result: 'success',
          created_at: '2026-05-18T10:00:00Z',
        },
      ],
    }, null, 2),
    curlExample: `curl ${BASE}/api/v1/dashboard/issues/59`,
  },
  {
    method: 'GET',
    path: '/api/v1/dashboard/tasks',
    description: 'Recent tasks from the tasks table (all statuses). Optional ?limit=N (max 500).',
    responseSchema: JSON.stringify({
      tasks: [
        {
          id: 42,
          issue_number: 59,
          role: 'developer',
          status: 'dispatched',
          created_at: '2026-05-18T10:00:00Z',
        },
      ],
    }, null, 2),
    curlExample: `curl "${BASE}/api/v1/dashboard/tasks?limit=50"`,
  },
  {
    method: 'GET',
    path: '/api/v1/dashboard/audit',
    description: 'Recent entries from the transition_audit table. Optional ?limit=N (max 500).',
    responseSchema: JSON.stringify({
      audit: [
        {
          id: 1,
          issue_number: 59,
          from_status: 'status:ready-for-dev',
          to_status: 'status:in-development',
          actor: 'bud-dev',
          trigger_type: 'webhook',
          result: 'success',
          created_at: '2026-05-18T10:00:00Z',
        },
      ],
    }, null, 2),
    curlExample: `curl "${BASE}/api/v1/dashboard/audit?limit=50"`,
  },
  {
    method: 'GET',
    path: '/api/v1/dashboard/stream',
    description: 'Server-Sent Events stream. Pushes issue_updated, task_queued, task_dispatched, and task_completed events.',
    responseSchema: `event: connected
data: {"clients":1}

event: task_queued
data: {"type":"task_queued","data":{"issue_number":59,"task_id":42,"role":"developer","status":"status:in-development","assignee":"bud-dev"}}

: heartbeat`,
    curlExample: `curl -N -H "Accept: text/event-stream" ${BASE}/api/v1/dashboard/stream`,
  },
  {
    method: 'GET',
    path: '/api/v1/workflows',
    description: 'List all workflow definitions parsed from workflows/*.yaml.',
    responseSchema: JSON.stringify({
      workflows: [
        { id: 'simplified_3_role_issue_workflow', key: 'lean', statusCount: 11, edgeCount: 18 },
        { id: 'full_6_roles_issue_workflow', key: 'full', statusCount: 15, edgeCount: 28 },
      ],
    }, null, 2),
    curlExample: `curl ${BASE}/api/v1/workflows`,
  },
  {
    method: 'GET',
    path: '/api/v1/workflows/{id}',
    description: 'Full workflow as graph-ready JSON (nodes + edges). {id} may be the workflow key (e.g. lean) or the full ID.',
    responseSchema: JSON.stringify({
      id: 'simplified_3_role_issue_workflow',
      key: 'lean',
      nodes: [{ id: 'status:new', role: 'po', category: 'intake', terminal: false, label: 'status:new' }],
      edges: [
        {
          id: 'po_start_definition__status:new',
          source: 'status:new',
          target: 'status:story-definition',
          allowedRoles: ['po'],
          description: 'PO starts user story definition.',
        },
      ],
    }, null, 2),
    curlExample: `curl ${BASE}/api/v1/workflows/lean`,
  },
];


export function DocsApi() {
  const [expanded, setExpanded] = useState<string | null>(null);

  return (
    <div style={{ maxWidth: '900px' }}>
      <h2 className="page-title">API Reference</h2>
      <p style={{ color: 'var(--md-sys-color-on-surface-variant)', marginBottom: 'var(--spacing-xl)' }}>
        All endpoints are unauthenticated (v1: trusted internal network). Base URL:{' '}
        <code style={{ fontFamily: 'var(--font-mono)' }}>{BASE}</code>
      </p>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-md)' }}>
        {ENDPOINTS.map((ep) => {
          const key = `${ep.method}:${ep.path}`;
          const isOpen = expanded === key;
          return (
            <div
              key={key}
              style={{
                border: '1px solid var(--md-sys-color-outline-variant)',
                borderRadius: 'var(--md-shape-medium)',
                overflow: 'hidden',
              }}
            >
              {/* Endpoint header */}
              <button
                onClick={() => setExpanded(isOpen ? null : key)}
                style={{
                  width: '100%',
                  padding: 'var(--spacing-md)',
                  background: isOpen ? 'var(--md-sys-color-surface-variant)' : 'var(--md-sys-color-surface)',
                  border: 'none',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--spacing-md)',
                  textAlign: 'left',
                }}
                aria-expanded={isOpen}
              >
                <span className={`http-method http-method--${ep.method.toLowerCase()}`}>
                  {ep.method}
                </span>
                <code
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: '0.875rem',
                    color: 'var(--md-sys-color-on-surface)',
                    flex: 1,
                  }}
                >
                  {ep.path}
                </code>
                <span
                  style={{
                    fontSize: '0.875rem',
                    color: 'var(--md-sys-color-on-surface-variant)',
                    flex: 2,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {ep.description}
                </span>
                {isOpen
                  ? <ChevronUp size={18} style={{ color: 'var(--md-sys-color-on-surface-variant)', flexShrink: 0 }} />
                  : <ChevronDown size={18} style={{ color: 'var(--md-sys-color-on-surface-variant)', flexShrink: 0 }} />
                }
              </button>

              {/* Expanded content */}
              {isOpen && (
                <div
                  style={{
                    padding: 'var(--spacing-lg)',
                    borderTop: '1px solid var(--md-sys-color-outline-variant)',
                  }}
                >
                  <p style={{ marginTop: 0, fontSize: '0.875rem' }}>{ep.description}</p>

                  {ep.requestBody && (
                    <div style={{ marginBottom: 'var(--spacing-md)' }}>
                      <Label>Request body</Label>
                      <CodeBlock code={ep.requestBody} language="request" />
                    </div>
                  )}

                  <div style={{ marginBottom: 'var(--spacing-md)' }}>
                    <Label>Response (200 OK)</Label>
                    <CodeBlock code={ep.responseSchema} language="json" />
                  </div>

                  <div>
                    <Label>curl example</Label>
                    <CodeBlock code={ep.curlExample} language="bash" />
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function Label({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        fontSize: '0.75rem',
        fontWeight: 500,
        color: 'var(--md-sys-color-on-surface-variant)',
        marginBottom: 'var(--spacing-xs)',
        textTransform: 'uppercase',
        letterSpacing: '0.08em',
      }}
    >
      {children}
    </div>
  );
}
