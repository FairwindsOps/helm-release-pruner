#!/bin/bash
# Pre-e2e script: Build and load the pruner image into kind
# This runs BEFORE the kind cluster is created, on the host machine
set -o errexit
set -o nounset
set -o pipefail

echo "=== Building helm-release-pruner image ==="
docker build -t helm-release-pruner:test .

echo "=== Loading image into kind cluster ==="
kind load docker-image helm-release-pruner:test

echo "=== Copying test scripts to command runner ==="
docker cp e2e e2e-command-runner:/e2e
docker exec e2e-command-runner chmod +x /e2e/test.sh /e2e/run-e2e.sh

echo "=== Pre-e2e setup complete ==="
