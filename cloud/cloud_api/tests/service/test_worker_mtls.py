"""The API's gRPC client credentials against a real TLS server presenting a
cert minted by CertLib.issue_server_cert (IP SAN), requiring a client cert:
proves the handshake the production path depends on, without a VM."""

import socket

import grpc
import grpc.aio
import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

from cloud_api.config import WorkerDefaults
from cloud_api.lib.events import EventBus
from cloud_api.models.api.common import Status
from cloud_api.models.api.worker import Worker, WorkerState, WorkerType
from cloud_api.service.worker_service import WorkerService, build_worker_config

pytestmark = pytest.mark.asyncio


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _server_creds(cert_lib, ip: str):
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    csr = (
        x509.CertificateSigningRequestBuilder()
        .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "w")]))
        .sign(key, hashes.SHA256())
    )
    cert, ca = cert_lib.issue_server_cert(
        csr.public_bytes(serialization.Encoding.PEM).decode(), "w", ip_addresses=[ip]
    )
    key_pem = key.private_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.TraditionalOpenSSL,
        serialization.NoEncryption(),
    )
    return grpc.ssl_server_credentials(
        [(key_pem, cert.encode())],
        root_certificates=ca.encode(),
        require_client_auth=True,
    )


def _service(cert_lib, port: int) -> WorkerService:
    settings = WorkerDefaults(grpc_tls_port=port, image="i", api_public_url="http://x")
    return WorkerService(
        worker_repository=None,
        event_repository=None,
        vm_manager=None,
        cert_lib=cert_lib,
        settings=settings,
        event_bus=EventBus(),
    )


def _worker(ip: str, settings: WorkerDefaults) -> Worker:
    return Worker(
        id="worker_t",
        worker_type=WorkerType.CONNECTION_WORKER,
        config=build_worker_config(settings),
        state=WorkerState.AVAILABLE,
        status=Status.SUCCEEDED,
        extra_data={"vm": {"public_ip": ip}},
    )


async def test_api_client_completes_mtls_handshake_with_issued_server_cert(cert_lib):
    port = _free_port()
    server = grpc.aio.server()
    server.add_secure_port(f"127.0.0.1:{port}", _server_creds(cert_lib, "127.0.0.1"))
    await server.start()
    try:
        svc = _service(cert_lib, port)
        client = await svc.get_client(_worker("127.0.0.1", svc.settings))
        assert await client.ping(timeout=5)
        assert (
            await svc.get_client(_worker("127.0.0.1", svc.settings)) is client
        )  # pooled
        await svc.close()
    finally:
        await server.stop(None)


async def test_server_cert_for_a_different_ip_is_rejected(cert_lib):
    port = _free_port()
    server = grpc.aio.server()
    # cert is for 203.0.113.9 but the API dials 127.0.0.1: must not connect
    server.add_secure_port(f"127.0.0.1:{port}", _server_creds(cert_lib, "203.0.113.9"))
    await server.start()
    try:
        svc = _service(cert_lib, port)
        client = await svc.get_client(_worker("127.0.0.1", svc.settings))
        assert not await client.ping(timeout=2)
        await svc.close()
    finally:
        await server.stop(None)


async def test_client_without_a_cert_is_rejected_by_the_server(cert_lib):
    port = _free_port()
    server = grpc.aio.server()
    server.add_secure_port(f"127.0.0.1:{port}", _server_creds(cert_lib, "127.0.0.1"))
    await server.start()
    try:
        from cloud_api.lib.conn_worker_client import ConnWorkerClient

        anon = ConnWorkerClient(
            f"127.0.0.1:{port}",
            credentials=grpc.ssl_channel_credentials(
                root_certificates=cert_lib.ca_pem.encode()
            ),
        )
        assert not await anon.ping(timeout=2)
        await anon.close()
    finally:
        await server.stop(None)
