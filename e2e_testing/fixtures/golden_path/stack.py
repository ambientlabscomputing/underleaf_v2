import subprocess

import pytest
import requests

# The golden path goes through the compose stack the way a customer would:
# the browser talks to nginx, nginx to cloud_api, and Stripe's sandbox talks
# back through the `stripe_cli` webhook listener. Nothing here starts the
# stack -- it's heavy and slow -- it checks it and says how to bring it up.
STACK_URL = "https://localhost"

_COMPOSE = ["docker", "compose", "--profile", "stripe"]


def _compose(test_config, *args, check=True):
    return subprocess.run(
        [*_COMPOSE, *args],
        cwd=test_config.repo_root_dir,
        capture_output=True,
        text=True,
        check=check,
    )


@pytest.fixture(scope="session")
def base_url():
    return STACK_URL


@pytest.fixture(scope="session")
def browser_context_args(browser_context_args):
    # nginx terminates TLS with the stack's self-signed root CA.
    return {**browser_context_args, "ignore_https_errors": True}


@pytest.fixture(scope="session")
def stack(test_config):
    """The compose stack with the Stripe listener, ready to serve the UI."""
    try:
        health = requests.get(
            f"{STACK_URL}/api/v2/cloud/health", verify=False, timeout=3
        )
        health.raise_for_status()
    except requests.RequestException as exc:
        pytest.fail(
            f"Stack not reachable at {STACK_URL} ({exc}). "
            "Run `make cloud-ui-build stripe-up` from the repo root first."
        )

    running = _compose(test_config, "ps", "--status", "running", "--services")
    if "stripe_cli" not in running.stdout.split():
        pytest.fail(
            "The stripe_cli listener is not running, so Stripe's webhooks can't "
            "reach cloud_api. Run `make stripe-up` from the repo root."
        )

    ui = requests.get(f"{STACK_URL}/signup", verify=False, timeout=3)
    bundle = "".join(
        requests.get(f"{STACK_URL}{src}", verify=False, timeout=10).text
        for src in _bundle_paths(ui.text)
    )
    if "localhost:8080" in bundle:
        # Built without VITE_API_URL: its API calls target a port the compose
        # stack deliberately doesn't publish.
        pytest.fail("The served UI bundle is stale. Run `make cloud-ui-build`.")


def _bundle_paths(html: str) -> list[str]:
    import re

    return re.findall(r'src="(/assets/[^"]+\.js)"', html)


@pytest.fixture(scope="session")
def stripe_catalog(stack, test_config):
    """Paid plans linked to Stripe sandbox Prices (idempotent)."""
    result = _compose(
        test_config,
        "exec",
        "-T",
        "cloud_api",
        "python",
        "-m",
        "cloud_api.scripts.sync_stripe_catalog",
        check=False,
    )
    if result.returncode != 0:
        pytest.fail(f"Stripe catalog sync failed:\n{result.stdout}\n{result.stderr}")
    return result.stdout
