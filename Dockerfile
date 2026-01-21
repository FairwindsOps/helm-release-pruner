FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates git

# Download dependencies first (better layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Build the binary
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /helm-release-pruner \
    ./cmd/pruner

# Final image
FROM alpine:3.21

LABEL org.opencontainers.image.authors="FairwindsOps, Inc." \
      org.opencontainers.image.vendor="FairwindsOps, Inc." \
      org.opencontainers.image.title="helm-release-pruner" \
      org.opencontainers.image.description="Automatically delete old Helm releases from your Kubernetes cluster" \
      org.opencontainers.image.documentation="https://github.com/FairwindsOps/helm-release-pruner" \
      org.opencontainers.image.source="https://github.com/FairwindsOps/helm-release-pruner" \
      org.opencontainers.image.url="https://github.com/FairwindsOps/helm-release-pruner" \
      org.opencontainers.image.licenses="Apache License 2.0"

# Install ca-certificates for HTTPS connections to Kubernetes API
RUN apk add --no-cache ca-certificates

COPY --from=builder /helm-release-pruner /helm-release-pruner

USER nobody

ENTRYPOINT ["/helm-release-pruner"]
