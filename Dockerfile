# Same pattern as pluto and insights-cli: GoReleaser builds the binary per platform;
# this image only copies it. No RUN steps, so multi-arch builds work without QEMU.
FROM alpine:3.21

LABEL org.opencontainers.image.authors="FairwindsOps, Inc." \
      org.opencontainers.image.vendor="FairwindsOps, Inc." \
      org.opencontainers.image.title="helm-release-pruner" \
      org.opencontainers.image.description="Automatically delete old Helm releases from your Kubernetes cluster" \
      org.opencontainers.image.documentation="https://github.com/FairwindsOps/helm-release-pruner" \
      org.opencontainers.image.source="https://github.com/FairwindsOps/helm-release-pruner" \
      org.opencontainers.image.url="https://github.com/FairwindsOps/helm-release-pruner" \
      org.opencontainers.image.licenses="Apache License 2.0"

USER nobody
COPY helm-release-pruner /
ENTRYPOINT ["/helm-release-pruner"]

