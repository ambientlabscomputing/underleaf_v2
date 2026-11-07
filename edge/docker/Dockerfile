FROM node:24-slim AS uibuilder

# Set working directory
WORKDIR /app

# Copy dependency definitions and install
COPY orchestrator_ui/package*.json ./
RUN npm install

# Copy source code and build
COPY orchestrator_ui/ .
RUN npm run build

# The Orchestrator serves the UI and the API from one listener (one origin), so
# there is no separate web server in the image.
FROM alpine:3.20 AS runner

# Target architecture is injected by BuildKit
ARG TARGETARCH

# CA certificates: the Orchestrator makes outbound HTTPS calls (GitHub, cloud).
RUN apk add --no-cache ca-certificates

COPY --from=uibuilder /app/dist /usr/share/underleaf/ui
COPY bin/orch-server-linux-${TARGETARCH} /usr/local/bin/orch-server

# Inside the container the Orchestrator's local listeners (REST, local gRPC) must
# bind the container's interface for Docker's port publishing to reach them.
# They have no application-level auth, so exposure is decided by how the ports
# are published: bind them to loopback (-p 127.0.0.1:...), as
# orchestrator_ui/public/tools/install.sh does. UNDERLEAF_CONTAINER=1 is what
# permits the wildcard bind, and only when the process really is in a container;
# it never applies to the cluster listener (worker nodes), which is mTLS-only
# and needs no switch. Do not publish the local ports on a non-loopback address.
ENV UNDERLEAF_BIND_ADDR=0.0.0.0 \
    UNDERLEAF_CONTAINER=1 \
    UNDERLEAF_UI_DIR=/usr/share/underleaf/ui

# The Orchestrator keeps its state in paths relative to its working directory:
# the database (orchestrator.db, which also holds API tokens), the cluster CA
# and node certificates (cluster-ca/), and cloud-issued certs (certs/). Make that
# directory a volume so the state survives `docker rm` and image upgrades.
# install.sh mounts a named volume here; without one Docker creates an anonymous
# volume, which also survives `docker rm` but is easy to lose track of.
WORKDIR /data
VOLUME /data

# 9090: UI + REST API (one origin). 50100: agent gRPC.
EXPOSE 9090 50100
CMD ["/usr/local/bin/orch-server", "run"]
