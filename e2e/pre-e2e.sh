#!/bin/bash
# Pre-e2e script: Build the pruner image, load into kind, and copy workspace
# This runs AFTER the kind cluster is created, on the host machine
set -o errexit
set -o nounset
set -o pipefail

echo "=== Building helm-release-pruner binary (Dockerfile expects pre-built binary) ==="
if command -v go >/dev/null 2>&1; then
  go build -o helm-release-pruner ./cmd/pruner
else
  docker run --rm -v "$(pwd):/app" -w /app golang:1.25-alpine go build -o helm-release-pruner ./cmd/pruner
fi

echo "=== Building helm-release-pruner image ==="
docker build -t helm-release-pruner:test .

echo "=== Loading image into kind cluster ==="
# Find kind binary - rok8s orb may install it in various locations
KIND_BIN=""
for path in /usr/local/bin/kind /home/circleci/bin/kind $(which kind 2>/dev/null || true); do
    if [[ -x "$path" ]]; then
        KIND_BIN="$path"
        break
    fi
done

if [[ -n "$KIND_BIN" ]]; then
    echo "Found kind at: $KIND_BIN"
    "$KIND_BIN" load docker-image helm-release-pruner:test --name e2e
else
    echo "kind not found in PATH, loading image directly to kind node..."
    # Alternative: Load image directly into the kind node's containerd
    docker save helm-release-pruner:test | docker exec -i e2e-control-plane ctr --namespace=k8s.io images import -
fi

echo "=== Copying workspace to command runner ==="
docker cp "$(pwd)" e2e-command-runner:/helm-release-pruner

echo "=== Pre-e2e setup complete ==="
