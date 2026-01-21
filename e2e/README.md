# End-to-End Tests

This directory contains automated E2E tests for helm-release-pruner using kind (Kubernetes in Docker).

## Test Coverage

| Test | What it verifies |
|------|------------------|
| **Dry Run** | `--dry-run` doesn't delete anything |
| **System NS Protection** | `kube-system`, `default` are never deleted |
| **Orphan Requires Filter** | `--cleanup-orphan-namespaces` requires `--orphan-namespace-filter` |
| **Older Than** | `--older-than` deletes releases older than threshold |
| **Namespace Filter** | `--namespace-filter` only affects matching namespaces |
| **Release Exclude** | `--release-exclude` skips matching releases |
| **Preserve Namespace** | `--preserve-namespace` keeps NS after release deletion |
| **Max Releases** | `--max-releases-to-keep` keeps only N newest releases |
| **Orphan Cleanup** | `--cleanup-orphan-namespaces` deletes empty namespaces |

## CI Integration

Tests run automatically in CircleCI on every PR using the `rok8s/kubernetes_e2e_tests` orb:

1. **pre-e2e.sh** - Builds Docker image and loads it into kind
2. **run-e2e.sh** - Runs the test suite inside the cluster
3. **test.sh** - The actual test cases with assertions

Tests run against multiple Kubernetes versions (1.32, 1.33).

## Running Locally

### Prerequisites
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [helm](https://helm.sh/docs/intro/install/)
- [docker](https://docs.docker.com/get-docker/)

### Quick Start

```bash
# Create kind cluster
kind create cluster --name pruner-test

# Build and load image
docker build -t helm-release-pruner:test .
kind load docker-image helm-release-pruner:test --name pruner-test

# Run tests
./e2e/test.sh

# Cleanup
kind delete cluster --name pruner-test
```

### Test Output

```
[INFO] ==============================================
[INFO]   helm-release-pruner E2E Tests
[INFO] ==============================================

[STEP] Setting up RBAC...
[STEP] Installing test releases...
[INFO]   Installed release 'old-release-1' in 'test-ns-old'
...

[STEP] TEST: Dry Run Mode
[INFO] Verifies --dry-run doesn't delete anything
[INFO] Running pruner: --older-than=1s --namespace-filter=^test- --dry-run
[INFO]   ✓ No releases deleted in dry-run mode
[INFO] ✓ TEST PASSED: No releases deleted in dry-run mode

...

[INFO] ==============================================
[INFO]   Test Results
[INFO] ==============================================
[INFO]   Passed: 9
[INFO]   Failed: 0
[INFO] ==============================================
[INFO] All tests passed!
```

## Adding New Tests

1. Add a new test function in `test.sh`:

```bash
test_my_new_feature() {
    log_step "TEST: My New Feature"
    log_info "Verifies something important"
    
    # Setup
    install_release "test-release" "test-ns"
    
    # Run pruner
    run_pruner --my-new-flag --namespace-filter="^test-"
    
    # Assert results
    if assert_release_not_exists "test-release" "test-ns"; then
        log_info "✓ TEST PASSED: My new feature works"
        ((TESTS_PASSED++))
    else
        log_error "✗ TEST FAILED: My new feature broken"
        ((TESTS_FAILED++))
    fi
}
```

2. Call the test in `main()`:

```bash
test_my_new_feature
echo ""
```
