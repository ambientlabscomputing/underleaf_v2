import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { logsService, openLogStream, type LogLine } from '../api/services/LogsService';

export const logKeys = {
  history: (dockerId: string, sinceMs?: number) => ['logs', 'history', dockerId, sinceMs] as const,
};

export function useContainerLogHistory(dockerId: string, sinceMs?: number) {
  return useQuery({
    queryKey: logKeys.history(dockerId, sinceMs),
    queryFn: () => logsService.getLogs(dockerId, { since_ms: sinceMs }),
    enabled: !!dockerId,
  });
}

export function useLiveContainerLogs(dockerId: string, sinceMs?: number) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);

  const connect = useCallback(() => {
    if (!dockerId) return;
    const ws = openLogStream(dockerId, sinceMs);
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onerror = () => setConnected(false);
    ws.onmessage = (evt) => {
      try {
        const line: LogLine = JSON.parse(evt.data);
        setLines((prev) => [...prev, line]);
      } catch {
        // ignore malformed frames
      }
    };
  }, [dockerId, sinceMs]);

  const disconnect = useCallback(() => {
    wsRef.current?.close();
    wsRef.current = null;
    setConnected(false);
  }, []);

  useEffect(() => {
    connect();
    return () => disconnect();
  }, [connect, disconnect]);

  return { lines, connected, reconnect: connect, disconnect };
}
