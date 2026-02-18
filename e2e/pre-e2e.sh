#!/bin/bash
# Pre-e2e script: Build the pruner image, load into kind, and copy workspace
# This runs AFTER the kind cluster is created, on the host machine
set -o errexit
set -o nounset
set -o pipefail

# Ensure we run from repo root: find directory that contains go.mod (rok8s orb may copy only e2e/ or use another layout)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR/.."
for _ in 1 2 3 4 5; do
  if [[ -f "$REPO_ROOT/go.mod" ]]; then
    break
  fi
  REPO_ROOT="$REPO_ROOT/.."
done
REPO_ROOT="$(cd "$REPO_ROOT" && pwd)"
if [[ ! -f "$REPO_ROOT/go.mod" ]]; then
  # Fallback: CI workspace env or common paths
  for dir in "${CIRCLE_WORKING_DIRECTORY:-}" /home/circleci/project /root/project "$SCRIPT_DIR/.."; do
    [[ -z "$dir" || ! -d "$dir" ]] && continue
    if [[ -f "$dir/go.mod" ]]; then
      REPO_ROOT="$(cd "$dir" && pwd)"
      break
    fi
  done
fi
if [[ ! -f "$REPO_ROOT/go.mod" ]]; then
  echo "ERROR: Cannot find repo root (no go.mod). Checked: $REPO_ROOT and fallbacks." >&2
  ls -la "$REPO_ROOT" 2>/dev/null || true
  exit 1
fi
cd "$REPO_ROOT"
echo "=== Working in repo root: $REPO_ROOT ==="

echo "=== Building helm-release-pruner binary (Dockerfile expects pre-built binary) ==="
if command -v go >/dev/null 2>&1; then
  go build -o helm-release-pruner ./cmd/pruner
else
  # Use absolute path for mount so the container sees the same repo root
  docker run --rm -v "${REPO_ROOT}:/app" -w /app golang:1.25-alpine go build -o helm-release-pruner ./cmd/pruner
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
docker cp "$REPO_ROOT" e2e-command-runner:/helm-release-pruner

echo "=== Pre-e2e setup complete ==="
