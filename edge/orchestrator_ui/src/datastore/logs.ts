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
  // Bumped on every connect/disconnect so a ticket request that resolves after
  // the hook moved on does not leave an orphaned socket open.
  const generation = useRef(0);

  const connect = useCallback(async () => {
    if (!dockerId) return;
    const mine = ++generation.current;
    let ws: WebSocket;
    try {
      ws = await openLogStream(dockerId, sinceMs);
    } catch {
      if (mine === generation.current) setConnected(false);
      return;
    }
    if (mine !== generation.current) {
      ws.close();
      return;
    }
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
    generation.current++;
    wsRef.current?.close();
    wsRef.current = null;
    setConnected(false);
  }, []);

  useEffect(() => {
    void connect();
    return () => disconnect();
  }, [connect, disconnect]);

  return { lines, connected, reconnect: connect, disconnect };
}
