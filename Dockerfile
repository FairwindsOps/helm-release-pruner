FROM alpine

RUN apk add --update --no-cache bash coreutils curl jq

ENV KUBECTL_VERSION=v1.15.10
ENV HELM_VERSION=v3.1.2

RUN curl -L "https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VERSION}-linux-amd64.tar.gz" | tar xzvf - -C "/tmp/" \
  && mv "/tmp/linux-amd64/helm" "/usr/local/bin/helm" \
  && chmod +x "/usr/local/bin/helm" \
  && rm -rf "/tmp/linux-amd64"

RUN curl -L "https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o "/usr/local/bin/kubectl" \
  && chmod +x "/usr/local/bin/kubectl"

COPY prune-releases.sh /usr/local/bin
