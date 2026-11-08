#!/bin/sh

# Usage:
#   install.sh [tag]                      install a manager node (single-node cluster)
#   install.sh join <join-string> [tag]   add this machine to an existing cluster
#                                         (the string comes from `orcli nodes add`)
#
# Everything runs as native processes under your user account: the Orchestrator
# (orch-server) and the agent (ufagentd) are launchd agents on macOS and systemd
# user services on Linux, and orcli is a plain command. Docker is only used by the
# agent, to run your workloads.
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
if [ -n "$2" ]; then
    echo "usage: install.sh [tag]  (the Orchestrator listens on 127.0.0.1:9090 and 127.0.0.1:50100)" >&2
    exit 1
fi
if [ -z "$TAG" ]; then
    TAG="latest"
fi

REPO="ambientlabscomputing/underleaf_v2"
HTTP_PORT="9090"
# These can be overridden for a mirror or a local test; the defaults are the
# public release.
#   UNDERLEAF_RELEASE_BASE  where ufagent/ufagentd/orcli/orch-server-<os>-<arch> and
#                           underleaf-ui.tar.gz are downloaded from
#   UNDERLEAF_INSTALL_DIR   where the binaries go
#   UNDERLEAF_DATA_DIR      where the Orchestrator keeps its state
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

# The Orchestrator's state (database with API tokens, cluster CA and its private
# key, cloud-issued certificates) lives in one directory that only you can read.
if [ -n "$UNDERLEAF_DATA_DIR" ]; then
    DATA_DIR="$UNDERLEAF_DATA_DIR"
elif [ "$GOOS" = "darwin" ]; then
    DATA_DIR="${HOME}/Library/Application Support/Underleaf"
else
    DATA_DIR="${XDG_DATA_HOME:-${HOME}/.local/share}/underleaf"
fi
UI_DIR="${DATA_DIR}/ui"
# The agent keeps its local database in its working directory. A launchd agent
# starts in "/", where it cannot write, so it gets a directory of its own.
AGENT_DIR="${DATA_DIR}/agent"

mkdir -p "$DATA_DIR"
chmod 700 "$DATA_DIR"

# ── Download ──────────────────────────────────────────────────────────────────
# Download to a temporary name and rename into place: overwriting a running
# executable in place fails on Linux and can get the process killed on macOS.
fetch() { # fetch <url> <dest> [chmod-mode]
    TMP_DL="$2.download.$$"
    if ! curl -fSL "$1" -o "$TMP_DL"; then
        rm -f "$TMP_DL"
        echo "Failed to download $1" >&2
        exit 1
    fi
    [ -n "$3" ] && chmod "$3" "$TMP_DL"
    mv -f "$TMP_DL" "$2"
}

echo "Installing binaries from release '${TAG}' (${GOOS}/${GOARCH}) to ${INSTALL_DIR} ..."
mkdir -p "$INSTALL_DIR"

BINS="ufagent ufagentd"
if [ "$MODE" = "install" ]; then
    BINS="ufagent ufagentd orcli orch-server"
fi
for BIN in $BINS; do
    echo "  Downloading ${BIN}..."
    fetch "${RELEASE_BASE}/${BIN}-${GOOS}-${GOARCH}" "${INSTALL_DIR}/${BIN}" 755
done

echo "Binaries installed. Make sure ${INSTALL_DIR} is in your PATH."

