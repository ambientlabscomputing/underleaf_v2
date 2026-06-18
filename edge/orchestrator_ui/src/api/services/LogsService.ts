import { apiClient } from '../client';

export interface LogLine {
  docker_id: string;
  ts_ms: number;
  stream: 'stdout' | 'stderr';
  message: string;
}

export interface LogPage {
  lines: LogLine[] | null;
  next_cursor: number;
}

export const logsService = {
  getLogs: (dockerId: string, params?: { since_ms?: number; until_ms?: number; limit?: number; cursor_id?: number }) => {
    const qs = new URLSearchParams();
    if (params?.since_ms) qs.set('since_ms', String(params.since_ms));
    if (params?.until_ms) qs.set('until_ms', String(params.until_ms));
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.cursor_id) qs.set('cursor_id', String(params.cursor_id));
    const query = qs.toString();
    return apiClient.get<LogPage>(`/containers/${dockerId}/logs${query ? `?${query}` : ''}`);
  },
};

// Build a WebSocket URL from the REST BASE_URL.
const wsBase = (() => {
  const base = import.meta.env.VITE_API_URL ?? 'http://localhost:9090/api/v2';
  return base.replace(/^http/, 'ws');
})();

export function openLogStream(dockerId: string, sinceMs?: number): WebSocket {
  const qs = sinceMs ? `?since_ms=${sinceMs}` : '';
  return new WebSocket(`${wsBase}/containers/${dockerId}/logs/stream${qs}`);
}
