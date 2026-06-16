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
