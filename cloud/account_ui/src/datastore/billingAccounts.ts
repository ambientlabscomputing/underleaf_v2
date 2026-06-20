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
    onSuccess: () => qc.invalidateQueries({ queryKey: billingAccountKeys.all }),
  });
}

export function useDeleteBillingAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => billingAccountsService.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: billingAccountKeys.all }),
  });
}
