import { apiClient } from '../client';

export interface ClusterCandidate {
  id: string;
  user_code: string;
  status: 'pending' | 'approved' | 'expired' | 'consumed';
  proposed_cluster_name: string;
  proposed_cluster_id: string;
  principal_account_id: string | null;
  cluster_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface ApproveCandidateResponse {
  cluster: {
    id: string;
    name: string;
    principal_account_id: string;
    created_at: string;
    updated_at: string;
  };
}

export const registrationService = {
  getCandidate: (userCode: string) =>
    apiClient.get<ClusterCandidate>(`/registration/candidate?user_code=${encodeURIComponent(userCode)}`),

  approve: (userCode: string) =>
    apiClient.post<ApproveCandidateResponse>('/registration/approve', { user_code: userCode }),
};
