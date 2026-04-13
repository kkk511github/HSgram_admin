FROM golang:1.23-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY HSgram_admin/backend/go.mod HSgram_admin/backend/go.sum ./HSgram_admin/backend/
COPY HSgram_server ./HSgram_server

WORKDIR /src/HSgram_admin/backend
RUN go mod download

COPY HSgram_admin/backend ./ 

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/admin-api ./cmd/admin-api

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home appuser

WORKDIR /app

COPY --from=builder /out/admin-api /app/admin-api

ENV ADMIN_LISTEN_ADDR=:8088

EXPOSE 8088

USER appuser

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 CMD curl -fsS http://127.0.0.1:8088/api/healthz || exit 1

ENTRYPOINT ["/app/admin-api"]
