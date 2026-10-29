import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface Plan {
  id: string;
  name: string;
  stripe_product_id: string | null;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface PlanPrice {
  id: string;
  plan_id: string;
  stripe_price_id: string | null;
  interval: string;
  unit_amount_cents: number;
  currency: string;
  is_active: boolean;
  network_traffic_limit_bytes: number;
  connection_slot_limit: number;
  grace_bytes: number;
  top_up_block_bytes: number;
  top_up_price_cents: number;
  created_at: string;
  updated_at: string;
}

// -- Service -------------------------------------------------------------------

export const plansService = {
  list: (params?: { is_active?: boolean }) => {
    const qs = params?.is_active !== undefined ? `?is_active=${params.is_active}` : '';
    return apiClient.get<ListResponse<Plan>>(`/plans${qs}`);
  },
  getById: (id: string) => apiClient.get<Plan>(`/plans/${id}`),
};

export const planPricesService = {
  list: (params?: { plan_id?: string; is_active?: boolean }) => {
    const qs = new URLSearchParams();
    if (params?.plan_id) qs.set('plan_id', params.plan_id);
    if (params?.is_active !== undefined) qs.set('is_active', String(params.is_active));
    const q = qs.toString();
    return apiClient.get<ListResponse<PlanPrice>>(`/plan-prices${q ? `?${q}` : ''}`);
  },
  getById: (id: string) => apiClient.get<PlanPrice>(`/plan-prices/${id}`),
};
