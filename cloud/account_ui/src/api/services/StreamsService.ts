import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface Stream {
  id: string;
  name: string;
  node_id: string;
  created_at: string;
  updated_at: string;
}

export interface CreateStreamRequest {
  name: string;
  node_id: string;
}

export interface PatchStreamRequest {
  name?: string;
}

// -- Service -------------------------------------------------------------------

export const streamsService = {
  list: (params?: { name?: string; node_id?: string }) => {
    const qs = new URLSearchParams();
    if (params?.name) qs.set('name', params.name);
    if (params?.node_id) qs.set('node_id', params.node_id);
    const q = qs.toString();
    return apiClient.get<ListResponse<Stream>>(`/streams${q ? `?${q}` : ''}`);
  },
  getById: (id: string) => apiClient.get<Stream>(`/streams/${id}`),
  create: (req: CreateStreamRequest) => apiClient.post<Stream>('/streams', req),
  update: (id: string, req: PatchStreamRequest) =>
    apiClient.patch<Stream>(`/streams/${id}`, req),
  delete: (id: string) => apiClient.delete<void>(`/streams/${id}`),
};
