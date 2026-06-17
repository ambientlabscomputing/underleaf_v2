import { useQuery } from '@tanstack/react-query';
import { containersService } from '../api/services/ContainersService';

export const containerKeys = {
  all: ['containers'] as const,
  byNode: (nodeId: string) => [...containerKeys.all, nodeId] as const,
};

export function useContainers() {
  return useQuery({
    queryKey: containerKeys.all,
    queryFn: () => containersService.getContainers(),
  });
}

export function useContainersByNode(nodeId: string) {
  return useQuery({
    queryKey: containerKeys.byNode(nodeId),
    queryFn: () => containersService.getContainersByNode(nodeId),
  });
}
