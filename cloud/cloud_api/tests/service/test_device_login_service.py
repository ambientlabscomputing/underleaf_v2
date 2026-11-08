from datetime import UTC, datetime, timedelta

import pytest

from cloud_api.models.api.ufctl import UfctlDeviceRequest
from cloud_api.service.device_key_service import DeviceKeyService
from cloud_api.service.device_login_service import (
    DEVICE_CODE_TTL_SECONDS,
    DeviceLoginError,
    DeviceLoginExpiredError,
    DeviceLoginNotFoundError,
    DeviceLoginNotPendingError,
    DeviceLoginService,
    TooManyAttemptsError,
    generate_user_code,
    normalize_user_code,
)
from tests.ufctl_fakes import FakeDeviceKeyRepo, FakeDeviceLoginRepo


@pytest.fixture
def keys():
    return FakeDeviceKeyRepo()


@pytest.fixture
def repo():
    return FakeDeviceLoginRepo()


@pytest.fixture
def svc(repo, keys):
    return DeviceLoginService(repo, DeviceKeyService(keys))


async def _start(svc, name="my-laptop"):
    return await svc.start(UfctlDeviceRequest(device_name=name))


async def _err(coro) -> str:
    with pytest.raises(DeviceLoginError) as e:
        await coro
    return e.value.code


@pytest.mark.asyncio
async def test_start_returns_the_documented_shape(svc, repo):
    r = await _start(svc)
    assert set(r.model_dump()) == {
        "device_code",
        "user_code",
        "verification_uri",
        "verification_uri_complete",
        "expires_in",
        "interval",
    }
    assert r.verification_uri.endswith("/ufctl/activate")
    assert (
        r.verification_uri_complete == f"{r.verification_uri}?user_code={r.user_code}"
    )
    assert r.expires_in == DEVICE_CODE_TTL_SECONDS and r.interval == 5
    assert len(r.device_code) >= 64
    (row,) = repo.rows.values()
    # the device code is stored only as a hash
    assert r.device_code not in repr(vars(row))
    assert row.device_name == "my-laptop" and row.status == "pending"


@pytest.mark.asyncio
async def test_full_flow_pending_then_key_once(svc, keys):
    r = await _start(svc)
    t = datetime.now(UTC)
    assert await _err(svc.poll(r.device_code, now=t)) == "authorization_pending"
    await svc.approve(r.user_code, "acct_a", now=t)
    got = await svc.poll(r.device_code, now=t + timedelta(seconds=6))
    assert got.device_key.startswith("ufdk_") and got.device_key_id.startswith("dk_")
    (row,) = keys.rows.values()
    assert (row.principal_account_id, row.name) == ("acct_a", "my-laptop")
    # single use: the same device code never yields a second key
    assert (
        await _err(svc.poll(r.device_code, now=t + timedelta(seconds=12)))
        == "expired_token"
    )
    assert len(keys.rows) == 1


@pytest.mark.asyncio
async def test_concurrent_polls_mint_exactly_one_key(svc, keys):
    import asyncio

    r = await _start(svc)
    t = datetime.now(UTC)
    await svc.approve(r.user_code, "acct_a", now=t)
    row = next(iter(svc.repo.rows.values()))
    # defeat the slow_down guard to model many racing requests
    results = await asyncio.gather(
        *[svc.poll(r.device_code, now=t + timedelta(seconds=9)) for _ in range(5)],
        return_exceptions=True,
    )
    assert sum(not isinstance(x, Exception) for x in results) == 1
    assert len(keys.rows) == 1
    assert row.status == "consumed"


@pytest.mark.asyncio
async def test_deny_is_reported_and_never_yields_a_key(svc, keys):
    r = await _start(svc)
    t = datetime.now(UTC)
    await svc.deny(r.user_code, "acct_a", now=t)
    assert await _err(svc.poll(r.device_code, now=t)) == "access_denied"
    assert (
        await _err(svc.poll(r.device_code, now=t + timedelta(seconds=6)))
        == "access_denied"
    )
    assert keys.rows == {}


@pytest.mark.asyncio
async def test_unknown_device_code_and_expiry_are_expired_token(svc, keys):
    assert await _err(svc.poll("f" * 64)) == "expired_token"
    r = await _start(svc)
    t = datetime.now(UTC)
    await svc.approve(r.user_code, "acct_a", now=t)
    late = t + timedelta(seconds=DEVICE_CODE_TTL_SECONDS + 1)
    assert await _err(svc.poll(r.device_code, now=late)) == "expired_token"
    assert keys.rows == {}  # approved but never collected in time: no key


