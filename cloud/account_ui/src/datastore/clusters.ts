import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { clustersService, type CreateClusterRequest, type PatchClusterRequest, type SyncNodesRequest } from '../api/services/ClustersService';

export const clusterKeys = {
  all: ['clusters'] as const,
  list: (params?: { name?: string }) => [...clusterKeys.all, params] as const,
  byId: (id: string) => [...clusterKeys.all, id] as const,
};

export function useClusters(params?: { name?: string }) {
  return useQuery({
    queryKey: clusterKeys.list(params),
    queryFn: () => clustersService.list(params),
  });
}

export function useCreateCluster() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateClusterRequest) => clustersService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: clusterKeys.all }),
  });
}

export function useUpdateCluster() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: PatchClusterRequest }) =>
      clustersService.update(id, req),
    onSuccess: () => qc.invalidateQueries({ queryKey: clusterKeys.all }),
  });
}

export function useSyncNodes() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: SyncNodesRequest }) =>
      clustersService.syncNodes(id, req),
    onSuccess: () => qc.invalidateQueries({ queryKey: clusterKeys.all }),
  });
}

export function useDeleteCluster() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => clustersService.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: clusterKeys.all }),
  });
}
