from unittest.mock import AsyncMock

import pytest
from fastapi import HTTPException
from stripe import WebhookSignature

import cloud_api
from cloud_api.interface.stripe_webhooks.stripe_webhooks import stripe_webhook


class _FakeRequest:
    def __init__(self, body: bytes) -> None:
        self._body = body

    async def body(self) -> bytes:
        return self._body


@pytest.fixture
def webhook_secret(monkeypatch):
    secret = "whsec_test_secret"
    monkeypatch.setattr(cloud_api.app_config.stripe, "webhook_secret", secret)
    return secret


@pytest.mark.asyncio
async def test_stripe_webhook_accepts_validly_signed_payload(webhook_secret):
    payload = (
        b'{"id": "evt_1", "type": "checkout.session.completed", "data": {"object": {}}}'
    )
    header = WebhookSignature.generate_signature_header(
        payload=payload.decode(), secret=webhook_secret
    )
    service = AsyncMock()

    result = await stripe_webhook(
        request=_FakeRequest(payload),
        stripe_signature=header,
        stripe_webhook_service=service,
    )

    assert result == {"status": "ok"}
    service.handle_event.assert_awaited_once()
    (event,) = service.handle_event.await_args.args
    assert event["id"] == "evt_1"


@pytest.mark.asyncio
async def test_stripe_webhook_rejects_tampered_payload(webhook_secret):
    payload = (
        b'{"id": "evt_1", "type": "checkout.session.completed", "data": {"object": {}}}'
    )
    header = WebhookSignature.generate_signature_header(
        payload=payload.decode(), secret=webhook_secret
    )
    tampered_payload = payload.replace(b"evt_1", b"evt_evil")
    service = AsyncMock()

    with pytest.raises(HTTPException) as exc_info:
        await stripe_webhook(
            request=_FakeRequest(tampered_payload),
            stripe_signature=header,
            stripe_webhook_service=service,
        )

    assert exc_info.value.status_code == 400
    service.handle_event.assert_not_awaited()


@pytest.mark.asyncio
async def test_stripe_webhook_rejects_wrong_secret(webhook_secret):
    payload = (
        b'{"id": "evt_1", "type": "checkout.session.completed", "data": {"object": {}}}'
    )
    header = WebhookSignature.generate_signature_header(
        payload=payload.decode(), secret="whsec_wrong_secret"
    )
    service = AsyncMock()

    with pytest.raises(HTTPException) as exc_info:
        await stripe_webhook(
            request=_FakeRequest(payload),
            stripe_signature=header,
            stripe_webhook_service=service,
        )

    assert exc_info.value.status_code == 400
    service.handle_event.assert_not_awaited()
