import stripe
from fastapi import APIRouter, Depends, Header, HTTPException, Request, status

from cloud_api import logger
from cloud_api.service.stripe_webhook_service import StripeWebhookService
from cloud_api.service_manager import get_stripe_webhook_service

# Deliberately NOT gated by Depends(get_access_claims): Stripe can't present
# an Underleaf bearer token. Authenticity comes from verifying Stripe's own
# request signature instead (stripe.Webhook.construct_event), which is the
# Stripe-native equivalent of an auth check -- see interface/deps.py's
# x_subject_id_token_middleware_, which only conditionally mints a token per
# request rather than blanket-gating every route, so this endpoint simply
# never receives one.
router = APIRouter(prefix="/stripe", tags=["Stripe"])


@router.post(
    "/webhooks",
    status_code=status.HTTP_200_OK,
    summary="Stripe webhook receiver (signature-verified, not bearer-token-authenticated)",
)
async def stripe_webhook(
    request: Request,
    stripe_signature: str = Header(..., alias="Stripe-Signature"),
    stripe_webhook_service: StripeWebhookService = Depends(get_stripe_webhook_service),
) -> dict:
    from cloud_api import app_config

    payload = await request.body()
    try:
        event = stripe.Webhook.construct_event(
            payload=payload,
            sig_header=stripe_signature,
            secret=app_config.stripe.webhook_secret,
        )
    except (ValueError, stripe.SignatureVerificationError) as exc:
        logger.bind(error=str(exc)).warning("Rejected Stripe webhook: bad signature")
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST, detail="Invalid signature"
        )

    await stripe_webhook_service.handle_event(event)
    return {"status": "ok"}
