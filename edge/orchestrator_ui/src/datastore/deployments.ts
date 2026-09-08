import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { deploymentsService, type CreateDeploymentRequest } from '../api/services/DeploymentsService';

export const deploymentKeys = {
  all: ['deployments'] as const,
  byId: (id: string) => [...deploymentKeys.all, id] as const,
};

// Polls every 2s (matching `orcli deploy`'s poll interval) while any
// deployment is still reconciling, and stops once nothing is in progress.
// refetchIntervalInBackground keeps this going even if the tab isn't
// focused — a user who kicks off a deploy and switches away should still
// see it update when they come back, not find polling silently paused.
export function useDeployments() {
  return useQuery({
    queryKey: deploymentKeys.all,
    queryFn: () => deploymentsService.getDeployments(),
    refetchInterval: (query) =>
      query.state.data?.some((d) => d.status === 'in_progress') ? 2000 : false,
    refetchIntervalInBackground: true,
  });
}

export function useDeployment(id: string) {
  return useQuery({
    queryKey: deploymentKeys.byId(id),
    queryFn: () => deploymentsService.getDeployment(id),
    enabled: !!id,
    refetchInterval: (query) => (query.state.data?.status === 'in_progress' ? 2000 : false),
    refetchIntervalInBackground: true,
  });
}

export function useCreateDeployment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateDeploymentRequest) => deploymentsService.createDeployment(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: deploymentKeys.all });
    },
  });
}
