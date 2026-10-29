import { useMutation, useQuery } from '@tanstack/react-query';
import { invoicesService, type CreateCheckoutSessionRequest } from '../api/services/InvoicesService';

export const invoiceKeys = {
  all: ['invoices'] as const,
  byBillingAccount: (billingAccountId: string) => [...invoiceKeys.all, billingAccountId] as const,
};

export function useInvoices(billingAccountId: string | undefined) {
  return useQuery({
    queryKey: invoiceKeys.byBillingAccount(billingAccountId ?? ''),
    queryFn: () => invoicesService.list(billingAccountId as string),
    enabled: !!billingAccountId,
  });
}

export function useCreateCheckoutSession(billingAccountId: string | undefined) {
  return useMutation({
    mutationFn: (req: CreateCheckoutSessionRequest) =>
      invoicesService.createCheckoutSession(billingAccountId as string, req),
  });
}
