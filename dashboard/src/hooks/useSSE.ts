import { useEffect, useRef, useState } from 'react';
import type { SSEEvent } from '../api/types';
import { BASE_URL } from '../api/client';

export type SSEStatus = 'connecting' | 'connected' | 'disconnected';

interface UseSSEResult {
  status: SSEStatus;
  lastEvent: SSEEvent | null;
}

export function useSSE(): UseSSEResult {
  const [status, setStatus] = useState<SSEStatus>('connecting');
  const [lastEvent, setLastEvent] = useState<SSEEvent | null>(null);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let active = true;

    function connect() {
      if (!active) return;
      setStatus('connecting');

      const es = new EventSource(`${BASE_URL}/api/v1/dashboard/stream`);
      esRef.current = es;

      es.addEventListener('connected', () => {
        if (active) setStatus('connected');
      });

      const forwardEvent = (type: string) => (e: MessageEvent) => {
        if (!active) return;
        try {
          const data = JSON.parse(e.data);
          setLastEvent({ type, data });
        } catch {
          // ignore malformed events
        }
      };

      for (const type of ['task_queued', 'task_dispatched', 'task_completed', 'issue_updated']) {
        es.addEventListener(type, forwardEvent(type));
      }

      es.onerror = () => {
        if (!active) return;
        setStatus('disconnected');
        es.close();
        // Reconnect after 5 seconds.
        setTimeout(connect, 5000);
      };
    }

    connect();

    return () => {
      active = false;
      esRef.current?.close();
    };
  }, []);

  return { status, lastEvent };
}
