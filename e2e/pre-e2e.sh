#!/bin/bash
# Pre-e2e script: Build the pruner image and copy workspace to command runner
# This runs BEFORE the kind cluster tests, on the host machine
set -o errexit
set -o nounset
set -o pipefail

echo "=== Building helm-release-pruner image ==="
docker build -t helm-release-pruner:test .

echo "=== Copying workspace to command runner ==="
docker cp "$(pwd)" e2e-command-runner:/helm-release-pruner

echo "=== Pre-e2e setup complete ==="
