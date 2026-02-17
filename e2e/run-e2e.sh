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

# Run the e2e tests
echo "=== Running E2E tests ==="
chmod +x e2e/test.sh
e2e/test.sh

echo "=== E2E tests complete ==="