@pytest.mark.asyncio
async def test_polling_too_fast_is_slow_down_and_raises_the_interval(svc, repo):
    r = await _start(svc)
    t = datetime.now(UTC)
    assert await _err(svc.poll(r.device_code, now=t)) == "authorization_pending"
    assert (
        await _err(svc.poll(r.device_code, now=t + timedelta(seconds=1))) == "slow_down"
    )
    row = next(iter(repo.rows.values()))
    assert row.poll_interval == 10
    # 6 s after the first poll is no longer too fast for 5 s but is for 10 s;
    # every slow_down adds 5 s, as the client does on its side
    assert (
        await _err(svc.poll(r.device_code, now=t + timedelta(seconds=6))) == "slow_down"
    )
    assert row.poll_interval == 15
    assert row.last_polled_at == t  # slow_down answers do not count as polls
    assert (
        await _err(svc.poll(r.device_code, now=t + timedelta(seconds=16)))
        == "authorization_pending"
    )
    assert row.last_polled_at == t + timedelta(seconds=16)


@pytest.mark.asyncio
async def test_poll_exactly_on_the_interval_is_allowed(svc):
    r = await _start(svc)
    t = datetime.now(UTC)
    await _err(svc.poll(r.device_code, now=t))
    # a client sleeping its interval can arrive fractions of a second early
    assert (
        await _err(svc.poll(r.device_code, now=t + timedelta(seconds=4.7)))
        == "authorization_pending"
    )


@pytest.mark.asyncio
async def test_approval_binds_the_callers_account_and_is_one_shot(svc):
    r = await _start(svc)
    t = datetime.now(UTC)
    out = await svc.approve(r.user_code.lower().replace("-", " "), "acct_a", now=t)
    assert out.status == "approved" and out.device_name == "my-laptop"
    row = next(iter(svc.repo.rows.values()))
    assert row.principal_account_id == "acct_a"
    # a second account (or the same one) cannot decide it again
    with pytest.raises(DeviceLoginNotPendingError):
        await svc.approve(r.user_code, "acct_b", now=t)
    with pytest.raises(DeviceLoginNotPendingError):
        await svc.deny(r.user_code, "acct_b", now=t)
    assert row.principal_account_id == "acct_a"


@pytest.mark.asyncio
async def test_expired_request_cannot_be_approved_or_shown(svc):
    r = await _start(svc)
    late = datetime.now(UTC) + timedelta(seconds=DEVICE_CODE_TTL_SECONDS + 1)
    with pytest.raises(DeviceLoginExpiredError):
        await svc.approve(r.user_code, "acct_a", now=late)
    with pytest.raises(DeviceLoginExpiredError):
        await svc.get_request(r.user_code, "acct_a", now=late)


@pytest.mark.asyncio
async def test_get_request_shows_the_device_name_and_code(svc):
    r = await _start(svc, "build-box")
    info = await svc.get_request(r.user_code, "acct_a")
    assert (info.user_code, info.device_name, info.status) == (
        r.user_code,
        "build-box",
        "pending",
    )


@pytest.mark.asyncio
async def test_failed_user_code_guesses_are_rate_limited_per_account(svc):
    for _ in range(10):
        with pytest.raises(DeviceLoginNotFoundError):
            await svc.approve("ZZZZ-ZZZZ", "acct_a")
    with pytest.raises(TooManyAttemptsError):
        await svc.approve("ZZZZ-ZZZZ", "acct_a")
    # another account is not affected, and a correct code works for it
    r = await _start(svc)
    await svc.approve(r.user_code, "acct_b")


@pytest.mark.asyncio
async def test_device_creation_is_capped(svc):
    with pytest.raises(TooManyAttemptsError):
        for _ in range(500):
            await _start(svc)


@pytest.mark.asyncio
async def test_device_name_is_sanitised(svc, repo):
    await _start(svc, "evil\x1b[2J\nhost" + "x" * 200)
    row = next(iter(repo.rows.values()))
    assert "\x1b" not in row.device_name and "\n" not in row.device_name
    assert len(row.device_name) <= 64
    await svc.start(UfctlDeviceRequest(device_name=""))  # os.Hostname() failed
    assert "unknown device" in {r.device_name for r in repo.rows.values()}


def test_user_codes_are_unambiguous_and_normalised():
    for _ in range(200):
        c = generate_user_code()
        assert len(c) == 9 and c[4] == "-"
        assert not set(c) & set("01OIL")
    assert normalize_user_code(" abcd efgh ") == "ABCD-EFGH"
    assert normalize_user_code("abcd-efgh") == "ABCD-EFGH"
    assert normalize_user_code("short") == "short"
