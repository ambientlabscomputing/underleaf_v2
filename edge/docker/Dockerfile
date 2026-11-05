FROM node:24-slim AS uibuilder

# Set working directory
WORKDIR /app

# Copy dependency definitions and install
COPY orchestrator_ui/package*.json ./
RUN npm install

# Copy source code and build
COPY orchestrator_ui/ .
RUN npm run build

FROM nginx:stable-alpine AS runner

# Target architecture is injected by BuildKit
ARG TARGETARCH

# Copy the static build output to Nginx's web root
COPY --from=uibuilder /app/dist /usr/share/nginx/html
COPY bin/orch-server-linux-${TARGETARCH} /usr/local/bin/orch-server

# Copy custom Nginx configuration to handle SPA routing (optional but recommended)
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY docker/run-all.sh /usr/local/bin/run-all.sh
RUN chmod +x /usr/local/bin/run-all.sh

# Inside the container the Orchestrator's local listeners (REST, local gRPC) must
# bind the container's interface for Docker's port publishing to reach them.
# They have no application-level auth, so exposure is decided by how the ports
# are published: bind them to loopback (-p 127.0.0.1:...), as
# orchestrator_ui/public/tools/install.sh does. UNDERLEAF_CONTAINER=1 is what
# permits the wildcard bind, and only when the process really is in a container;
# it never applies to the cluster listener (worker nodes), which is mTLS-only
# and needs no switch. Do not publish the local ports on a non-loopback address.
ENV UNDERLEAF_BIND_ADDR=0.0.0.0 \
    UNDERLEAF_CONTAINER=1

# Expose port and start Nginx
EXPOSE 80 50100
CMD ["/usr/local/bin/run-all.sh"]
