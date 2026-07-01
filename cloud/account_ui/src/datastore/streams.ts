import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { streamsService, type CreateStreamRequest, type PatchStreamRequest } from '../api/services/StreamsService';

export const streamKeys = {
  all: ['streams'] as const,
  list: (params?: { name?: string; node_id?: string }) => [...streamKeys.all, params] as const,
};

export function useStreams(params?: { name?: string; node_id?: string }) {
  return useQuery({
    queryKey: streamKeys.list(params),
    queryFn: () => streamsService.list(params),
  });
}

export function useCreateStream() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateStreamRequest) => streamsService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: streamKeys.all }),
  });
}

export function useUpdateStream() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: PatchStreamRequest }) =>
      streamsService.update(id, req),
    onSuccess: () => qc.invalidateQueries({ queryKey: streamKeys.all }),
  });
}

export function useDeleteStream() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => streamsService.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: streamKeys.all }),
  });
}
