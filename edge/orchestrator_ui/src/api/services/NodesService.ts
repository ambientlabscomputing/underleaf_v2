import { apiClient } from '../client';

export interface Node {
  id: string;
  name: string;
}

interface GetNodesResponse {
  results: Node[];
}

export const nodesService = {
  getNodes: () => apiClient.get<GetNodesResponse>('/nodes').then((r) => r.results),
};
