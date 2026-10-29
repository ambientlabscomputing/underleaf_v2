#!/usr/bin/env bash
# Resolve Stripe *sandbox* credentials for the local stack and exec the given
# command with them exported:
#
#   scripts/stripe_env.sh docker compose --profile stripe up -d
#
# Key source, in order: $UNDERLEAF_STRIPE_API_KEY, then the test-mode key the
# host `stripe login` already stored. Anything that isn't a test-mode key is
# refused -- this tooling must never be able to touch live mode.
#
# The webhook signing secret is derived with `stripe listen --print-secret`,
# which is stable for a given key, so cloud_api (which verifies signatures)
# and the stripe_cli container (which signs them) always agree.
set -euo pipefail

key="${UNDERLEAF_STRIPE_API_KEY:-}"
if [ -z "$key" ] && command -v stripe >/dev/null 2>&1; then
  key="$(stripe config --list 2>/dev/null | sed -n "s/^test_mode_api_key = '\(.*\)'/\1/p")"
fi

case "$key" in
  sk_test_* | rk_test_*) ;;
  "")
    echo "No Stripe sandbox key found. Set UNDERLEAF_STRIPE_API_KEY=sk_test_... or run \`stripe login\`." >&2
    exit 1
    ;;
  *)
    echo "Refusing: UNDERLEAF_STRIPE_API_KEY is not a test-mode key (sk_test_/rk_test_)." >&2
    exit 1
    ;;
esac

secret="$(docker run --rm -e STRIPE_API_KEY="$key" stripe/stripe-cli:v1.55.1 listen --print-secret | tail -1)"
case "$secret" in
  whsec_*) ;;
  *)
    echo "Could not derive a webhook signing secret from Stripe." >&2
    exit 1
    ;;
esac

export UNDERLEAF_STRIPE_API_KEY="$key"
export UNDERLEAF_STRIPE_WEBHOOK_SECRET="$secret"
exec "$@"
