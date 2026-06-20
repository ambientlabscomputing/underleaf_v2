import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface ClusterNode {
  id: string;
  name: string;
  cluster_id: string;
  created_at: string;
  updated_at: string;
}

export interface Cluster {
  id: string;
  name: string;
  principal_account_id: string;
  created_at: string;
  updated_at: string;
}

export interface CreateClusterRequest {
  id: string;
  name: string;
}

export interface PatchClusterRequest {
  name?: string;
}

export interface SyncNodesRequest {
  nodes: ClusterNode[];
}

// -- Service -------------------------------------------------------------------

export const clustersService = {
  list: (params?: { name?: string }) => {
    const qs = params?.name ? `?name=${encodeURIComponent(params.name)}` : '';
    return apiClient.get<ListResponse<Cluster>>(`/clusters${qs}`);
  },
  getById: (id: string) => apiClient.get<Cluster>(`/clusters/${id}`),
  create: (req: CreateClusterRequest) => apiClient.post<Cluster>('/clusters', req),
  update: (id: string, req: PatchClusterRequest) =>
    apiClient.patch<Cluster>(`/clusters/${id}`, req),
  syncNodes: (id: string, req: SyncNodesRequest) =>
    apiClient.post<Cluster>(`/clusters/${id}/nodes/sync`, req),
  delete: (id: string) => apiClient.delete<void>(`/clusters/${id}`),
};
