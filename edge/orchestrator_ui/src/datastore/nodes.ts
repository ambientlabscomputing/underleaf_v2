import { useQuery } from '@tanstack/react-query';
import { nodesService, type GetNodesRequest } from '../api/services/NodesService';

export const nodeKeys = {
  all: ['nodes'] as const,
  list: (params?: GetNodesRequest) => [...nodeKeys.all, params] as const,
};

export function useNodes(params?: GetNodesRequest) {
  return useQuery({
    queryKey: nodeKeys.list(params),
    queryFn: () => nodesService.getNodes(params),
  });
}
