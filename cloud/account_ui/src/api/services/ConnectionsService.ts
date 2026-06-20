import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface Connection {
  id: string;
  name: string;
  tunnel_id: string;
  created_at: string;
  updated_at: string;
}

export interface CreateConnectionRequest {
  name: string;
  tunnel_id: string;
}

export interface PatchConnectionRequest {
  name?: string;
}

// -- Service -------------------------------------------------------------------

export const connectionsService = {
  list: (params?: { name?: string; tunnel_id?: string }) => {
    const qs = new URLSearchParams();
    if (params?.name) qs.set('name', params.name);
    if (params?.tunnel_id) qs.set('tunnel_id', params.tunnel_id);
    const q = qs.toString();
    return apiClient.get<ListResponse<Connection>>(`/connections${q ? `?${q}` : ''}`);
  },
  getById: (id: string) => apiClient.get<Connection>(`/connections/${id}`),
  create: (req: CreateConnectionRequest) => apiClient.post<Connection>('/connections', req),
  update: (id: string, req: PatchConnectionRequest) =>
    apiClient.patch<Connection>(`/connections/${id}`, req),
  delete: (id: string) => apiClient.delete<void>(`/connections/${id}`),
};
