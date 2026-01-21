#!/bin/bash
# Pre-e2e script: Build the pruner image
# This runs BEFORE the kind cluster is created
set -o errexit
set -o nounset
set -o pipefail

echo "=== Building helm-release-pruner image ==="
docker build -t helm-release-pruner:test .

echo "=== Pre-e2e setup complete ==="
