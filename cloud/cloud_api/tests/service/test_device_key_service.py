from datetime import UTC, datetime, timedelta

import pytest

from cloud_api.service.device_key_service import (
    DEVICE_KEY_PREFIX,
    LAST_USED_THROTTLE,
    DeviceKeyService,
    clean_device_name,
    hash_device_key,
    looks_like_device_key,
)
from tests.ufctl_fakes import FakeDeviceKeyRepo


@pytest.fixture
def repo():
    return FakeDeviceKeyRepo()


@pytest.fixture
def svc(repo):
    return DeviceKeyService(repo)


@pytest.mark.asyncio
async def test_secret_is_prefixed_high_entropy_and_only_its_hash_is_stored(svc, repo):
    key, secret = await svc.create_for_account("acct_a", "laptop")
    assert secret.startswith(DEVICE_KEY_PREFIX)
    assert len(secret) == len(DEVICE_KEY_PREFIX) + 43  # 256 bits, base64url
    assert key.id.startswith("dk_")
    (row,) = repo.rows.values()
    assert row.key_hash == hash_device_key(secret)
    # the secret appears nowhere in what is stored or returned to the owner
    assert secret not in repr(vars(row))
    assert secret not in key.model_dump_json()
    assert "key_hash" not in key.model_dump()
    _, other = await svc.create_for_account("acct_a", "laptop")
    assert other != secret


@pytest.mark.asyncio
async def test_authenticate_returns_the_owning_account(svc):
    key, secret = await svc.create_for_account("acct_a", "laptop")
    got = await svc.authenticate(secret)
    assert got is not None
    assert (got.id, got.principal_account_id) == (key.id, "acct_a")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "bad",
    [
        "",
        "ufdk_",
        "ufdk_short",
        "ufdk_" + "A" * 43,  # well formed but unknown
        "ufdk_" + "A" * 44,
        "UFDK_" + "A" * 43,
        "Bearer ufdk_" + "A" * 43,
        "ufdk_" + "A" * 42 + "'",
        "ufdk_" + "A" * 43 + "\n",
    ],
)
async def test_unknown_or_malformed_keys_do_not_authenticate(svc, bad):
    await svc.create_for_account("acct_a", "laptop")
    assert await svc.authenticate(bad) is None


@pytest.mark.asyncio
async def test_revoked_key_never_authenticates_again(svc):
    key, secret = await svc.create_for_account("acct_a", "laptop")
    assert await svc.authenticate(secret) is not None
    assert await svc.revoke("acct_a", key.id) is True
    assert await svc.authenticate(secret) is None
    assert await svc.list_for_account("acct_a") == []
    # idempotent for the owner
    assert await svc.revoke("acct_a", key.id) is True


@pytest.mark.asyncio
async def test_foreign_and_missing_ids_cannot_be_revoked_and_look_alike(svc):
    key, secret = await svc.create_for_account("acct_a", "laptop")
    assert await svc.revoke("acct_b", key.id) is False
    assert await svc.revoke("acct_b", "dk_nope") is False
    assert await svc.revoke("acct_a", "dk_nope") is False
    assert await svc.authenticate(secret) is not None  # untouched


@pytest.mark.asyncio
async def test_list_is_scoped_to_the_account_and_never_has_secrets(svc):
    a, _ = await svc.create_for_account("acct_a", "one")
    await svc.create_for_account("acct_b", "two")
    items = await svc.list_for_account("acct_a")
    assert [i.id for i in items] == [a.id]
    assert "key_hash" not in items[0].model_dump()


@pytest.mark.asyncio
async def test_last_used_is_throttled(svc, repo):
    _, secret = await svc.create_for_account("acct_a", "laptop")
    t0 = datetime.now(UTC)
    await svc.authenticate(secret, now=t0)
    await svc.authenticate(secret, now=t0 + timedelta(seconds=1))
    await svc.authenticate(secret, now=t0 + LAST_USED_THROTTLE - timedelta(seconds=1))
    assert repo.touches == 1
    await svc.authenticate(secret, now=t0 + LAST_USED_THROTTLE + timedelta(seconds=1))
    assert repo.touches == 2


@pytest.mark.asyncio
async def test_bookkeeping_failure_does_not_fail_authentication(svc, repo):
    _, secret = await svc.create_for_account("acct_a", "laptop")

    async def boom(*a, **k):
        raise RuntimeError("db down")

    repo.touch_last_used = boom
    assert await svc.authenticate(secret) is not None


def test_device_key_recognition_and_name_cleaning():
    assert looks_like_device_key("ufdk_abc")
    assert not looks_like_device_key("eyJhbGciOi.x.y")
    assert not looks_like_device_key(" ufdk_abc")
    assert clean_device_name("host\x00\x1b[31m\nname") == "host[31mname"
    assert clean_device_name("") == "unknown device"
    assert len(clean_device_name("x" * 500)) == 64
