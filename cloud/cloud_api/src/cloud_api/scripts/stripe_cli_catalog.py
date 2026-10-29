"""Create the paid-plan Stripe Products/Prices with the Stripe CLI.

    python -m cloud_api.scripts.stripe_cli_catalog             # dry run, test mode
    python -m cloud_api.scripts.stripe_cli_catalog --apply     # create in test mode
    python -m cloud_api.scripts.stripe_cli_catalog --apply --live

Driven by cloud_api.lib.catalog so the Stripe prices can't drift from the
catalog. Idempotent: a Price is identified by its lookup_key
(underleaf_<plan>_month); an existing one is reused (and its amount checked),
never duplicated. Prices are immutable in Stripe -- if an amount ever has to
change, create a new Price rather than editing this one.

Prints a JSON map {plan: {product, price}}. Run `make stripe-sync` (with the
matching API key) in each environment afterwards to record the ids in that
environment's database.
"""

import argparse
import json
import subprocess
import sys

from cloud_api.lib import catalog

CURRENCY = "usd"
INTERVAL = "month"


def stripe_cli(args: list[str], live: bool) -> dict:
    cmd = ["stripe", *args] + (["--live"] if live else [])
    result = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(
            f"`{' '.join(cmd)}` failed:\n{result.stderr or result.stdout}"
        )
    body = json.loads(result.stdout)
    # The CLI reports API errors as a JSON body with exit status 0.
    if isinstance(body, dict) and "error" in body:
        err = body["error"]
        raise RuntimeError(
            f"Stripe refused `stripe {' '.join(args[:2])}`: {err.get('message', err)}"
        )
    return body


def lookup_key(plan: catalog.PlanSpec) -> str:
    return f"underleaf_{plan.name}_{INTERVAL}"


def describe(plan: catalog.PlanSpec) -> str:
    gb = plan.included_bytes / 10**9
    size = f"{gb / 1000:g} TB" if gb >= 1000 else f"{gb:g} GB"
    return f"{size} of gateway data per month"


def find_price(plan: catalog.PlanSpec, live: bool) -> dict | None:
    found = stripe_cli(
        [
            "prices",
            "list",
            "-d",
            f"lookup_keys[]={lookup_key(plan)}",
            "-d",
            "active=true",
        ],
        live,
    )
    return found["data"][0] if found["data"] else None


def find_product(plan: catalog.PlanSpec, live: bool) -> dict | None:
    found = stripe_cli(
        [
            "products",
            "search",
            "--query",
            f"metadata['underleaf_plan']:'{plan.name}' AND active:'true'",
        ],
        live,
    )
    return found["data"][0] if found["data"] else None


def ensure_plan(plan: catalog.PlanSpec, live: bool, apply: bool) -> dict:
    existing = find_price(plan, live)
    if existing:
        if existing["unit_amount"] != plan.price_cents:
            raise RuntimeError(
                f"{plan.name}: Stripe price {existing['id']} is {existing['unit_amount']}c but the "
                f"catalog says {plan.price_cents}c. Prices are immutable; create a new one "
                f"deliberately rather than letting this script guess."
            )
        return {
            "product": existing["product"],
            "price": existing["id"],
            "created": False,
        }

    product = find_product(plan, live)
    if not apply:
        return {
            "product": product["id"] if product else "(would create)",
            "price": "(would create)",
            "created": False,
        }

    if product is None:
        product = stripe_cli(
            [
                "products",
                "create",
                "--name",
                f"Underleaf {plan.name.capitalize()}",
                "--description",
                describe(plan),
                "-d",
                f"metadata[underleaf_plan]={plan.name}",
                "--idempotency",
                f"underleaf-product-{plan.name}-v1",
            ],
            live,
        )
    price = stripe_cli(
        [
            "prices",
            "create",
            "--product",
            product["id"],
            "--unit-amount",
            str(plan.price_cents),
            "--currency",
            CURRENCY,
            "-d",
            f"recurring[interval]={INTERVAL}",
            "--lookup-key",
            lookup_key(plan),
            "-d",
            f"metadata[underleaf_plan]={plan.name}",
            "--idempotency",
            f"underleaf-price-{lookup_key(plan)}-{plan.price_cents}",
        ],
        live,
    )
    return {"product": product["id"], "price": price["id"], "created": True}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument(
        "--apply", action="store_true", help="create objects (default: dry run)"
    )
    parser.add_argument(
        "--live", action="store_true", help="target live mode (default: test mode)"
    )
    args = parser.parse_args()

    mode = "LIVE" if args.live else "test"
    print(f"# mode={mode} apply={args.apply}", file=sys.stderr)
    out = {}
    for plan in catalog.PLANS:
        if not plan.paid:
            continue
        out[plan.name] = ensure_plan(plan, args.live, args.apply)
        print(f"# {plan.name:<8} {out[plan.name]}", file=sys.stderr)
    print(json.dumps(out, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
