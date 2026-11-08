#!/bin/sh

# Usage:
#   install.sh [tag] [http-port] [grpc-port]   install a manager node (single-node cluster)
#   install.sh join <join-string> [tag]        add this machine to an existing cluster
#                                              (the string comes from `orcli nodes add`)
MODE="install"
if [ "$1" = "join" ]; then
    MODE="join"
    JOIN_STRING="$2"
    if [ -z "$JOIN_STRING" ]; then
        echo "usage: install.sh join <join-string> [tag]" >&2
        exit 1
    fi
    shift
    shift
fi

TAG=$1
HTTP_PORT=$2
GRPC_PORT=$3

if [ -z "$TAG" ]; then
    TAG="latest"
fi
if [ -z "$HTTP_PORT" ]; then
    HTTP_PORT="9090"
fi
if [ -z "$GRPC_PORT" ]; then
    GRPC_PORT="50100"
fi

REPO="ambientlabscomputing/underleaf_v2"
# These can be overridden for a mirror, a private registry or a local test; the
# defaults are the public release.
#   UNDERLEAF_RELEASE_BASE  where ufagent/ufagentd/orcli-<os>-<arch> are downloaded from
#   UNDERLEAF_DOCKER_IMAGE  the Orchestrator image (without the tag)
#   UNDERLEAF_INSTALL_DIR   where the binaries go
#   UNDERLEAF_SKIP_PULL=1   use the image already present locally
DOCKER_IMAGE="${UNDERLEAF_DOCKER_IMAGE:-ghcr.io/ambientlabscomputing/underleaf}"
INSTALL_DIR="${UNDERLEAF_INSTALL_DIR:-${HOME}/.local/bin}"
RELEASE_BASE="${UNDERLEAF_RELEASE_BASE:-https://github.com/${REPO}/releases/download/${TAG}}"

# ── Detect OS and architecture ────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Linux)  GOOS="linux" ;;
    Darwin) GOOS="darwin" ;;
    *)
        echo "Unsupported OS: $OS" >&2
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64)          GOARCH="amd64" ;;
    aarch64|arm64)   GOARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH" >&2
        exit 1
        ;;
esac

# ── Install binaries ──────────────────────────────────────────────────────────
echo "Installing binaries from release '${TAG}' (${GOOS}/${GOARCH}) to ${INSTALL_DIR} ..."
mkdir -p "$INSTALL_DIR"

BINS="ufagent ufagentd orcli"
if [ "$MODE" = "join" ]; then
    BINS="ufagent ufagentd" # a worker needs no orcli
fi
for BIN in $BINS; do
    URL="${RELEASE_BASE}/${BIN}-${GOOS}-${GOARCH}"
    echo "  Downloading ${BIN}..."
    if ! curl -fSL "$URL" -o "${INSTALL_DIR}/${BIN}"; then
        echo "Failed to download ${BIN} from ${URL}" >&2
        exit 1
    fi
    chmod +x "${INSTALL_DIR}/${BIN}"
done

echo "Binaries installed. Make sure ${INSTALL_DIR} is in your PATH."

# ── Register ufagentd as a daemon ─────────────────────────────────────────────
install_service() {
    UFAGENTD="${INSTALL_DIR}/ufagentd"

    case "$GOOS" in
        linux)
            SERVICE_DIR="${HOME}/.config/systemd/user"
            SERVICE_FILE="${SERVICE_DIR}/ufagentd.service"
            mkdir -p "$SERVICE_DIR"
            cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Underleaf Edge Agent Daemon
After=network.target

[Service]
ExecStart=${UFAGENTD} run
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
            systemctl --user daemon-reload
            systemctl --user enable --now ufagentd
            # Allow the service to run without an active login session
            loginctl enable-linger "$USER" 2>/dev/null || true
            echo "ufagentd installed as a systemd user service."
            echo "  Status:   systemctl --user status ufagentd"
            echo "  Logs:     journalctl --user -u ufagentd -f"
            echo "  Uninstall: systemctl --user disable --now ufagentd && rm ${SERVICE_FILE}"
            ;;

        darwin)
            PLIST_DIR="${HOME}/Library/LaunchAgents"
            PLIST_FILE="${PLIST_DIR}/com.ambientlabs.ufagentd.plist"
            LOG_DIR="${HOME}/Library/Logs/ufagentd"
            mkdir -p "$PLIST_DIR" "$LOG_DIR"
            cat > "$PLIST_FILE" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ambientlabs.ufagentd</string>
    <key>ProgramArguments</key>
    <array>
        <string>${UFAGENTD}</string>
        <string>run</string>
    </array>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>${LOG_DIR}/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>${LOG_DIR}/stderr.log</string>
