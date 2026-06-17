import { apiClient } from '../client';

export interface Container {
  id: string;
  docker_id: string;
  node_id: string;
  image: string;
  status: string;
  uptime: number;
}

interface GetContainersResponse {
  results: Container[];
}

export const containersService = {
  getContainers: () =>
    apiClient.get<GetContainersResponse>('/containers').then((r) => r.results),
  getContainersByNode: (nodeId: string) =>
    apiClient
      .get<GetContainersResponse>(`/nodes/${nodeId}/containers`)
      .then((r) => r.results),
};
