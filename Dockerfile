FROM --platform=$BUILDPLATFORM golang:1.25.6 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags="-s -w -X github.com/skidoodle/safebin/internal/app.Version=$VERSION" \
    -trimpath \
    -o /app/safebin .

FROM debian:trixie-slim

LABEL org.opencontainers.image.source="https://github.com/skidoodle/safebin"
LABEL org.opencontainers.image.description="Minimalist, self-hosted file storage with Zero-Knowledge at Rest encryption."
LABEL org.opencontainers.image.licenses="GPL-2.0-only"

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    media-types \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 10001 -s /bin/bash appuser
WORKDIR /app

COPY --from=builder /app/safebin .

RUN mkdir -p /app/storage && chown 10001:10001 /app/storage
VOLUME ["/app/storage"]

USER 10001
EXPOSE 8080

ENTRYPOINT ["/app/safebin"]
