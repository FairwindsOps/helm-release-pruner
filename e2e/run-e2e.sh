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

# Load the pre-built image into kind cluster
echo "=== Loading helm-release-pruner image into kind cluster ==="
if command -v kind &> /dev/null; then
    # Get the kind cluster name (usually "e2e" in the orb)
    CLUSTER_NAME="${KIND_CLUSTER:-e2e}"
    kind load docker-image helm-release-pruner:test --name "$CLUSTER_NAME" || {
        echo "Warning: Could not load image with kind, trying direct approach..."
        # Image might already be available if using same docker daemon
    }
else
    echo "kind not found, assuming image is already available in cluster"
fi

# Run the e2e tests
# Check if test.sh is at /e2e or in the current directory
if [[ -x /e2e/test.sh ]]; then
    /e2e/test.sh
elif [[ -x ./e2e/test.sh ]]; then
    ./e2e/test.sh
else
    echo "ERROR: test.sh not found"
    exit 1
fi

echo "=== E2E tests complete ==="
