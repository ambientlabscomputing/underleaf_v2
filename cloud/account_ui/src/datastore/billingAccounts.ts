import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { billingAccountsService, type CreateBillingAccountRequest, type PatchBillingAccountRequest } from '../api/services/BillingAccountsService';

export const billingAccountKeys = {
  all: ['billing-accounts'] as const,
  list: (params?: { name?: string }) => [...billingAccountKeys.all, params] as const,
  byId: (id: string) => [...billingAccountKeys.all, id] as const,
};

export function useBillingAccounts(params?: { name?: string }) {
  return useQuery({
    queryKey: billingAccountKeys.list(params),
    queryFn: () => billingAccountsService.list(params),
  });
}

/** A principal account has exactly one billing account, so "the current
 * billing account" is simply the first (only) one the API returns. */
export function useCurrentBillingAccount() {
  const query = useBillingAccounts();
  return { ...query, billingAccount: query.data?.items[0] };
}

export const usageKeys = {
  byBillingAccount: (id: string) => ['usage', id] as const,
};

export function useUsage(billingAccountId: string | undefined) {
  return useQuery({
    queryKey: usageKeys.byBillingAccount(billingAccountId ?? ''),
    queryFn: () => billingAccountsService.usage(billingAccountId as string),
    enabled: !!billingAccountId,
    // Usage moves while streams run; keep the panel roughly live.
    refetchInterval: 30_000,
  });
}

export function useBuyTopUp(billingAccountId: string | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => billingAccountsService.buyTopUp(billingAccountId as string),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['usage'] }),
  });
}

export function useCreateBillingAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateBillingAccountRequest) => billingAccountsService.create(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: billingAccountKeys.all }),
  });
}

export function useUpdateBillingAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, req }: { id: string; req: PatchBillingAccountRequest }) =>
      billingAccountsService.update(id, req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: billingAccountKeys.all });
      qc.invalidateQueries({ queryKey: ['usage'] });
    },
  });
}

export function useDeleteBillingAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => billingAccountsService.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: billingAccountKeys.all }),
  });
}
