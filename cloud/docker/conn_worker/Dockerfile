FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/bin/conn_worker ./cloud/conn_worker/cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /app/bin/conn_worker /conn_worker
USER nonroot:nonroot
EXPOSE 9070 50102 9020 9021
ENTRYPOINT ["/conn_worker"]
