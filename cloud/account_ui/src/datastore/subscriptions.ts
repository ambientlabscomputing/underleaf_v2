import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { subscriptionsService, type CreateSubscriptionRequest, type PatchSubscriptionRequest, type SubscriptionTier } from '../api/services/SubscriptionsService';

export const subscriptionKeys = {
  all: ['subscriptions'] as const,
  list: (params?: { billing_account_id?: string; tier?: SubscriptionTier }) => [...subscriptionKeys.all, params] as const,
};

export function useSubscriptions(params?: { billing_account_id?: string; tier?: SubscriptionTier }) {
  return useQuery({
    queryKey: subscriptionKeys.list(params),
    queryFn: () => subscriptionsService.list(params),
  });
}

export function useCreateSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateSubscriptionRequest) => subscriptionsService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: subscriptionKeys.all }),
  });
}

export function useUpdateSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: PatchSubscriptionRequest }) =>
      subscriptionsService.update(id, req),
    onSuccess: () => qc.invalidateQueries({ queryKey: subscriptionKeys.all }),
  });
}

export function useDeleteSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => subscriptionsService.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: subscriptionKeys.all }),
  });
}
