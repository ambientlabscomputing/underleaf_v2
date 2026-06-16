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

# Expose port and start Nginx
EXPOSE 80 50100
CMD ["/usr/local/bin/run-all.sh"]