# ── Run a binary as a user service ────────────────────────────────────────────
# install_service <name> <description> [<workdir> <ui-dir>]
# The binary is ${INSTALL_DIR}/<name>, started as `<name> run`.
install_service() {
    SVC_NAME="$1"
    SVC_DESC="$2"
    SVC_WORKDIR="$3"
    SVC_UIDIR="$4"
    SVC_BIN="${INSTALL_DIR}/${SVC_NAME}"

    case "$GOOS" in
        linux)
            SERVICE_DIR="${HOME}/.config/systemd/user"
            SERVICE_FILE="${SERVICE_DIR}/${SVC_NAME}.service"
            mkdir -p "$SERVICE_DIR"
            {
                echo "[Unit]"
                echo "Description=${SVC_DESC}"
                echo "After=network.target"
                echo ""
                echo "[Service]"
                echo "ExecStart=\"${SVC_BIN}\" run"
                [ -n "$SVC_WORKDIR" ] && echo "WorkingDirectory=${SVC_WORKDIR}"
                [ -n "$SVC_UIDIR" ] && echo "Environment=\"UNDERLEAF_UI_DIR=${SVC_UIDIR}\""
                echo "Restart=on-failure"
                echo "RestartSec=5"
                echo ""
                echo "[Install]"
                echo "WantedBy=default.target"
            } > "$SERVICE_FILE"
            systemctl --user daemon-reload
            systemctl --user enable "$SVC_NAME"
            # restart, not just start: a re-run installs new binaries
            systemctl --user restart "$SVC_NAME"
            # Allow the service to run without an active login session
            loginctl enable-linger "$USER" 2>/dev/null || true
            echo "${SVC_NAME} installed as a systemd user service."
            echo "  Status:   systemctl --user status ${SVC_NAME}"
            echo "  Logs:     journalctl --user -u ${SVC_NAME} -f"
            echo "  Uninstall: systemctl --user disable --now ${SVC_NAME} && rm ${SERVICE_FILE}"
            ;;

        darwin)
            PLIST_DIR="${HOME}/Library/LaunchAgents"
            PLIST_FILE="${PLIST_DIR}/com.ambientlabs.${SVC_NAME}.plist"
            LOG_DIR="${HOME}/Library/Logs/${SVC_NAME}"
            mkdir -p "$PLIST_DIR" "$LOG_DIR"
            {
                echo '<?xml version="1.0" encoding="UTF-8"?>'
                echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">'
                echo '<plist version="1.0">'
                echo '<dict>'
                echo '    <key>Label</key>'
                echo "    <string>com.ambientlabs.${SVC_NAME}</string>"
                echo '    <key>ProgramArguments</key>'
                echo '    <array>'
                echo "        <string>${SVC_BIN}</string>"
                echo '        <string>run</string>'
                echo '    </array>'
                if [ -n "$SVC_WORKDIR" ]; then
                    echo '    <key>WorkingDirectory</key>'
                    echo "    <string>${SVC_WORKDIR}</string>"
                fi
                if [ -n "$SVC_UIDIR" ]; then
                    echo '    <key>EnvironmentVariables</key>'
                    echo '    <dict>'
                    echo '        <key>UNDERLEAF_UI_DIR</key>'
                    echo "        <string>${SVC_UIDIR}</string>"
                    echo '    </dict>'
                fi
                echo '    <key>KeepAlive</key>'
                echo '    <true/>'
                echo '    <key>RunAtLoad</key>'
                echo '    <true/>'
                echo '    <key>StandardOutPath</key>'
                echo "    <string>${LOG_DIR}/stdout.log</string>"
                echo '    <key>StandardErrorPath</key>'
                echo "    <string>${LOG_DIR}/stderr.log</string>"
                echo '</dict>'
                echo '</plist>'
            } > "$PLIST_FILE"
            # Unload any existing instance before reloading
            launchctl unload "$PLIST_FILE" 2>/dev/null || true
            launchctl load -w "$PLIST_FILE"
            echo "${SVC_NAME} installed as a launchd user agent."
            echo "  Status:   launchctl list | grep ${SVC_NAME}"
            echo "  Logs:     tail -f ${LOG_DIR}/stderr.log"
            echo "  Uninstall: launchctl unload ${PLIST_FILE} && rm ${PLIST_FILE}"
            ;;
    esac
}

install_agent() {
    mkdir -p "$AGENT_DIR"
    chmod 700 "$AGENT_DIR"
    install_service ufagentd "Underleaf Edge Agent Daemon" "$AGENT_DIR"
}

