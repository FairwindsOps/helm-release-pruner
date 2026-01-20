FROM alpine:3.23

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
