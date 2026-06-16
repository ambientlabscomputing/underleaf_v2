#!/bin/sh

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
DOCKER_IMAGE="ghcr.io/ambientlabscomputing/underleaf"
INSTALL_DIR="${HOME}/.local/bin"
RELEASE_BASE="https://github.com/${REPO}/releases/download/${TAG}"

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

for BIN in ufagent ufagentd orcli; do
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

install_service

# ── Pull and run the orchestrator via Docker ──────────────────────────────────
echo "Pulling ${DOCKER_IMAGE}:${TAG} ..."
docker pull "${DOCKER_IMAGE}:${TAG}"

echo "Starting orchestrator (HTTP :${HTTP_PORT}, gRPC :${GRPC_PORT}) ..."
docker run -d \
    -p "${HTTP_PORT}:80" \
    -p "${GRPC_PORT}:50100" \
    --restart unless-stopped \
    --name underleaf "${DOCKER_IMAGE}:${TAG}"

echo "Done! UI available at http://localhost:${HTTP_PORT}"
