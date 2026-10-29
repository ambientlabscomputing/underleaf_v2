import { apiClient } from '../client';
import type { ListResponse } from './Common';

// -- Types ---------------------------------------------------------------------

export interface Invoice {
  id: string;
  billing_account_id: string;
  stripe_invoice_id: string;
  status: string;
  amount_due_cents: number;
  amount_paid_cents: number;
  currency: string;
  hosted_invoice_url: string | null;
  invoice_pdf_url: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateCheckoutSessionRequest {
  plan_price_id: string;
}

export interface CreateCheckoutSessionResponse {
  url: string;
}

// -- Service -------------------------------------------------------------------
// Read-only mirror of Stripe invoices, and the checkout-session kickoff --
// both scoped under a billing account, matching the backend's REST shape
// (billing_accounts.py: GET .../invoices, POST .../checkout-session).

export const invoicesService = {
  list: (billingAccountId: string) =>
    apiClient.get<ListResponse<Invoice>>(`/billing-accounts/${billingAccountId}/invoices`),
  createCheckoutSession: (billingAccountId: string, req: CreateCheckoutSessionRequest) =>
    apiClient.post<CreateCheckoutSessionResponse>(
      `/billing-accounts/${billingAccountId}/checkout-session`,
      req,
    ),
};
