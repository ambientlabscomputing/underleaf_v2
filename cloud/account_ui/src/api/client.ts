// -- Token helpers -------------------------------------------------------------
// Centralised so auth/ can also call these without a circular dep.
export function getAccessToken(): string | null {
  return localStorage.getItem('access_token');
}

export function getRefreshToken(): string | null {
  return localStorage.getItem('refresh_token');
}

export function setTokens(access: string, refresh: string): void {
  localStorage.setItem('access_token', access);
  localStorage.setItem('refresh_token', refresh);
}

export function clearTokens(): void {
  localStorage.removeItem('access_token');
  localStorage.removeItem('refresh_token');
}

// -- Base URL ------------------------------------------------------------------
export const BASE_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v2';

// -- Error type ----------------------------------------------------------------
export class ApiError extends Error {
  status: number;
  data?: unknown;

  constructor(status: number, message: string, data?: unknown) {
    super(message);
    this.status = status;
    this.data = data;
  }
}

// -- Refresh lock — prevents multiple simultaneous refresh calls ---------------
let refreshing: Promise<void> | null = null;

async function attemptRefresh(): Promise<void> {
  const refresh = getRefreshToken();
  if (!refresh) throw new ApiError(401, 'No refresh token');

  const res = await fetch(`${BASE_URL.replace('/api/v2', '')}/oauth/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ grant_type: 'refresh_token', refresh_token: refresh }),
  });

  if (!res.ok) {
    clearTokens();
    throw new ApiError(401, 'Refresh failed');
  }

  const data = await res.json() as { access_token: string; refresh_token: string };
  setTokens(data.access_token, data.refresh_token);
}

// -- Core fetch wrapper --------------------------------------------------------
async function request<T>(endpoint: string, options: RequestInit = {}, retry = true): Promise<T> {
  const token = getAccessToken();

  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...options.headers,
  };

  const response = await fetch(`${BASE_URL}${endpoint}`, { ...options, headers });

  if (response.status === 401 && retry) {
    // Attempt a single token refresh then retry
    if (!refreshing) {
      refreshing = attemptRefresh().finally(() => { refreshing = null; });
    }
    try {
      await refreshing;
    } catch {
      // Refresh failed — clear tokens and redirect to login
      clearTokens();
      window.location.href = '/login';
      throw new ApiError(401, 'Session expired');
    }
    return request<T>(endpoint, options, false);
  }

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({})) as { message?: string; detail?: string };
    const msg = errorData.message ?? errorData.detail ?? `HTTP ${response.status}`;
    throw new ApiError(response.status, msg, errorData);
  }

  // 204 No Content
  if (response.status === 204) return undefined as unknown as T;

  return response.json() as Promise<T>;
}

// -- Public API client ---------------------------------------------------------
export const apiClient = {
  get: <T>(url: string, init?: RequestInit) => request<T>(url, { method: 'GET', ...init }),
  post: <T>(url: string, body: unknown, init?: RequestInit) =>
    request<T>(url, { method: 'POST', body: JSON.stringify(body), ...init }),
  put: <T>(url: string, body: unknown, init?: RequestInit) =>
    request<T>(url, { method: 'PUT', body: JSON.stringify(body), ...init }),
  delete: <T>(url: string, init?: RequestInit) => request<T>(url, { method: 'DELETE', ...init }),
  patch: <T>(url: string, body: unknown, init?: RequestInit) =>
    request<T>(url, { method: 'PATCH', body: JSON.stringify(body), ...init }),
};
