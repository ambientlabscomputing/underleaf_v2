import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export type SubscriptionTier = 'free' | 'builder' | 'pro';

export interface Subscription {
  id: string;
  billing_account_id: string;
  tier: SubscriptionTier;
  created_at: string;
  updated_at: string;
}

export interface CreateSubscriptionRequest {
  billing_account_id: string;
  tier: SubscriptionTier;
}

export interface PatchSubscriptionRequest {
  tier?: SubscriptionTier;
}

// -- Service -------------------------------------------------------------------

export const subscriptionsService = {
  list: (params?: { billing_account_id?: string; tier?: SubscriptionTier }) => {
    const qs = new URLSearchParams();
    if (params?.billing_account_id) qs.set('billing_account_id', params.billing_account_id);
    if (params?.tier) qs.set('tier', params.tier);
    const q = qs.toString();
    return apiClient.get<ListResponse<Subscription>>(`/subscriptions${q ? `?${q}` : ''}`);
  },
  getById: (id: string) => apiClient.get<Subscription>(`/subscriptions/${id}`),
  create: (req: CreateSubscriptionRequest) =>
    apiClient.post<Subscription>('/subscriptions', req),
  update: (id: string, req: PatchSubscriptionRequest) =>
    apiClient.patch<Subscription>(`/subscriptions/${id}`, req),
  delete: (id: string) => apiClient.delete<void>(`/subscriptions/${id}`),
};
