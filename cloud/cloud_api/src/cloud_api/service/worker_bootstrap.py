"""Renders the cloud-init payload a worker VM runs on first boot.

The payload is plain `#cloud-config` + a shell script, so any provider that
accepts user-data on a stock Ubuntu image can use it unchanged.

What the VM does:
  1. install docker, ufw, jq
  2. generate its own key + CSR (the private key never leaves the VM)
  3. POST the CSR to /internal/workers/{id}/register, get back a server cert
  4. start redis + conn_worker + an nginx TLS front (compose)
  5. wait until conn_worker answers locally, then POST /ready

Only the one-time bootstrap token and the CA *public* cert are in user-data.
"""

from dataclasses import dataclass
from textwrap import dedent, indent

# Local (VM-only) ports conn_worker listens on; nginx fronts the two that
# leave the box. Matches shared/utils/config.go.
GATEWAY_PORT = 9020
NODE_CONNECTIONS_PORT = 9021
GRPC_PORT = 50102
TUNNEL_PUBLIC_PORT = 19021  # agents dial this (mTLS)

# Only the API's own identity may reach gRPC; any other CA-signed cert
# (e.g. a node's) is routed to a closed port.
API_CLIENT_CN = "cloud_api"


@dataclass(frozen=True)
class BootstrapParams:
    worker_id: str
    token: str
    callback_url: str  # e.g. https://host/api/v2/cloud/internal/workers/<id>
    image: str
    ca_pem: str
    gateway_domain: str
    grpc_tls_port: int
    api_egress_ip: str = ""


def render_conn_worker_config(gateway_domain: str) -> str:
    return dedent(
        f"""\
        gateway:
            gateway_port: {GATEWAY_PORT}
            node_connections_port: {NODE_CONNECTIONS_PORT}
            read_timeout: "5s"
            write_timeout: "10s"
            idle_timeout: "15s"
            domain: "{gateway_domain}"
        redis:
            host: "redis"
            port: 6379
            password: ""
            db: 0
            ttl_seconds: 86400
        """
    )


def render_nginx_conf(grpc_tls_port: int) -> str:
    return dedent(
        f"""\
        worker_processes auto;
        events {{ worker_connections 1024; }}

        stream {{
            # Agents (nodes): any cert signed by our CA.
            server {{
                listen {TUNNEL_PUBLIC_PORT} ssl;
                ssl_certificate         /certs/server.crt;
                ssl_certificate_key     /certs/server.key;
                ssl_client_certificate  /certs/ca.pem;
                ssl_verify_client       on;
                proxy_pass              127.0.0.1:{NODE_CONNECTIONS_PORT};
                proxy_timeout           3600s;
                proxy_connect_timeout   10s;
            }}

            # API -> conn_worker gRPC: mTLS, and only CN={API_CLIENT_CN}.
            map $ssl_client_s_dn $grpc_upstream {{
                "CN={API_CLIENT_CN}" 127.0.0.1:{GRPC_PORT};
                default              127.0.0.1:1;   # closed port: drop everyone else
            }}
            server {{
                listen {grpc_tls_port} ssl;
                ssl_alpn                h2;
                ssl_certificate         /certs/server.crt;
                ssl_certificate_key     /certs/server.key;
                ssl_client_certificate  /certs/ca.pem;
                ssl_verify_client       on;
                proxy_pass              $grpc_upstream;
                proxy_timeout           3600s;
                proxy_connect_timeout   10s;
            }}
        }}
        """
    )


def render_compose(image: str) -> str:
    return dedent(
        f"""\
        services:
          redis:
            image: redis:7-alpine
            restart: unless-stopped
          conn_worker:
            image: {image}
            restart: unless-stopped
            depends_on: [redis]
            environment:
              UNDERLEAF_CONFIG: /etc/underleaf/conn_worker.yaml
            volumes:
              - ./conn_worker.yaml:/etc/underleaf/conn_worker.yaml:ro
            ports:
              - "127.0.0.1:{GATEWAY_PORT}:{GATEWAY_PORT}"
              - "127.0.0.1:{NODE_CONNECTIONS_PORT}:{NODE_CONNECTIONS_PORT}"
              - "127.0.0.1:{GRPC_PORT}:{GRPC_PORT}"
          nginx:
            image: nginx:stable-alpine
            restart: unless-stopped
            network_mode: host
            volumes:
              - ./nginx.conf:/etc/nginx/nginx.conf:ro
              - ./certs:/certs:ro
        """
    )


