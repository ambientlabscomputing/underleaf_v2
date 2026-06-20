import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface BillingAccount {
  id: string;
  name: string;
  principal_account_id: string;
  stripe_customer_id: string | null;
  stripe_data: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface CreateBillingAccountRequest {
  name: string;
  principal_account_id: string;
}

export interface PatchBillingAccountRequest {
  name?: string;
  stripe_customer_id?: string;
  stripe_data?: Record<string, unknown>;
}

// -- Service -------------------------------------------------------------------

export const billingAccountsService = {
  list: (params?: { name?: string }) => {
    const qs = params?.name ? `?name=${encodeURIComponent(params.name)}` : '';
    return apiClient.get<ListResponse<BillingAccount>>(`/billing-accounts${qs}`);
  },
  getById: (id: string) => apiClient.get<BillingAccount>(`/billing-accounts/${id}`),
  create: (req: CreateBillingAccountRequest) =>
    apiClient.post<BillingAccount>('/billing-accounts', req),
  update: (id: string, req: PatchBillingAccountRequest) =>
    apiClient.patch<BillingAccount>(`/billing-accounts/${id}`, req),
  delete: (id: string) => apiClient.delete<void>(`/billing-accounts/${id}`),
};
