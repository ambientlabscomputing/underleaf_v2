import { useQuery } from '@tanstack/react-query';
import { plansService, planPricesService } from '../api/services/PlansService';

export const planKeys = {
  all: ['plans'] as const,
  list: (params?: { is_active?: boolean }) => [...planKeys.all, params] as const,
};

export const planPriceKeys = {
  all: ['plan-prices'] as const,
  list: (params?: { plan_id?: string; is_active?: boolean }) => [...planPriceKeys.all, params] as const,
};

export function usePlans(params?: { is_active?: boolean }) {
  return useQuery({
    queryKey: planKeys.list(params),
    queryFn: () => plansService.list(params),
  });
}

export function usePlanPrices(params?: { plan_id?: string; is_active?: boolean }) {
  return useQuery({
    queryKey: planPriceKeys.list(params),
    queryFn: () => planPricesService.list(params),
  });
}
