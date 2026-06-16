FROM golang:1.25 AS gobuilder

WORKDIR /app

# Copy module files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code
COPY . .

# Explicitly pull BuildKit's automatic platform arguments
ARG TARGETOS
ARG TARGETARCH

# Compile the Go binary using target OS options
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o orch-server ./orchestrator/cmd/server

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

# Copy the static build output to Nginx's web root
COPY --from=uibuilder /app/dist /usr/share/nginx/html
COPY --from=gobuilder /app/orch-server /usr/local/bin/orch-server

# Copy custom Nginx configuration to handle SPA routing (optional but recommended)
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY docker/run-all.sh /usr/local/bin/run-all.sh
RUN chmod +x /usr/local/bin/run-all.sh

# Expose port and start Nginx
EXPOSE 80 50100
CMD ["/usr/local/bin/run-all.sh"]
