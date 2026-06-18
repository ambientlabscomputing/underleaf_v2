import { apiClient } from '../client';
import type { BaseQueryRequest, BaseQueryResponse } from './Common';

export interface Node {
  id: string;
  name: string;
  ip_address: string;
  os: string;
  arch: string;
}

export interface GetNodesRequest extends BaseQueryRequest {
    name?: string;
    os?: string;
    arch?: string;
    search?: string;
}

export interface GetNodesResponse extends BaseQueryResponse {
  results: Node[];
}

export const nodesService = {
  getNodes: (params?: GetNodesRequest) => {
    const qs = new URLSearchParams();
    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        if (v !== undefined && v !== null && v !== '') {
          qs.set(k, String(v));
        }
      });
    }
    const query = qs.toString();
    return apiClient.get<GetNodesResponse>(`/nodes${query ? `?${query}` : ''}`).then((r) => r.results ?? []);
  },
};
