

// Same-origin by default: the Orchestrator serves this UI and the API from one
// origin (and `npm run dev` proxies /api to it), so no CORS is involved.
const BASE_URL = import.meta.env.VITE_API_URL ?? '/api/v1';

export class ApiError extends Error {
    status: number;
    data?: unknown;

    constructor(status: number, message: string, data?: unknown) {
        super(message);
        this.status = status;
        this.data = data;
    }
}

async function request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
    // const token = localStorage.getItem('auth_token');
    const token = "TOKEN";

    const headers: HeadersInit = {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...options.headers,
    };

    const response = await fetch(`${BASE_URL}${endpoint}`, { ...options, headers });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({})) as { message?: string; error?: string };
        throw new ApiError(response.status, errorData.message ?? errorData.error ?? 'Network error occurred', errorData);
    }

    return response.json() as Promise<T>;
}

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
