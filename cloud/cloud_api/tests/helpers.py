class FakeStripeEvent(dict):
    """Minimal stand-in for a verified stripe.Event: dict-style access
    (event["id"], event["data"]["object"]) plus the to_dict() the webhook
    service calls for JSONB persistence."""

    def to_dict(self, for_json: bool = True) -> dict:
        return dict(self)


def stripe_event(event_id: str, event_type: str, obj: dict) -> FakeStripeEvent:
    return FakeStripeEvent(id=event_id, type=event_type, data={"object": obj})
