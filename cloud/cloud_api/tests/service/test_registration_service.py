from datetime import UTC, datetime, timedelta
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest
from cryptography import x509
from cryptography.x509.oid import NameOID

from cloud_api.service.registration_service import (
    InvalidOneTimeTokenError,
    RegistrationService,
)
from tests.conftest import generate_csr_pem


def _make_candidate(**overrides):
    defaults = {
        "id": "candidate_1",
        "cluster_id": "cluster_abc",
        "proposed_cluster_id": "cluster_abc",
        "one_time_token_expires_at": datetime.now(UTC) + timedelta(minutes=5),
    }
    defaults.update(overrides)
    return SimpleNamespace(**defaults)


def _cn_of(cert_pem: str) -> str:
    cert = x509.load_pem_x509_certificate(cert_pem.encode())
    return cert.subject.get_attributes_for_oid(NameOID.COMMON_NAME)[0].value


@pytest.mark.asyncio
async def test_issue_certificate_success(cert_lib):
    repo = AsyncMock()
    repo.find_candidate_by_token_hash.return_value = _make_candidate()
    service = RegistrationService(
        registration_repo=repo, cluster_repo=None, cert_lib=cert_lib
    )

    resp = await service.issue_certificate(
        one_time_token="one-time-token",
        csr_pem=generate_csr_pem(),
        node_id="node_xyz",
    )

    assert _cn_of(resp.certificate_pem) == "cluster_abc"
    assert _cn_of(resp.node_certificate_pem) == "node_xyz"
    assert resp.ca_chain_pem == cert_lib._ca_cert_pem

    repo.consume.assert_awaited_once_with("candidate_1")


@pytest.mark.asyncio
async def test_issue_certificate_unknown_token_raises(cert_lib):
    repo = AsyncMock()
    repo.find_candidate_by_token_hash.return_value = None
    service = RegistrationService(
        registration_repo=repo, cluster_repo=None, cert_lib=cert_lib
    )

    with pytest.raises(InvalidOneTimeTokenError):
        await service.issue_certificate(
            one_time_token="unknown-token",
            csr_pem=generate_csr_pem(),
            node_id="node_xyz",
        )

    repo.consume.assert_not_awaited()


@pytest.mark.asyncio
async def test_issue_certificate_expired_token_raises(cert_lib):
    repo = AsyncMock()
    repo.find_candidate_by_token_hash.return_value = _make_candidate(
        one_time_token_expires_at=datetime.now(UTC) - timedelta(minutes=5)
    )
    service = RegistrationService(
        registration_repo=repo, cluster_repo=None, cert_lib=cert_lib
    )

    with pytest.raises(InvalidOneTimeTokenError):
        await service.issue_certificate(
            one_time_token="expired-token",
            csr_pem=generate_csr_pem(),
            node_id="node_xyz",
        )

    repo.consume.assert_not_awaited()


@pytest.mark.asyncio
async def test_issue_certificate_falls_back_to_proposed_cluster_id(cert_lib):
    """subject_id = candidate.cluster_id or candidate.proposed_cluster_id —
    exercise the fallback branch for a candidate that hasn't been linked to a
    real Cluster row yet.
    """
    repo = AsyncMock()
    repo.find_candidate_by_token_hash.return_value = _make_candidate(
        cluster_id=None, proposed_cluster_id="cluster_proposed"
    )
    service = RegistrationService(
        registration_repo=repo, cluster_repo=None, cert_lib=cert_lib
    )

    resp = await service.issue_certificate(
        one_time_token="one-time-token",
        csr_pem=generate_csr_pem(),
        node_id="node_xyz",
    )

    assert _cn_of(resp.certificate_pem) == "cluster_proposed"


@pytest.mark.asyncio
async def test_renew_certificate_signs_for_mtls_derived_subject(cert_lib):
    """renew_certificate takes subject_id directly (from claims.sub, i.e. an
    already mTLS-verified caller) rather than looking anything up — no
    registration_repo/cluster_repo interaction at all.
    """
    repo = AsyncMock()
    service = RegistrationService(
        registration_repo=repo, cluster_repo=None, cert_lib=cert_lib
    )

    resp = await service.renew_certificate(
        subject_id="cluster_abc",
        csr_pem=generate_csr_pem(),
        node_id="node_xyz",
    )

    assert _cn_of(resp.certificate_pem) == "cluster_abc"
    assert _cn_of(resp.node_certificate_pem) == "node_xyz"
    assert resp.ca_chain_pem == cert_lib._ca_cert_pem

    repo.find_candidate_by_token_hash.assert_not_awaited()
    repo.consume.assert_not_awaited()
