import re
import time
import uuid

import pytest
from playwright.sync_api import Page, expect

# Stripe's published sandbox card: always succeeds, never moves real money.
TEST_CARD = "4242 4242 4242 4242"

# Stripe has to deliver checkout.session.completed to cloud_api (via the
# stripe_cli listener) before the plan flips, so allow for that hop.
_WEBHOOK_TIMEOUT_S = 60


def _pay_on_stripe_checkout(page: Page, email: str) -> None:
    """Complete Stripe's hosted Checkout page in the sandbox."""
    page.wait_for_url(re.compile(r"https://checkout\.stripe\.com/"), timeout=30_000)
    # Sandbox sessions are cs_test_...; refuse to type a card into anything else.
    assert "/cs_test_" in page.url or "/c/pay/cs_test_" in page.url, page.url

    page.get_by_label("Email").fill(email)
    # Checkout lists payment methods as an accordion; the card fields only
    # exist once "Card" is selected.
    # The radio is decorative with no role, and an overlay button (which
    # Playwright counts as not visible) takes the real click. force=True lands
    # the same pointer click a user's would, on that overlay.
    page.locator("#payment-method-accordion-item-title-card").click(force=True)
    # By id: labels like "CVC" also match the card-brand icons.
    page.locator("#cardNumber").fill(TEST_CARD)
    page.locator("#cardExpiry").fill("12 / 34")
    page.locator("#cardCvc").fill("123")
    page.locator("#billingName").fill("E2E Tester")
    page.locator("#billingPostalCode").fill("94107")
    # Checked by default, and it makes a phone number mandatory, which blocks
    # the submit; a test has no reason to enroll in Link anyway.
    page.get_by_role("checkbox", name=re.compile("Save my information")).uncheck()
    page.get_by_test_id("hosted-payment-submit-button").click()


def test_signup_then_subscribe_activates_plan(page: Page, stack, stripe_catalog):
    """A new customer signs up, pays for Maker in Stripe's sandbox, and the
    webhook flips their account to the paid plan."""
    email = f"e2e-{uuid.uuid4().hex[:10]}@example.com"
    password = f"pw-{uuid.uuid4().hex}"

    # -- Account creation -------------------------------------------------
    page.goto("/signup")
    page.get_by_label("Name").fill("E2E Tester")
    page.get_by_label("Email").fill(email)
    page.get_by_label("Password").fill(password)
    page.get_by_role("button", name="Create account").click()
    expect(page).to_have_url(re.compile(r"/billing-accounts$"))

    # -- Free tier to start with -----------------------------------------
    page.goto("/plans")
    maker = page.locator(".MuiCard-root").filter(
        has_text=re.compile("maker", re.IGNORECASE)
    )
    expect(maker.get_by_role("button", name="Subscribe")).to_be_enabled()

    # -- Subscribe via Stripe Checkout ------------------------------------
    maker.get_by_role("button", name="Subscribe").click()
    _pay_on_stripe_checkout(page, email)
    page.wait_for_url(re.compile(r"/plans\?checkout=success"), timeout=60_000)

    # -- Webhook activates the plan --------------------------------------
    # The redirect can beat Stripe's webhook, so reload until the plan flips.
    # Each check waits for the page to render (expect), not just to load.
    maker = page.locator(".MuiCard-root").filter(
        has_text=re.compile("maker", re.IGNORECASE)
    )
    deadline = time.monotonic() + _WEBHOOK_TIMEOUT_S
    while True:
        page.goto("/plans")
        try:
            expect(maker.get_by_role("button", name="Current plan")).to_be_visible(
                timeout=5_000
            )
            break
        except AssertionError:
            if time.monotonic() > deadline:
                pytest.fail(
                    f"Maker never became the current plan within {_WEBHOOK_TIMEOUT_S}s "
                    "of paying. Check `make stripe-logs` for the webhook delivery."
                )
    expect(maker.get_by_text("Current", exact=True)).to_be_visible()
