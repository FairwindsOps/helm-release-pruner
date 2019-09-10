FROM alpine

RUN apk add --update --no-cache bash coreutils curl

ENV KUBECTL_VERSION=v1.13.10
ENV HELM_VERSION=v2.13.1

COPY _get_helm.sh /usr/local/bin/
RUN bash -c 'for ver in v2.13.{0,1} v2.14.{0,1,2,3}; do _get_helm.sh $ver; done'

RUN curl -L "https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o "/usr/local/bin/kubectl" \
  && chmod +x "/usr/local/bin/kubectl"

COPY prune-releases.sh /usr/local/bin
