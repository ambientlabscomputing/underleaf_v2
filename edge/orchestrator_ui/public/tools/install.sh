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

# pull ufagent, ufagentd, and orcli binaries from ghcr
# TODO: waiting on actions to publish these binaries to ghcr

# pull orchestrator via docker
docker pull ghcr.io/ambientlabscomputing/underleaf:$TAG

# start the orchestrator server
docker run -d \
    -p $HTTP_PORT:80 \
    -p $GRPC_PORT:50100 \
    --name underleaf ghcr.io/ambientlabscomputing/underleaf:$TAG

