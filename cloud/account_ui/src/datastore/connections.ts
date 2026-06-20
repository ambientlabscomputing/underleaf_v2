import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { connectionsService, type CreateConnectionRequest, type PatchConnectionRequest } from '../api/services/ConnectionsService';

export const connectionKeys = {
  all: ['connections'] as const,
  list: (params?: { name?: string; tunnel_id?: string }) => [...connectionKeys.all, params] as const,
};

export function useConnections(params?: { name?: string; tunnel_id?: string }) {
  return useQuery({
    queryKey: connectionKeys.list(params),
    queryFn: () => connectionsService.list(params),
  });
}

export function useCreateConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateConnectionRequest) => connectionsService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: connectionKeys.all }),
  });
}

export function useUpdateConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: PatchConnectionRequest }) =>
      connectionsService.update(id, req),
    onSuccess: () => qc.invalidateQueries({ queryKey: connectionKeys.all }),
  });
}

export function useDeleteConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => connectionsService.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: connectionKeys.all }),
  });
}
