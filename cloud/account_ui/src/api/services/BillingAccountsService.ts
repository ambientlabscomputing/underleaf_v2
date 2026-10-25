import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface BillingAccount {
  id: string;
  name: string;
  principal_account_id: string;
  stripe_customer_id: string | null;
  stripe_data: Record<string, unknown>;
  auto_top_up_enabled: boolean;
  monthly_top_up_cap_cents: number;
  created_at: string;
  updated_at: string;
}

export interface CreateBillingAccountRequest {
  name: string;
  principal_account_id: string;
}

// Stripe identifiers are deliberately not patchable from the client.
export interface PatchBillingAccountRequest {
  name?: string;
  auto_top_up_enabled?: boolean;
  monthly_top_up_cap_cents?: number;
}

export type UsageAlertKind =
  | 'usage_80'
  | 'usage_95'
  | 'usage_100'
  | 'top_up_cap_reached'
  | 'top_up_failed';

export interface UsageAlert {
  id: string;
  kind: UsageAlertKind;
  created_at: string;
}

export interface UsageSummary {
  billing_account_id: string;
  tier: 'free' | 'maker' | 'builder' | 'studio' | 'fleet';
  paid: boolean;
  period_start: string | null;
  period_end: string | null;
  allowance_bytes: number;
  used_bytes: number;
  remaining_bytes: number;
  percent_used: number;
  grace_policy_bytes: number;
  grace_remaining_bytes: number;
  grace_active: boolean;
  blocked: boolean;
  connection_slot_limit: number;
  connection_slots_remaining: number;
  auto_top_up_enabled: boolean;
  monthly_top_up_cap_cents: number;
  top_up_spent_cents: number;
  top_up_block_bytes: number;
  top_up_price_cents: number;
  can_top_up: boolean;
  alerts: UsageAlert[];
}

export interface TopUp {
  id: string;
  bytes: number;
  amount_cents: number;
  status: 'pending' | 'paid' | 'failed';
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
  usage: (id: string) => apiClient.get<UsageSummary>(`/billing-accounts/${id}/usage`),
  buyTopUp: (id: string) => apiClient.post<TopUp>(`/billing-accounts/${id}/top-ups`, {}),
};
