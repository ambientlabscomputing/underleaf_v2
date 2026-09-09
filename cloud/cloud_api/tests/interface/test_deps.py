"""Regression test for a real bug found while manually verifying track 4's
mTLS renewal flow against a live stack: x_subject_id_token_middleware_ minted
a Bearer token and appended it to the raw ASGI header list, but as
(b"Authorization", ...) -- capitalized. ASGI requires header names in that
list to be lowercase, and Starlette's Headers lookups don't renormalize case
when scanning it, so the injected header was silently invisible to every
downstream `request.headers` read -- including the APIKeyHeader dependency
this middleware exists to satisfy -- even though the middleware logged
success. This meant every mTLS-authenticated endpoint (anything relying on
X-Subject-Id -> get_access_claims) was unreachable in practice.
"""

from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import pytest
from starlette.requests import Request

from cloud_api.interface.deps import x_subject_id_token_middleware_


def _make_request(headers: dict[str, str]) -> Request:
    raw_headers = [(k.lower().encode(), v.encode()) for k, v in headers.items()]
    scope = {"type": "http", "headers": raw_headers, "method": "POST", "path": "/x"}
    return Request(scope)


@pytest.mark.asyncio
async def test_injected_authorization_header_is_visible_downstream():
    request = _make_request({"X-Subject-Id": "cluster_abc"})
    fake_cluster = SimpleNamespace(principal_account_id="principal_1", id="cluster_abc")

    with (
        patch(
            "cloud_api.interface.deps.fetch_data_for_node_or_cluster",
            new=AsyncMock(return_value=(None, fake_cluster)),
        ),
        patch("cloud_api.interface.deps.get_auth_lib", return_value=object()),
        patch("cloud_api.interface.deps.get_user_repo", return_value=object()),
        patch("cloud_api.interface.deps.mint_token", return_value="minted-token"),
    ):
        seen = {}

        async def call_next(req: Request):
            # This is exactly how FastAPI's APIKeyHeader dependency reads the
            # header downstream -- a case-insensitive `request.headers.get`.
            seen["authorization"] = req.headers.get("authorization")

        await x_subject_id_token_middleware_(request, call_next)

    assert seen["authorization"] == "Bearer minted-token"


@pytest.mark.asyncio
async def test_no_op_when_x_subject_id_header_absent():
    request = _make_request({})
    seen = {}

    async def call_next(req: Request):
        seen["authorization"] = req.headers.get("authorization")

    await x_subject_id_token_middleware_(request, call_next)

    assert seen["authorization"] is None


@pytest.mark.asyncio
async def test_does_not_override_an_existing_authorization_header():
    request = _make_request(
        {"X-Subject-Id": "cluster_abc", "Authorization": "Bearer real-token"}
    )
    seen = {}

    async def call_next(req: Request):
        seen["authorization"] = req.headers.get("authorization")

    await x_subject_id_token_middleware_(request, call_next)

    assert seen["authorization"] == "Bearer real-token"
