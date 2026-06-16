import { useQuery } from '@tanstack/react-query';
import { nodesService } from '../api/services/NodesService';

export const nodeKeys = {
  all: ['nodes'] as const,
};

export function useNodes() {
  return useQuery({
    queryKey: nodeKeys.all,
    queryFn: () => nodesService.getNodes(),
  });
}