# A joining node enrolls before its service starts, so the daemon comes up as a
# cluster member. A failed join installs nothing as a service.
if [ "$MODE" = "join" ]; then
    echo "Joining the cluster ..."
    if ! "${INSTALL_DIR}/ufagentd" join "$JOIN_STRING"; then
        echo "Join failed; no service was installed. Ask for a fresh string with 'orcli nodes add' and try again." >&2
        exit 1
    fi
    install_agent
    echo "Done! This machine is now a worker in the cluster."
    exit 0
fi

# ── Manager: the Orchestrator ─────────────────────────────────────────────────
# The UI is a set of static files the Orchestrator serves next to its API.
echo "  Downloading the UI..."
UI_TMP="${DATA_DIR}/ui.new.$$"
UI_TAR="${DATA_DIR}/ui.tar.gz.$$"
fetch "${RELEASE_BASE}/underleaf-ui.tar.gz" "$UI_TAR"
mkdir -p "$UI_TMP"
if ! tar -xzf "$UI_TAR" -C "$UI_TMP"; then
    rm -rf "$UI_TMP" "$UI_TAR"
    echo "The downloaded UI archive is not valid." >&2
    exit 1
fi
rm -f "$UI_TAR"
rm -rf "$UI_DIR"
mv "$UI_TMP" "$UI_DIR"

# Earlier versions ran the Orchestrator in a Docker container named `underleaf`.
# It cannot run alongside this one (same ports, same state), so move its state
# here once, and stop it. The container is kept (stopped, renamed) as a backup.
if command -v docker >/dev/null 2>&1 \
        && [ "$(docker inspect -f '{{.State.Running}}' underleaf 2>/dev/null)" = "true" ]; then
    echo "Found the old Docker-based Orchestrator ('underleaf'); moving its state to ${DATA_DIR} ..."
    docker stop underleaf >/dev/null
    if [ ! -e "${DATA_DIR}/orchestrator.db" ]; then
        for F in orchestrator.db orchestrator.db-wal orchestrator.db-shm certs cluster-ca; do
            # Older images keep state in the working directory (/); newer ones in /data.
            docker cp "underleaf:/data/${F}" "${DATA_DIR}/${F}" >/dev/null 2>&1 \
                || docker cp "underleaf:/${F}" "${DATA_DIR}/${F}" >/dev/null 2>&1 || true
        done
        if [ ! -e "${DATA_DIR}/orchestrator.db" ]; then
            echo "Could not find orchestrator.db in the old container; restarting it and stopping here." >&2
            docker start underleaf >/dev/null
            exit 1
        fi
    fi
    BACKUP="underleaf-backup-$(date +%Y%m%d%H%M%S)"
    docker rename underleaf "$BACKUP"
    docker update --restart no "$BACKUP" >/dev/null
    chmod -R go-rwx "$DATA_DIR"
    echo "The old container is kept (stopped) as '${BACKUP}', and its volume 'underleaf-data' is untouched."
    echo "Once you have checked the new install: docker rm ${BACKUP} && docker volume rm underleaf-data"
fi

echo "Starting the Orchestrator ..."
install_service orch-server "Underleaf Orchestrator" "$DATA_DIR" "$UI_DIR"

# Wait until it answers before starting the agent that links to it.
READY=""
TRIES=0
while [ "$TRIES" -lt 30 ]; do
    if curl -s -o /dev/null --max-time 2 "http://127.0.0.1:${HTTP_PORT}/"; then
        READY=1
        break
    fi
    TRIES=$((TRIES + 1))
    sleep 1
done
if [ -z "$READY" ]; then
    echo "The Orchestrator did not answer on 127.0.0.1:${HTTP_PORT} within 30 seconds. Is the port in use? Check the service logs above." >&2
    exit 1
fi

install_agent

echo "Done! UI available at http://localhost:${HTTP_PORT}"
echo "To add a worker node: orcli nodes add --advertise <this-machine's-address>:50104"
