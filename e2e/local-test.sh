#!/bin/bash
# Run e2e tests locally with kind
# Usage: ./e2e/local-test.sh [--keep-cluster]

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CLUSTER_NAME="pruner-e2e-test"
IMAGE_NAME="helm-release-pruner:test"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }

# Parse args
KEEP_CLUSTER=false
for arg in "$@"; do
    case $arg in
        --keep-cluster) KEEP_CLUSTER=true ;;
    esac
done

cleanup() {
    if [[ "$KEEP_CLUSTER" == "true" ]]; then
        log_warn "Keeping cluster '$CLUSTER_NAME' for debugging"
        log_warn "Delete it later with: kind delete cluster --name $CLUSTER_NAME"
    else
        log_info "Cleaning up kind cluster '$CLUSTER_NAME'..."
        kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Check prerequisites
for cmd in kind kubectl helm docker; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: '$cmd' is required but not installed."
        exit 1
    fi
done

log_info "=============================================="
log_info "  Running E2E Tests Locally"
log_info "=============================================="
echo ""

# Step 1: Create kind cluster
log_info "Step 1: Creating kind cluster '$CLUSTER_NAME'..."
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    log_warn "Cluster already exists, deleting..."
    kind delete cluster --name "$CLUSTER_NAME"
fi
kind create cluster --name "$CLUSTER_NAME" --wait 120s

# Step 2: Build Docker image
log_info "Step 2: Building Docker image..."
cd "$PROJECT_ROOT"
docker build -t "$IMAGE_NAME" .

# Step 3: Load image into kind
log_info "Step 3: Loading image into kind cluster..."
kind load docker-image "$IMAGE_NAME" --name "$CLUSTER_NAME"

# Step 4: Run tests
log_info "Step 4: Running e2e tests..."
echo ""
"$SCRIPT_DIR/test.sh"

if [[ "$KEEP_CLUSTER" == "true" ]]; then
    log_info "Deleting test namespaces..."
    while read -r ns; do
        [[ -z "$ns" ]] && continue
        kubectl delete namespace "$ns" --ignore-not-found=true --wait --timeout=60s 2>/dev/null || true
    done < <(kubectl get namespaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null | tr ' ' '\n' | grep '^test-' || true)
fi

echo ""
log_info "=============================================="
log_info "  Local E2E Tests Complete!"
log_info "=============================================="
