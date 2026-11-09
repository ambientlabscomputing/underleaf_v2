import { apiClient } from '../client';

// -- Types ---------------------------------------------------------------------

export interface DeviceLoginRequest {
  user_code: string;
  device_name: string;
  status: 'pending' | 'approved' | 'denied' | 'consumed';
  created_at: string;
  expires_at: string;
}

export interface DeviceLoginDecision {
  status: DeviceLoginRequest['status'];
  device_name: string;
}

/** A ufctl device key as shown to its owner. The secret is shown once, to the
 *  device, and is never returned by the API again. */
export interface DeviceKey {
  id: string;
  principal_account_id: string;
  name: string;
  created_at: string;
  updated_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
}

export interface ListDeviceKeysResponse {
  items: DeviceKey[];
  total: number;
}

// -- Service -------------------------------------------------------------------

export const ufctlService = {
  getRequest: (userCode: string) =>
    apiClient.get<DeviceLoginRequest>(`/ufctl/device/request?user_code=${encodeURIComponent(userCode)}`),

  approve: (userCode: string) =>
    apiClient.post<DeviceLoginDecision>('/ufctl/device/approve', { user_code: userCode }),

  deny: (userCode: string) =>
    apiClient.post<DeviceLoginDecision>('/ufctl/device/deny', { user_code: userCode }),

  listDevices: () => apiClient.get<ListDeviceKeysResponse>('/ufctl/devices'),

  revokeDevice: (id: string) => apiClient.delete<void>(`/ufctl/devices/${encodeURIComponent(id)}`),
};
