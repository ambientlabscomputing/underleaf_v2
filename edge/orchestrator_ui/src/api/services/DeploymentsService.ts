import { apiClient } from '../client';

export interface VolumeSpec {
  name: string;
}

export interface NetworkSpec {
  name: string;
  driver?: string;
}

export interface BuildSpec {
  context: string;
  dockerfile: string;
  args?: Record<string, string>;
}

export interface ExposeSpec {
  port: number;
  hostname?: string;
}

export interface SourceRef {
  owner: string;
  repo: string;
  ref: string;
  archive_url: string;
}

export interface ServiceSpec {
  name: string;
  image?: string;
  build?: BuildSpec;
  ports?: string[];
  environment?: Record<string, string>;
  networks?: string[];
  volumes?: string[];
  expose?: ExposeSpec;
  source?: SourceRef;
}

export interface DeploymentSpec {
  version: string;
  name: string;
  slug?: string;
  services: ServiceSpec[];
  networks?: NetworkSpec[];
  volumes?: VolumeSpec[];
}

export type DeploymentStatus = 'in_progress' | 'succeeded' | 'failed';

export interface Deployment {
  id: string;
  repo: string;
  ref: string;
  spec: DeploymentSpec;
  status: DeploymentStatus;
  error?: string; // populated when status is "failed"
}

export interface CreateDeploymentRequest {
  source: string; // "gh:<owner>/<repo>[@ref]"
  ref?: string;
  token?: string;
}

interface GetDeploymentsResponse {
  results: Deployment[];
}

export const deploymentsService = {
  getDeployments: () =>
    apiClient.get<GetDeploymentsResponse>('/deployments').then((r) => r.results),
  getDeployment: (id: string) => apiClient.get<Deployment>(`/deployments/${id}`),
  // Reconcile runs in the background — the returned deployment's status is
  // always "in_progress"; poll getDeployment(s) for the outcome.
  createDeployment: (req: CreateDeploymentRequest) =>
    apiClient.post<Deployment>('/deployments', req),
};
