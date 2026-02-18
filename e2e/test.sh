#!/bin/bash
# E2E test script for helm-release-pruner
# This script is designed to run in a kind cluster environment

set -o errexit
set -o nounset
set -o pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Counters
TESTS_PASSED=0
TESTS_FAILED=0

log_info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "${BLUE}[STEP]${NC} $*"; }

# ============================================================================
# Assertion Functions
# ============================================================================

assert_release_exists() {
    local release="$1"
    local ns="$2"
    if helm list -n "$ns" -q | grep -q "^${release}$"; then
        log_info "  ✓ Release '$release' exists in '$ns'"
        return 0
    else
        log_error "  ✗ Release '$release' NOT found in '$ns'"
        return 1
    fi
}

assert_release_not_exists() {
    local release="$1"
    local ns="$2"
    if ! helm list -n "$ns" -q | grep -q "^${release}$"; then
        log_info "  ✓ Release '$release' deleted from '$ns'"
        return 0
    else
        log_error "  ✗ Release '$release' still exists in '$ns'"
        return 1
    fi
}

assert_namespace_exists() {
    local ns="$1"
    if kubectl get namespace "$ns" &>/dev/null; then
        log_info "  ✓ Namespace '$ns' exists"
        return 0
    else
        log_error "  ✗ Namespace '$ns' NOT found"
        return 1
    fi
}

assert_namespace_not_exists() {
    local ns="$1"
    if ! kubectl get namespace "$ns" &>/dev/null; then
        log_info "  ✓ Namespace '$ns' deleted"
        return 0
    else
        log_error "  ✗ Namespace '$ns' still exists"
        return 1
    fi
}

count_releases() {
    helm list -A -q | wc -l | tr -d ' '
}

count_namespaces() {
    kubectl get namespaces -o name | wc -l | tr -d ' '
}

# ============================================================================
# Setup Functions
# ============================================================================

