#!/usr/bin/env bash
# End-to-end check of the Stripe sandbox webhook path. Needs `make stripe-up`.
#
# Fires a real sandbox event with the Stripe CLI and asserts it travelled
# Stripe -> stripe_cli container -> cloud_api (signature verified) -> Postgres,
# i.e. a stripe_webhook_events row for that event appears, processed with no
# error.
set -euo pipefail
cd "$(dirname "$0")/.."

EVENT="${EVENT:-invoice.paid}"
compose="docker compose --profile stripe"

psql_q() {
  $compose exec -T postgres psql -U postgres -At -c "$1"
}

if ! $compose ps --status running --services | grep -qx stripe_cli; then
  echo "stripe_cli is not running; start it with \`make stripe-up\`." >&2
  exit 1
fi

# Only look at rows created after this instant, so older runs (including
# earlier failures) can neither satisfy nor fail this one.
since="$(psql_q "select clock_timestamp()")"
./scripts/stripe_env.sh sh -c "stripe trigger $EVENT --api-key \"\$UNDERLEAF_STRIPE_API_KEY\"" >/dev/null

new_rows="from underleaf.stripe_webhook_events where event_type='$EVENT' and created_at > '$since'"
for _ in $(seq 1 20); do
  total="$(psql_q "select count(*) $new_rows")"
  done_="$(psql_q "select count(*) $new_rows and processed_at is not null")"
  if [ "$total" -gt 0 ] && [ "$done_" -eq "$total" ]; then
    errors="$(psql_q "select count(*) $new_rows and processing_error is not null")"
    if [ "$errors" -gt 0 ]; then
      echo "FAIL: $EVENT was delivered but cloud_api recorded a processing error:" >&2
      psql_q "select stripe_event_id, processing_error $new_rows and processing_error is not null" >&2
      exit 1
    fi
    echo "OK: $EVENT delivered, signature verified, recorded and processed."
    exit 0
  fi
  sleep 1
done

echo "FAIL: no processed $EVENT row appeared within 20s. Check \`make stripe-logs\`." >&2
exit 1
