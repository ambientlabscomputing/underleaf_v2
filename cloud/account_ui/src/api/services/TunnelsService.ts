import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface Tunnel {
  id: string;
  name: string;
  node_id: string;
  created_at: string;
  updated_at: string;
}

export interface CreateTunnelRequest {
  name: string;
  node_id: string;
}

export interface PatchTunnelRequest {
  name?: string;
}

// -- Service -------------------------------------------------------------------

export const tunnelsService = {
  list: (params?: { name?: string; node_id?: string }) => {
    const qs = new URLSearchParams();
    if (params?.name) qs.set('name', params.name);
    if (params?.node_id) qs.set('node_id', params.node_id);
    const q = qs.toString();
    return apiClient.get<ListResponse<Tunnel>>(`/tunnels${q ? `?${q}` : ''}`);
  },
  getById: (id: string) => apiClient.get<Tunnel>(`/tunnels/${id}`),
  create: (req: CreateTunnelRequest) => apiClient.post<Tunnel>('/tunnels', req),
  update: (id: string, req: PatchTunnelRequest) =>
    apiClient.patch<Tunnel>(`/tunnels/${id}`, req),
  delete: (id: string) => apiClient.delete<void>(`/tunnels/${id}`),
};