def render_bootstrap_script(p: BootstrapParams) -> str:
    grpc_rule = (
        f"ufw allow from {p.api_egress_ip} to any port {p.grpc_tls_port} proto tcp"
        if p.api_egress_ip
        else f"ufw allow {p.grpc_tls_port}/tcp"
    )
    # SSH is only for debugging (and only works if a key was installed); keep
    # it off the open internet whenever we know where the operator is.
    ssh_rule = (
        f"ufw allow from {p.api_egress_ip} to any port 22 proto tcp"
        if p.api_egress_ip
        else "ufw allow 22/tcp"
    )
    return dedent(
        f"""\
        #!/usr/bin/env bash
        set -euo pipefail
        exec > >(tee -a /var/log/underleaf-bootstrap.log) 2>&1
        cd /opt/underleaf
        export DEBIAN_FRONTEND=noninteractive

        post() {{  # post <path> <json>   (retries: the API may be briefly unreachable)
          curl -fsS --retry 8 --retry-delay 5 --retry-all-errors \\
            -H 'content-type: application/json' -X POST "{p.callback_url}/$1" -d "$2"
        }}

        apt-get update -y
        apt-get install -y docker.io docker-compose-v2 ufw jq curl openssl

        mkdir -p certs
        openssl req -newkey rsa:2048 -nodes -keyout certs/server.key \\
          -subj "/CN={p.worker_id}" -out certs/server.csr
        chmod 600 certs/server.key

        resp=$(post register "$(jq -n --rawfile csr certs/server.csr \\
          '{{token: "{p.token}", csr: $csr}}')")
        echo "$resp" | jq -r .certificate > certs/server.crt
        echo "$resp" | jq -r .ca > certs/ca.pem

        ufw --force reset
        ufw default deny incoming
        ufw default allow outgoing
        {ssh_rule}
        ufw allow {TUNNEL_PUBLIC_PORT}/tcp
        {grpc_rule}
        ufw --force enable

        systemctl enable --now docker
        docker compose up -d

        # Ready once conn_worker's gRPC and node ports answer locally.
        for _ in $(seq 1 60); do
          if (echo > /dev/tcp/127.0.0.1/{GRPC_PORT}) 2>/dev/null \\
             && (echo > /dev/tcp/127.0.0.1/{NODE_CONNECTIONS_PORT}) 2>/dev/null; then
            post ready '{{"token": "{p.token}"}}'
            echo "worker ready"
            exit 0
          fi
          sleep 5
        done
        echo "conn_worker did not become ready" >&2
        exit 1
        """
    )


def _file(path: str, content: str, perms: str = "0644") -> str:
    return (
        f"  - path: {path}\n"
        f"    permissions: '{perms}'\n"
        f"    content: |\n{indent(content, ' ' * 6)}"
    )


def render_cloud_init(p: BootstrapParams) -> str:
    files = [
        _file(
            "/opt/underleaf/conn_worker.yaml",
            render_conn_worker_config(p.gateway_domain),
        ),
        _file("/opt/underleaf/nginx.conf", render_nginx_conf(p.grpc_tls_port)),
        _file("/opt/underleaf/docker-compose.yml", render_compose(p.image)),
        _file(
            "/opt/underleaf/certs/ca.pem",
            p.ca_pem if p.ca_pem.endswith("\n") else p.ca_pem + "\n",
        ),
        _file("/opt/underleaf/bootstrap.sh", render_bootstrap_script(p), "0700"),
    ]
    return (
        "#cloud-config\n"
        # Keys only: DO mails a root password to droplets created without a key.
        "ssh_pwauth: false\n"
        "write_files:\n"
        + "\n".join(files)
        + "\nruncmd:\n  - [bash, /opt/underleaf/bootstrap.sh]\n"
    )
