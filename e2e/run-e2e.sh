#!/bin/bash
# E2E test runner script for CircleCI
# This runs inside the command runner container with access to the kind cluster
set -o errexit
set -o nounset
set -o pipefail

echo "=== helm-release-pruner E2E Tests ==="
echo "Helm version: $(helm version --short)"
echo "Kubectl version: $(kubectl version --client --short 2>/dev/null || kubectl version --client)"
echo "Cluster info:"
kubectl cluster-info
echo ""

# Change to the workspace directory (copied by pre-e2e.sh)
cd /helm-release-pruner

# Load the pre-built image into kind cluster
echo "=== Loading helm-release-pruner image into kind cluster ==="
if command -v kind &> /dev/null; then
    CLUSTER_NAME="${KIND_CLUSTER:-e2e}"
    kind load docker-image helm-release-pruner:test --name "$CLUSTER_NAME" || {
        echo "Warning: Could not load image with kind"
    }
else
    echo "kind not found, assuming image is already available in cluster"
fi

# Run the e2e tests
echo "=== Running E2E tests ==="
chmod +x e2e/test.sh
e2e/test.sh

echo "=== E2E tests complete ==="
