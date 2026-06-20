import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { registrationService } from '../api/services/RegistrationService';
import type { ApproveCandidateResponse } from '../api/services/RegistrationService';

export const registrationKeys = {
  candidate: (userCode: string) => ['registration', 'candidate', userCode] as const,
};

export function useCandidate(userCode: string | null) {
  return useQuery({
    queryKey: registrationKeys.candidate(userCode ?? ''),
    queryFn: () => registrationService.getCandidate(userCode!),
    enabled: !!userCode,
    retry: false,
  });
}

export function useApproveRegistration() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userCode: string) => registrationService.approve(userCode),
    onSuccess: (_data: ApproveCandidateResponse, userCode: string) => {
      qc.invalidateQueries({ queryKey: registrationKeys.candidate(userCode) });
    },
  });
}