</dict>
</plist>
EOF
            # Unload any existing instance before reloading
            launchctl unload "$PLIST_FILE" 2>/dev/null || true
            launchctl load -w "$PLIST_FILE"
            echo "ufagentd installed as a launchd user agent."
            echo "  Status:   launchctl list | grep ufagentd"
            echo "  Logs:     tail -f ${LOG_DIR}/stderr.log"
            echo "  Uninstall: launchctl unload ${PLIST_FILE} && rm ${PLIST_FILE}"
            ;;
    esac
}

# A joining node enrolls before its service starts, so the daemon comes up as a
# cluster member. A failed join installs nothing as a service.
if [ "$MODE" = "join" ]; then
    echo "Joining the cluster ..."
    if ! "${INSTALL_DIR}/ufagentd" join "$JOIN_STRING"; then
        echo "Join failed; no service was installed. Ask for a fresh string with 'orcli nodes add' and try again." >&2
        exit 1
    fi
fi

install_service

if [ "$MODE" = "join" ]; then
    echo "Done! This machine is now a worker in the cluster."
    exit 0
fi

# ── Pull and run the orchestrator via Docker ──────────────────────────────────
if [ "${UNDERLEAF_SKIP_PULL:-}" = "1" ]; then
    echo "Using local image ${DOCKER_IMAGE}:${TAG} (UNDERLEAF_SKIP_PULL=1)"
else
    echo "Pulling ${DOCKER_IMAGE}:${TAG} ..."
    docker pull "${DOCKER_IMAGE}:${TAG}"
fi

# The orchestrator gets its own user-defined network. Workloads run on a
# different one (created by the agent), so a compromised workload cannot reach
# the orchestrator's ports; the host agent still reaches it through the
# loopback-published ports below. (RFD 5, step 1)
ORCH_NETWORK="underleaf-control"
docker network inspect "$ORCH_NETWORK" >/dev/null 2>&1 || docker network create "$ORCH_NETWORK" >/dev/null

# The orchestrator's state (database, API tokens, cluster CA, certificates) lives
# in /data inside the container. Keep it in a named volume so it survives
# recreating the container or upgrading the image.
DATA_VOLUME="underleaf-data"

# The cluster listener (worker nodes joining over mTLS) is published so that
# `orcli nodes add` works without recreating the container. Nothing listens on it
# until the first `orcli nodes add`, and it then accepts mutual-TLS connections
# only. UNDERLEAF_CLUSTER_PUBLISH picks the host interface ("0.0.0.0" by
# default, "127.0.0.1" for a single machine, "none" to not publish at all).
CLUSTER_PORT="${UNDERLEAF_CLUSTER_PORT:-50104}"
CLUSTER_PUBLISH="${UNDERLEAF_CLUSTER_PUBLISH:-0.0.0.0}"

run_orchestrator() {
    echo "Starting orchestrator (HTTP :${HTTP_PORT}, gRPC :${GRPC_PORT}) ..."
    docker volume inspect "$DATA_VOLUME" >/dev/null 2>&1 || docker volume create "$DATA_VOLUME" >/dev/null
    if [ "$CLUSTER_PUBLISH" = "none" ]; then
        docker run -d \
            --network "$ORCH_NETWORK" \
            -v "${DATA_VOLUME}:/data" \
            -p "127.0.0.1:${HTTP_PORT}:9090" \
            -p "127.0.0.1:${GRPC_PORT}:50100" \
            --restart unless-stopped \
            --name underleaf "${DOCKER_IMAGE}:${TAG}"
    else
        docker run -d \
            --network "$ORCH_NETWORK" \
            -v "${DATA_VOLUME}:/data" \
            -p "127.0.0.1:${HTTP_PORT}:9090" \
            -p "127.0.0.1:${GRPC_PORT}:50100" \
            -p "${CLUSTER_PUBLISH}:${CLUSTER_PORT}:50104" \
            --restart unless-stopped \
            --name underleaf "${DOCKER_IMAGE}:${TAG}"
    fi
}

has_data_volume() {
    # Only our named volume counts: the image also declares VOLUME /data, so a
    # container started by hand without -v has an anonymous volume there.
    [ -n "$(docker inspect -f '{{range .Mounts}}{{if and (eq .Destination "/data") (eq .Name "'"$DATA_VOLUME"'")}}x{{end}}{{end}}' underleaf 2>/dev/null)" ]
}

