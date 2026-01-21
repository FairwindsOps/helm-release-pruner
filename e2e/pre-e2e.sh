#!/bin/bash
# Pre-e2e script: Build the pruner image, load into kind, and copy workspace
# This runs AFTER the kind cluster is created, on the host machine
set -o errexit
set -o nounset
set -o pipefail

echo "=== Building helm-release-pruner image ==="
docker build -t helm-release-pruner:test .

echo "=== Loading image into kind cluster ==="
# The rok8s orb creates the cluster with name "e2e"
kind load docker-image helm-release-pruner:test --name e2e

echo "=== Copying workspace to command runner ==="
docker cp "$(pwd)" e2e-command-runner:/helm-release-pruner

echo "=== Pre-e2e setup complete ==="
