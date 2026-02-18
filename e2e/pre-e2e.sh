#!/bin/bash
# Pre-e2e script: Build the pruner image, load into kind, and copy workspace
set -o errexit
set -o nounset
set -o pipefail

# Repo root (parent of e2e/); with golang-exec the workspace is the repo
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== Building helm-release-pruner binary (Dockerfile expects pre-built binary) ==="
# Build for Linux so the image runs in the container (works on Mac local and CI Linux)
CGO_ENABLED=0 GOOS=linux go build -o helm-release-pruner ./cmd/pruner

echo "=== Building helm-release-pruner image ==="
docker build -t helm-release-pruner:test .

echo "=== Loading image into kind cluster ==="
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
  docker save helm-release-pruner:test | docker exec -i e2e-control-plane ctr --namespace=k8s.io images import -
fi

echo "=== Copying workspace to command runner ==="
docker cp "$REPO_ROOT" e2e-command-runner:/helm-release-pruner

echo "=== Pre-e2e setup complete ==="