if docker inspect underleaf >/dev/null 2>&1; then
    if has_data_volume; then
        # Existing install that already keeps its state in the volume: only move
        # it onto the control network. The restart is required: without it Docker
        # leaves the published ports bound to the old network endpoint and the
        # host can no longer reach the container. To upgrade the image, run
        # `docker rm -f underleaf` and re-run this installer; the volume is kept.
        docker network connect "$ORCH_NETWORK" underleaf 2>/dev/null || true
        docker network disconnect bridge underleaf 2>/dev/null || true
        docker restart underleaf >/dev/null
        echo "Existing 'underleaf' container moved to network '${ORCH_NETWORK}'. Its state is in volume '${DATA_VOLUME}'."
        echo "To switch to ${DOCKER_IMAGE}:${TAG}: docker rm -f underleaf, then re-run this installer (the state is kept)."
        if [ -z "$(docker port underleaf 50104/tcp 2>/dev/null)" ]; then
            echo "This container does not publish the cluster port (${CLUSTER_PORT}), so worker nodes cannot join it. A container's published ports cannot change: docker rm -f underleaf and re-run this installer to add it (the state is kept)."
        fi
    elif [ "${UNDERLEAF_MIGRATE_STATE:-}" = "1" ]; then
        # Older container: its state is in the container's own filesystem, which
        # is lost when the container is removed. Copy it into the volume, keep the
        # old container (stopped, renamed) as a backup, and start the new one.
        BACKUP="underleaf-backup-$(date +%Y%m%d%H%M%S)"
        TMP_STATE="$(mktemp -d)"
        echo "Stopping 'underleaf' and copying its state into volume '${DATA_VOLUME}' ..."
        docker stop underleaf >/dev/null
        for F in orchestrator.db orchestrator.db-wal orchestrator.db-shm certs cluster-ca; do
            # Older images keep state in the working directory (/); a container
            # started by hand from the new image keeps it in /data.
            docker cp "underleaf:/${F}" "${TMP_STATE}/${F}" >/dev/null 2>&1 \
                || docker cp "underleaf:/data/${F}" "${TMP_STATE}/${F}" >/dev/null 2>&1 || true
        done
        if [ ! -e "${TMP_STATE}/orchestrator.db" ]; then
            echo "Could not find orchestrator.db in the old container; leaving it untouched (restarting it)." >&2
            docker start underleaf >/dev/null
            rm -rf "$TMP_STATE"
            exit 1
        fi
        docker volume inspect "$DATA_VOLUME" >/dev/null 2>&1 || docker volume create "$DATA_VOLUME" >/dev/null
        if ! docker run --rm --entrypoint sh -v "${DATA_VOLUME}:/data" -v "${TMP_STATE}:/src:ro" \
                "${DOCKER_IMAGE}:${TAG}" -c 'cp -a /src/. /data/'; then
            echo "Copying state into the volume failed; restarting the old container unchanged." >&2
            docker start underleaf >/dev/null
            rm -rf "$TMP_STATE"
            exit 1
        fi
        rm -rf "$TMP_STATE"
        docker rename underleaf "$BACKUP"
        docker update --restart no "$BACKUP" >/dev/null
        run_orchestrator
        echo "Old container kept (stopped) as '${BACKUP}'. Remove it once you have checked the new install: docker rm ${BACKUP}"
    else
        # Older container whose state is only in its own filesystem. Do not
        # recreate it on our own: that would discard the state.
        docker network connect "$ORCH_NETWORK" underleaf 2>/dev/null || true
        docker network disconnect bridge underleaf 2>/dev/null || true
        docker restart underleaf >/dev/null
        echo "Existing 'underleaf' container moved to network '${ORCH_NETWORK}'."
        echo "It keeps its current image, and its state is not in the '${DATA_VOLUME}' volume (removing the container could lose it)."
        echo "To move to ${DOCKER_IMAGE}:${TAG} and keep the state, re-run with: UNDERLEAF_MIGRATE_STATE=1 (the old container is kept as a stopped backup)."
    fi
else
    run_orchestrator
fi

echo "Done! UI available at http://localhost:${HTTP_PORT}"
if [ "$CLUSTER_PUBLISH" != "none" ]; then
    echo "To add a worker node: orcli nodes add --advertise <this-machine's-address>:${CLUSTER_PORT}"
fi
