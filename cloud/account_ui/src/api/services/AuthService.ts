import { BASE_URL, setTokens } from '../client';

// -- Types ---------------------------------------------------------------------

export interface User {
  id: string;
  name: string;
  email: string;
  principal_account_id: string;
  created_at: string;
  updated_at: string;
}

export interface PrincipalAccount {
  id: string;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface BillingAccountBasic {
  id: string;
  name: string;
  principal_account_id: string;
  created_at: string;
  updated_at: string;
}

export interface SubscriptionBasic {
  id: string;
  billing_account_id: string;
  tier: 'free' | 'builder' | 'pro';
  created_at: string;
  updated_at: string;
}

export interface TokenResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token: string;
}

export interface SignUpRequest {
  name: string;
  email: string;
  password: string;
}

export interface SignUpResponse {
  user: User;
  principal_account: PrincipalAccount;
  billing_account: BillingAccountBasic;
  subscription: SubscriptionBasic;
}

// -- Helpers -------------------------------------------------------------------

const OAUTH_BASE = BASE_URL

// -- Service -------------------------------------------------------------------

export const authService = {
  /** Password grant — stores tokens and returns the decoded user. */
  login: async (email: string, password: string): Promise<TokenResponse> => {
    const res = await fetch(`${OAUTH_BASE}/oauth/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ grant_type: 'password', username: email, password }),
    });

    if (!res.ok) {
      const err = await res.json().catch(() => ({})) as { detail?: string };
      throw new Error(err.detail ?? 'Login failed');
    }

    const data = await res.json() as TokenResponse;
    setTokens(data.access_token, data.refresh_token);
    return data;
  },

  /** GET /oauth/userinfo — returns the current user. */
  userinfo: async (): Promise<User> => {
    const token = localStorage.getItem('access_token');
    const res = await fetch(`${OAUTH_BASE}/oauth/userinfo`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) throw new Error('Could not load user info');
    return res.json() as Promise<User>;
  },

  /** POST /api/v2/accounts/signup */
  signup: async (req: SignUpRequest): Promise<SignUpResponse> => {
    const res = await fetch(`${BASE_URL}/accounts/signup`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({})) as { detail?: string };
      throw new Error(err.detail ?? 'Sign-up failed');
    }
    return res.json() as Promise<SignUpResponse>;
  },
};
