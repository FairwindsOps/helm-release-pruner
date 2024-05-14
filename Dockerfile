FROM alpine

RUN apk add --update --no-cache bash coreutils curl jq

ENV KUBECTL_VERSION=v1.30.0
ENV HELM_VERSION=v3.14.4

# Install latest helm
RUN curl -LO https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz \
  && tar -zxvf helm-${HELM_VERSION}-linux-amd64.tar.gz \
  && mv linux-amd64/helm /usr/local/bin/helm \
  && chmod +x /usr/local/bin/helm \
  && rm -rf linux-amd64 \
  && rm -f helm-${HELM_VERSION}-linux-amd64.tar.gz

RUN curl -L "https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o "/usr/local/bin/kubectl" \
  && chmod +x "/usr/local/bin/kubectl"

COPY prune-releases.sh /usr/local/bin