setup_rbac() {
    log_step "Setting up RBAC..."
    kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: helm-release-pruner
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: helm-release-pruner
rules:
  - apiGroups: [""]
    resources: ["secrets", "configmaps"]
    verbs: ["list", "get", "delete"]
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["list", "get", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: helm-release-pruner
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: helm-release-pruner
subjects:
  - kind: ServiceAccount
    name: helm-release-pruner
    namespace: default
EOF
}

create_test_chart() {
    # Create a minimal test chart that installs quickly
    local chart_dir="/tmp/test-chart"
    rm -rf "$chart_dir"
    mkdir -p "$chart_dir/templates"
    
    cat > "$chart_dir/Chart.yaml" <<EOF
apiVersion: v2
name: test-chart
version: 0.1.0
description: Minimal test chart for e2e testing
EOF

    cat > "$chart_dir/templates/configmap.yaml" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-config
  namespace: {{ .Release.Namespace }}
data:
  test: "value"
EOF
    
    echo "$chart_dir"
}

install_release() {
    local name="$1"
    local namespace="$2"
    local chart_dir="${3:-/tmp/test-chart}"
    
    kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1
    helm upgrade --install "$name" "$chart_dir" -n "$namespace" --wait --timeout 60s >/dev/null 2>&1
    log_info "  Installed release '$name' in '$namespace'"
}

run_pruner() {
    local args=("$@")
    log_info "Running pruner: ${args[*]}"
    
    # Run as a Job to ensure clean execution
    # Always add --once to run single cycle and exit
    local job_name="pruner-test-$(date +%s)"
    local all_args=("--once" "${args[@]}")
    
    kubectl apply -f - <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: $job_name
  namespace: default
spec:
  ttlSecondsAfterFinished: 300
  backoffLimit: 0
  template:
    spec:
      serviceAccountName: helm-release-pruner
      restartPolicy: Never
      containers:
      - name: pruner
        image: helm-release-pruner:test
        imagePullPolicy: Never
        args: $(printf '%s\n' "${all_args[@]}" | jq -R . | jq -s .)
EOF
    
    # Wait for job to complete (or failed, so we don't wait full timeout and miss logs before TTL)
    if ! kubectl wait --for=condition=complete --timeout=90s "job/$job_name" -n default 2>/dev/null; then
        kubectl wait --for=condition=failed --timeout=15s "job/$job_name" -n default 2>/dev/null || true
        log_error "Pruner job failed. Logs:"
        kubectl logs "job/$job_name" -n default 2>/dev/null || kubectl logs -l job-name="$job_name" -n default --tail=200 2>/dev/null || true
        kubectl describe job "$job_name" -n default 2>/dev/null || true
        kubectl delete job "$job_name" -n default --ignore-not-found >/dev/null 2>&1
        return 1
    fi
    
    kubectl logs "job/$job_name" -n default
    kubectl delete job "$job_name" -n default --ignore-not-found >/dev/null 2>&1
}

show_state() {
    log_info "Current state:"
    echo "  Namespaces: $(kubectl get namespaces -o name | grep -v '^namespace/kube-' | grep -v '^namespace/default$' | sort | tr '\n' ' ')"
    echo "  Releases:"
    helm list -A --no-headers | awk '{print "    " $1 " (" $2 ")"}' || echo "    (none)"
}

# ============================================================================
# Test Cases
# ============================================================================

test_dry_run() {
    log_step "TEST: Dry Run Mode"
    log_info "Verifies --dry-run doesn't delete anything"
    
    local count_before
    count_before=$(count_releases)
    
    run_pruner --older-than=1s --namespace-filter="^test-" --dry-run
    
    local count_after
    count_after=$(count_releases)
    
    if [[ "$count_before" == "$count_after" ]]; then
        log_info "✓ TEST PASSED: No releases deleted in dry-run mode"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: Releases were deleted in dry-run mode ($count_before -> $count_after)"
        ((++TESTS_FAILED))
    fi
}

test_older_than_filter() {
    log_step "TEST: --older-than Filter"
    log_info "Verifies releases older than threshold are deleted"
    
    # All test releases are "old" (just created, but threshold is 1s in the past)
    # Wait a moment to ensure releases are "old"
    sleep 2
    
    run_pruner --older-than=1s --namespace-filter="^test-ns-old"
    
    if assert_release_not_exists "old-release-1" "test-ns-old" && \
       assert_release_exists "keep-release" "test-ns-keep"; then
        log_info "✓ TEST PASSED: --older-than filter works correctly"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: --older-than filter did not work as expected"
        ((++TESTS_FAILED))
    fi
}

test_namespace_filter() {
    log_step "TEST: --namespace-filter"
    log_info "Verifies only matching namespaces are affected"
    
    sleep 2
    run_pruner --older-than=1s --namespace-filter="^test-feature-"
    
    if assert_release_not_exists "feature-app" "test-feature-branch" && \
       assert_release_exists "staging-app" "test-staging"; then
        log_info "✓ TEST PASSED: --namespace-filter works correctly"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: --namespace-filter did not work as expected"
        ((++TESTS_FAILED))
    fi
}

test_release_exclude() {
    log_step "TEST: --release-exclude"
    log_info "Verifies matching releases are excluded from deletion"
    
    sleep 2
    run_pruner --older-than=1s --namespace-filter="^test-exclude-" --release-exclude="-permanent$"
    
    if assert_release_not_exists "delete-me" "test-exclude-ns" && \
       assert_release_exists "keep-permanent" "test-exclude-ns"; then
        log_info "✓ TEST PASSED: --release-exclude works correctly"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: --release-exclude did not work as expected"
        ((++TESTS_FAILED))
    fi
}

test_orphan_namespace_cleanup() {
    log_step "TEST: --cleanup-orphan-namespaces"
    log_info "Verifies orphan namespaces (no helm releases) are deleted"
    
    # Create orphan namespaces (no helm releases)
    kubectl create namespace test-orphan-1 --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1
    kubectl create namespace test-orphan-2 --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1
    
    assert_namespace_exists "test-orphan-1"
    assert_namespace_exists "test-orphan-2"
    
    run_pruner --cleanup-orphan-namespaces --orphan-namespace-filter="^test-orphan-"
    
    # Give k8s time to delete namespaces
    sleep 5
    
    if assert_namespace_not_exists "test-orphan-1" && \
       assert_namespace_not_exists "test-orphan-2"; then
        log_info "✓ TEST PASSED: --cleanup-orphan-namespaces works correctly"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: --cleanup-orphan-namespaces did not work as expected"
        ((++TESTS_FAILED))
    fi
}

test_orphan_requires_filter() {
    log_step "TEST: Orphan cleanup requires filter"
    log_info "Verifies --cleanup-orphan-namespaces without filter is rejected"
    
    # This should warn and skip orphan cleanup
    local output
    output=$(run_pruner --cleanup-orphan-namespaces --older-than=1h --namespace-filter="^nonexistent-" 2>&1) || true
    
    if echo "$output" | grep -qi "requires.*filter\|disabled"; then
        log_info "✓ TEST PASSED: Correctly requires --orphan-namespace-filter"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: Should require --orphan-namespace-filter"
        ((++TESTS_FAILED))
    fi
}

test_system_namespace_protection() {
    log_step "TEST: System namespace protection"
    log_info "Verifies kube-system and default are never deleted"
    
    # Try to delete with broad filter - system namespaces should be protected
    run_pruner --cleanup-orphan-namespaces --orphan-namespace-filter=".*" --orphan-namespace-exclude="^test-" --dry-run
    
    if assert_namespace_exists "kube-system" && \
       assert_namespace_exists "default"; then
        log_info "✓ TEST PASSED: System namespaces protected"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: System namespaces not protected!"
        ((++TESTS_FAILED))
    fi
}

test_preserve_namespace() {
    log_step "TEST: --preserve-namespace"
    log_info "Verifies namespaces are kept after release deletion when flag is set"
    
    # Install a release, then delete it with --preserve-namespace
    install_release "preserve-test" "test-preserve-ns"
    
    sleep 2
    run_pruner --older-than=1s --namespace-filter="^test-preserve-" --preserve-namespace
    
    if assert_release_not_exists "preserve-test" "test-preserve-ns" && \
       assert_namespace_exists "test-preserve-ns"; then
        log_info "✓ TEST PASSED: --preserve-namespace works correctly"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: --preserve-namespace did not work as expected"
        ((++TESTS_FAILED))
    fi
}

test_max_releases_to_keep() {
    log_step "TEST: --max-releases-to-keep"
    log_info "Verifies only N newest releases are kept"
    
    # Install multiple releases
    install_release "max-test-1" "test-max-ns"
    sleep 1
    install_release "max-test-2" "test-max-ns"
    sleep 1
    install_release "max-test-3" "test-max-ns"
    
    # Keep only 1 newest
    run_pruner --max-releases-to-keep=1 --namespace-filter="^test-max-"
    
    # max-test-3 should be kept (newest), others deleted
    if assert_release_exists "max-test-3" "test-max-ns" && \
       assert_release_not_exists "max-test-1" "test-max-ns" && \
       assert_release_not_exists "max-test-2" "test-max-ns"; then
        log_info "✓ TEST PASSED: --max-releases-to-keep works correctly"
        ((++TESTS_PASSED))
    else
        log_error "✗ TEST FAILED: --max-releases-to-keep did not work as expected"
        ((++TESTS_FAILED))
    fi
}

# ============================================================================
# Main
# ============================================================================

main() {
    log_info "=============================================="
    log_info "  helm-release-pruner E2E Tests"
    log_info "=============================================="
    echo ""
    
    # Setup
    setup_rbac
    local chart_dir
    chart_dir=$(create_test_chart)
    
    log_step "Installing test releases..."
    
    # Releases for --older-than test
    install_release "old-release-1" "test-ns-old" "$chart_dir"
    install_release "keep-release" "test-ns-keep" "$chart_dir"
    
    # Releases for --namespace-filter test
    install_release "feature-app" "test-feature-branch" "$chart_dir"
    install_release "staging-app" "test-staging" "$chart_dir"
    
    # Releases for --release-exclude test
    install_release "delete-me" "test-exclude-ns" "$chart_dir"
    install_release "keep-permanent" "test-exclude-ns" "$chart_dir"
    
    echo ""
    show_state
    echo ""
    
    # Run tests
    test_dry_run
    echo ""
    test_system_namespace_protection
    echo ""
    test_orphan_requires_filter
    echo ""
    test_older_than_filter
    echo ""
    test_namespace_filter
    echo ""
    test_release_exclude
    echo ""
    test_preserve_namespace
    echo ""
    test_max_releases_to_keep
    echo ""
    test_orphan_namespace_cleanup
    echo ""
    
    # Summary
    log_info "=============================================="
    log_info "  Test Results"
    log_info "=============================================="
    log_info "  Passed: $TESTS_PASSED"
    log_info "  Failed: $TESTS_FAILED"
    log_info "=============================================="
    
    if [[ $TESTS_FAILED -gt 0 ]]; then
        log_error "Some tests failed!"
        exit 1
    fi
    
    log_info "All tests passed!"
}

main "$@"
