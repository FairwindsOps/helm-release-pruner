#!/bin/bash

function _get_helm() {
    local version
    version="${1}"

    echo "Checking if helm-${version} is already downloaded"
    if which "helm-${version}"; then
        echo "Version found! Skipping Download and Install"
        return 0
    else
        echo "Version not found, downloading and moving to /usr/local/bin/"
    fi

    echo "Downloading Helm ${version}"
    curl -sfL "https://storage.googleapis.com/kubernetes-helm/helm-${version}-linux-amd64.tar.gz" | tar xzvf - -C "/tmp/" > /dev/null 2>&1

    echo "Setting up helm install as /usr/local/bin/helm-${version}"
    mv "/tmp/linux-amd64/helm" "/usr/local/bin/helm-${version}"
    chmod +x "/usr/local/bin/helm-${version}"

    echo "Cleaning up old download folder"
    rm -rf "/tmp/linux-amd64"
}

function _set_helm_version() {
    local version
    version="${1}"
    echo "Checking if /usr/local/bin/helm-${version} exists already."
    if [ -f "$(which helm-${version})" ]; then
        echo "Version found! Adding symlink to /use/local/bin/helm"
        ln -s /usr/local/bin/helm-${version} /usr/local/bin/helm
    else
        echo "Failed finding the desired helm version locally in /usr/local/bin/helm-${version}"
        return 1
    fi
}

if [ $# -ne 0 ]; then
  _get_helm $@
fi
