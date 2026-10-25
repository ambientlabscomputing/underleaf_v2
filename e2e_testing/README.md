# E2E Testing

## Structure

- Fixtures: pytest fixtures for providing all needed dependencies
- Tests: actual tests following the tested app's structure as best as possible

e.g.,

- fixtures
    - servers
        - fooApp.py # starts the Foo app API server
- tests
    - fooApp.py
        - api
            - health
                test_health.py # hits the Foo /health/ API

## Golden path (`tests/golden_path/`)

Browser-driven customer journeys, using [Playwright](https://playwright.dev/python/). Unlike the per-service tests above, these don't start anything themselves: they drive the real compose stack the way a customer would (browser → nginx → cloud_api), with Stripe's **sandbox** calling back through the `stripe_cli` webhook listener.

```bash
# from the repo root
make cloud-ui-build stripe-up      # UI bundle + stack + Stripe listener (test-mode keys only)
cd e2e_testing && uv run pytest tests/golden_path --headed   # drop --headed to run headless
```

`test_account_to_subscription.py`: sign up in the Account UI → subscribe to Maker on Stripe's hosted Checkout with the `4242…` test card → wait for the webhook to flip the account to the paid plan. The fixtures fail fast, with the command to run, if the stack, listener or UI bundle isn't ready, and the test refuses to enter a card unless the Checkout session is a sandbox (`cs_test_…`) one.

Playwright is pinned to the release whose Chromium build is already installed; after bumping it run `uv run playwright install chromium`.
