import json
from types import SimpleNamespace
from unittest.mock import patch

import pytest

from cloud_api.lib import catalog
from cloud_api.scripts import stripe_cli_catalog as script


def _run(stdout, returncode=0, stderr=""):
    return SimpleNamespace(
        stdout=json.dumps(stdout), stderr=stderr, returncode=returncode
    )


def test_cli_error_bodies_become_readable_errors_not_keyerrors():
    denied = {
        "error": {
            "code": "more_permissions_required",
            "message": "Products Write required",
        }
    }
    with (
        patch.object(script.subprocess, "run", return_value=_run(denied)),
        pytest.raises(RuntimeError, match="Products Write required"),
    ):
        script.stripe_cli(["products", "create"], live=True)


def test_live_flag_is_only_added_when_requested():
    with patch.object(script.subprocess, "run", return_value=_run({"data": []})) as run:
        script.stripe_cli(["prices", "list"], live=False)
        script.stripe_cli(["prices", "list"], live=True)
    assert "--live" not in run.call_args_list[0].args[0]
    assert "--live" in run.call_args_list[1].args[0]


def test_existing_price_is_reused_not_recreated():
    existing = {
        "id": "price_1",
        "product": "prod_1",
        "unit_amount": catalog.MAKER.price_cents,
    }
    with (
        patch.object(script, "find_price", return_value=existing),
        patch.object(script, "stripe_cli") as cli,
    ):
        result = script.ensure_plan(catalog.MAKER, live=False, apply=True)
    assert result == {"product": "prod_1", "price": "price_1", "created": False}
    cli.assert_not_called()


def test_amount_drift_is_refused_because_prices_are_immutable():
    existing = {"id": "price_1", "product": "prod_1", "unit_amount": 1}
    with (
        patch.object(script, "find_price", return_value=existing),
        pytest.raises(RuntimeError, match="immutable"),
    ):
        script.ensure_plan(catalog.MAKER, live=False, apply=True)


def test_dry_run_creates_nothing():
    with (
        patch.object(script, "find_price", return_value=None),
        patch.object(script, "find_product", return_value=None),
        patch.object(script, "stripe_cli") as cli,
    ):
        result = script.ensure_plan(catalog.MAKER, live=True, apply=False)
    assert result["price"] == "(would create)"
    cli.assert_not_called()
