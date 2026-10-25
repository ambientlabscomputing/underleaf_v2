"""The customer-facing API must not let a tenant edit its own entitlements,
usage records, or the plan catalog, and money-moving endpoints must check
ownership."""

import pytest
from fastapi import HTTPException

import cloud_api
from cloud_api.interface.billing_accounts.billing_accounts import _owned_account
from cloud_api.interface.router import app
from cloud_api.models.api import BillingAccount, PatchBillingAccountRequest
from cloud_api.service.billing_account_service import BillingAccountNotFoundError


def _routes():
    return {
        (method, r.path)
        for r in app.routes
        if hasattr(r, "methods")
        for method in r.methods
    }


@pytest.mark.parametrize("path", ["/entitlements-buckets", "/usage-events"])
def test_entitlements_and_usage_events_are_not_exposed_to_customers(path):
    assert not any(path in p for _, p in _routes())


@pytest.mark.parametrize("prefix", ["/plans", "/plan-prices"])
def test_plan_catalog_is_read_only(prefix):
    methods = {m for m, p in _routes() if f"/api/v2/cloud{prefix}" in p}
    assert methods == {"GET"}


def test_customer_patch_cannot_set_stripe_identifiers():
    assert "stripe_customer_id" not in PatchBillingAccountRequest.model_fields
    assert "stripe_data" not in PatchBillingAccountRequest.model_fields
    assert {"auto_top_up_enabled", "monthly_top_up_cap_cents"} <= set(
        PatchBillingAccountRequest.model_fields
    )


def test_money_moving_endpoints_are_registered():
    routes = _routes()
    base = "/api/v2/cloud/billing-accounts/{billing_account_id}"
    assert ("GET", f"{base}/usage") in routes
    assert ("POST", f"{base}/top-ups") in routes
    assert ("POST", "/api/v2/cloud/stripe/webhooks") in routes


class _Service:
    def __init__(self, account=None):
        self.account = account

    async def get_billing_account(self, _id):
        if self.account is None:
            raise BillingAccountNotFoundError(_id)
        return self.account


class _Claims:
    azp = "principal_mine"


@pytest.mark.asyncio
async def test_owned_account_returns_the_callers_own_account():
    acct = BillingAccount(id="b", name="n", principal_account_id="principal_mine")
    assert await _owned_account("b", _Claims, _Service(acct)) is acct


@pytest.mark.asyncio
async def test_foreign_account_is_indistinguishable_from_missing():
    foreign = BillingAccount(id="b", name="n", principal_account_id="principal_other")
    for service in (_Service(foreign), _Service(None)):
        with pytest.raises(HTTPException) as exc:
            await _owned_account("b", _Claims, service)
        assert exc.value.status_code == 404


def test_stripe_secrets_can_come_from_the_environment(monkeypatch):
    monkeypatch.setenv("UNDERLEAF_STRIPE_API_KEY", "sk_test_from_env")
    monkeypatch.setenv("UNDERLEAF_STRIPE_WEBHOOK_SECRET", "whsec_from_env")
    cfg = cloud_api.load_config()
    assert cfg.stripe.api_key == "sk_test_from_env"
    assert cfg.stripe.webhook_secret == "whsec_from_env"
