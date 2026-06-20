import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { tunnelsService, type CreateTunnelRequest, type PatchTunnelRequest } from '../api/services/TunnelsService';

export const tunnelKeys = {
  all: ['tunnels'] as const,
  list: (params?: { name?: string; node_id?: string }) => [...tunnelKeys.all, params] as const,
};

export function useTunnels(params?: { name?: string; node_id?: string }) {
  return useQuery({
    queryKey: tunnelKeys.list(params),
    queryFn: () => tunnelsService.list(params),
  });
}

export function useCreateTunnel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateTunnelRequest) => tunnelsService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: tunnelKeys.all }),
  });
}

export function useUpdateTunnel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: PatchTunnelRequest }) =>
      tunnelsService.update(id, req),
    onSuccess: () => qc.invalidateQueries({ queryKey: tunnelKeys.all }),
  });
}

export function useDeleteTunnel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => tunnelsService.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: tunnelKeys.all }),
  });
}
