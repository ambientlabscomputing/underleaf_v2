import shutil
import subprocess

import pytest
import yaml

from cloud_api.service.worker_bootstrap import (
    BootstrapParams,
    render_cloud_init,
    render_conn_worker_config,
)

PARAMS = BootstrapParams(
    worker_id="worker_abc",
    token="tok-123",
    callback_url="https://api.example.com/api/v2/cloud/internal/workers/worker_abc",
    image="ghcr.io/acme/conn-worker:1",
    ca_pem="-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----",
    gateway_domain="example.com",
    grpc_tls_port=50443,
    api_egress_ip="198.51.100.4",
)


def files(params=PARAMS) -> dict[str, dict]:
    doc = yaml.safe_load(render_cloud_init(params))
    return {f["path"]: f for f in doc["write_files"]}


def test_cloud_init_is_valid_yaml_with_expected_files():
    doc = yaml.safe_load(render_cloud_init(PARAMS))
    assert doc["runcmd"] == [["bash", "/opt/underleaf/bootstrap.sh"]]
    assert set(files()) == {
        "/opt/underleaf/conn_worker.yaml",
        "/opt/underleaf/nginx.conf",
        "/opt/underleaf/docker-compose.yml",
        "/opt/underleaf/certs/ca.pem",
        "/opt/underleaf/bootstrap.sh",
    }
    assert render_cloud_init(PARAMS).startswith("#cloud-config\n")
    assert doc["ssh_pwauth"] is False  # DO mails a root password without a key


def test_embedded_files_are_themselves_valid_and_carry_params():
    f = files()
    assert (
        yaml.safe_load(f["/opt/underleaf/conn_worker.yaml"]["content"])["gateway"][
            "domain"
        ]
        == "example.com"
    )
    compose = yaml.safe_load(f["/opt/underleaf/docker-compose.yml"]["content"])
    assert compose["services"]["conn_worker"]["image"] == "ghcr.io/acme/conn-worker:1"
    # conn_worker's own ports are never published on the public interface
    assert all(
        p.startswith("127.0.0.1:") for p in compose["services"]["conn_worker"]["ports"]
    )
    assert f["/opt/underleaf/bootstrap.sh"]["permissions"] == "0700"
    assert "BEGIN CERTIFICATE" in f["/opt/underleaf/certs/ca.pem"]["content"]


def test_nginx_only_admits_the_api_on_the_grpc_port():
    conf = files()["/opt/underleaf/nginx.conf"]["content"]
    assert "ssl_verify_client       on" in conf
    assert '"CN=cloud_api" 127.0.0.1:50102' in conf
    assert "default              127.0.0.1:1" in conf
    assert "listen 50443 ssl" in conf and "listen 19021 ssl" in conf


def test_script_has_token_callback_and_firewall_but_no_private_key():
    script = files()["/opt/underleaf/bootstrap.sh"]["content"]
    assert "tok-123" in script and PARAMS.callback_url in script
    assert "ufw allow from 198.51.100.4 to any port 50443 proto tcp" in script
    assert "ufw allow 19021/tcp" in script
    # SSH is debugging-only: not open to the world when we know the operator
    assert "ufw allow from 198.51.100.4 to any port 22 proto tcp" in script
    assert "ufw allow 22/tcp" not in script
    # the key is generated on the VM; user-data must never contain one
    assert "PRIVATE KEY" not in render_cloud_init(PARAMS)


def test_script_opens_grpc_to_all_without_an_egress_ip():
    from dataclasses import replace

    script = files(replace(PARAMS, api_egress_ip=""))["/opt/underleaf/bootstrap.sh"][
        "content"
    ]
    assert "ufw allow 50443/tcp" in script
    assert "ufw allow 22/tcp" in script


@pytest.mark.skipif(shutil.which("bash") is None, reason="bash not available")
def test_script_passes_bash_syntax_check():
    script = files()["/opt/underleaf/bootstrap.sh"]["content"]
    result = subprocess.run(
        ["bash", "-n"], input=script, text=True, capture_output=True, check=False
    )
    assert result.returncode == 0, result.stderr


def test_conn_worker_config_matches_what_go_expects():
    cfg = yaml.safe_load(render_conn_worker_config("x.io"))
    assert cfg["gateway"]["node_connections_port"] == 9021
    assert cfg["redis"]["host"] == "redis"
