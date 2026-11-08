import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { ufctlService } from '../api/services/UfctlService';

export const ufctlKeys = {
  request: (userCode: string) => ['ufctl', 'request', userCode] as const,
  devices: ['ufctl', 'devices'] as const,
};

export function useDeviceLoginRequest(userCode: string | null) {
  return useQuery({
    queryKey: ufctlKeys.request(userCode ?? ''),
    queryFn: () => ufctlService.getRequest(userCode!),
    enabled: !!userCode,
    retry: false,
  });
}

export function useApproveDeviceLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userCode: string) => ufctlService.approve(userCode),
    onSuccess: (_data, userCode) => {
      qc.invalidateQueries({ queryKey: ufctlKeys.request(userCode) });
      qc.invalidateQueries({ queryKey: ufctlKeys.devices });
    },
  });
}

export function useDenyDeviceLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userCode: string) => ufctlService.deny(userCode),
    onSuccess: (_data, userCode) => {
      qc.invalidateQueries({ queryKey: ufctlKeys.request(userCode) });
    },
  });
}

export function useDeviceKeys() {
  return useQuery({
    queryKey: ufctlKeys.devices,
    queryFn: () => ufctlService.listDevices(),
  });
}

export function useRevokeDeviceKey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => ufctlService.revokeDevice(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ufctlKeys.devices }),
  });
}
