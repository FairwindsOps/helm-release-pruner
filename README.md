# helm-release-pruner

A Go-based daemon for automatically deleting old Helm releases. Runs continuously in your cluster and prunes stale releases at configurable intervals.

## Features

- **Daemon mode** — Runs continuously with configurable prune intervals
- **Native Helm SDK** — Uses Helm Go SDK directly (no CLI shelling)
- **Flexible filtering** — Filter by release name, namespace, age, or count
- **Regex support** — Include/exclude releases and namespaces using regex patterns
- **Namespace cleanup** — Optionally delete empty namespaces after pruning
- **Health endpoints** — Built-in `/healthz`, `/readyz`, and `/metrics` for Kubernetes probes
- **Prometheus metrics** — Exposes metrics for monitoring prune operations
- **Graceful shutdown** — Handles SIGTERM/SIGINT for clean pod termination
- **Rate limiting** — Configurable rate limiting to avoid overwhelming the API server
- **Dry-run mode** — Preview what would be deleted before making changes
- **Minimal image** — Distroless container (~10MB) with no shell

## Installation

### Using Docker

```bash
docker pull quay.io/fairwinds/helm-release-pruner:latest
```

### Building from source

```bash
go build -o helm-release-pruner ./cmd/pruner
```

## Usage

```bash
helm-release-pruner [flags]
```

The pruner runs as a daemon by default, executing prune cycles at the configured interval.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--interval` | `1h` | How often to run the pruning cycle |
| `--max-releases-to-keep` | `0` | Keep only the N most recent releases globally (0 = no limit) |
| `--older-than` | | Delete releases older than this duration |
| `--release-filter` | | Regex to include matching release names |
| `--namespace-filter` | | Regex to include matching namespaces |
| `--release-exclude` | | Regex to exclude matching release names |
| `--namespace-exclude` | | Regex to exclude matching namespaces |
| `--preserve-namespace` | `false` | Don't delete empty namespaces |
| `--cleanup-orphan-namespaces` | `false` | Delete namespaces with no Helm releases (requires `--orphan-namespace-filter`) |
| `--orphan-namespace-filter` | | Regex filter for orphan namespace cleanup (required with `--cleanup-orphan-namespaces`) |
| `--orphan-namespace-exclude` | | Regex to exclude namespaces from orphan cleanup |
| `--system-namespaces` | | Comma-separated additional namespaces to never delete |
| `--delete-rate-limit` | `100ms` | Minimum duration between delete operations (0 to disable) |
| `--dry-run` | `false` | Show what would be deleted |
| `--debug` | `false` | Enable debug logging |
| `--health-addr` | `:8080` | Address for health check and metrics endpoints |

### Duration formats

The `--older-than` and `--interval` flags support:
- Standard Go durations: `1h`, `336h`, `720h30m`
- Days: `14d` (14 days)
- Weeks: `2w` (2 weeks)

### Examples

Run daemon that prunes releases older than 2 weeks, checking every hour:

```bash
helm-release-pruner --older-than=2w --interval=1h
```

Prune feature branch releases every 30 minutes:

```bash
helm-release-pruner \
  --interval=30m \
  --older-than=1w \
  --release-filter="^feature-.+-web$" \
  --namespace-filter="^feature-.+"
```

Keep only the 5 most recent releases globally (after filtering), excluding permanent releases:

```bash
helm-release-pruner \
  --interval=6h \
  --max-releases-to-keep=5 \
  --release-exclude="-permanent$"
```

Dry run to preview deletions:

```bash
helm-release-pruner --older-than=30d --dry-run
```

Add custom system namespaces that should never be deleted:

```bash
helm-release-pruner \
  --older-than=2w \
  --system-namespaces="monitoring,logging,istio-system"
```

## Health Endpoints

The daemon exposes health and metrics endpoints for Kubernetes probes and monitoring:

| Endpoint | Description |
|----------|-------------|
| `/healthz` | Liveness probe - returns 200 if process is running |
| `/readyz` | Readiness probe - returns 200 after initialization and if cluster is reachable |
| `/metrics` | Prometheus metrics endpoint |

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `helm_pruner_releases_deleted_total` | Counter | Total number of Helm releases deleted |
| `helm_pruner_namespaces_deleted_total` | Counter | Total number of namespaces deleted |
| `helm_pruner_cycle_duration_seconds` | Histogram | Duration of prune cycles in seconds |
| `helm_pruner_cycle_failures_total` | Counter | Total number of failed prune cycles |
| `helm_pruner_releases_scanned_total` | Counter | Total number of releases scanned across all cycles |

## Kubernetes Deployment

Deploy as a Deployment (not CronJob) since it runs as a daemon:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: helm-release-pruner
spec:
  replicas: 1
  selector:
    matchLabels:
      app: helm-release-pruner
  template:
    metadata:
      labels:
        app: helm-release-pruner
    spec:
      serviceAccountName: helm-release-pruner
      containers:
        - name: pruner
          image: quay.io/fairwinds/helm-release-pruner:latest
          args:
            - --interval=1h
            - --older-than=2w
          ports:
            - name: health
              containerPort: 8080
          livenessProbe:
            httpGet:
              path: /healthz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: health
            initialDelaySeconds: 30
            periodSeconds: 10
            failureThreshold: 3
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              memory: 128Mi
```

### Required RBAC

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: helm-release-pruner
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: helm-release-pruner
rules:
  # List and delete Helm releases (stored as secrets by default)
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["list", "get", "delete"]
  # Optional: delete empty namespaces
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
```

### ServiceMonitor (for Prometheus Operator)

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: helm-release-pruner
spec:
  selector:
    matchLabels:
      app: helm-release-pruner
  endpoints:
    - port: health
      path: /metrics
      interval: 30s
```

## Migration from v3.x (Bash)

The Go version maintains CLI compatibility where possible:

| Old Flag (Bash) | New Flag (Go) | Notes |
|-----------------|---------------|-------|
| `--helm-release-filter` | `--release-filter` | Same regex behavior |
| `--helm-release-negate-filter` | `--release-exclude` | Same regex behavior |
| `--namespace-negate-filter` | `--namespace-exclude` | Same regex behavior |
| `--older-than="4 weeks ago"` | `--older-than=4w` | Uses Go duration format |
| `--preserve-namespace` | `--preserve-namespace` | **Bug fix:** Now works correctly (was broken in bash) |
| `--max-releases-to-keep` | `--max-releases-to-keep` | Same global behavior |
| N/A | `--debug` | New flag for verbose logging |

**Key changes:**

- **Daemon mode**: Now runs continuously instead of one-shot. Use `--interval` to control how often the pruning cycle runs (default: 1h).
- **Duration format**: Uses Go duration format (`2w`, `14d`, `336h`) instead of GNU date strings (`"2 weeks ago"`).
- **Health endpoints**: Exposes `/healthz`, `/readyz`, and `/metrics` on `:8080` by default.
- **Prometheus metrics**: Exposes metrics for monitoring prune operations.
- **Rate limiting**: Default 100ms between delete operations to avoid overwhelming the API server.
- **System namespace protection**: System namespaces (`kube-system`, `kube-public`, `default`, `kube-node-lease`) are never deleted, even if they match filters. Add more with `--system-namespaces`.
- **Graceful shutdown**: Handles SIGTERM/SIGINT properly for clean pod termination.
- **Bug fix**: `--preserve-namespace` now works correctly. The bash version had a bug where the flag was parsed but the variable was never used.

**Behavioral clarification:**

The `--max-releases-to-keep` flag applies **globally across all filtered releases**, not per release name. For example, with `--max-releases-to-keep=5` and `--release-filter="^feature-"`:
- The pruner finds all releases matching `^feature-`
- Sorts them by deployment time (newest first)
- Keeps the 5 most recently deployed releases
- Deletes all others

This is the same behavior as the original bash script. If you need to keep N releases per application, use separate pruner instances with specific filters.

## Development

### Prerequisites

- Go 1.21+
- Access to a Kubernetes cluster (for testing)

### Building

```bash
# Build binary
go build -o helm-release-pruner ./cmd/pruner

# Build Docker image
docker build -t helm-release-pruner:dev .

# Run tests
go test ./...
```

### Running locally

```bash
# Uses your local kubeconfig
./helm-release-pruner --dry-run --older-than=1w --interval=5m --debug
```

<!-- Begin boilerplate -->
## Join the Fairwinds Open Source Community

The goal of the Fairwinds Community is to exchange ideas, influence the open source roadmap,
and network with fellow Kubernetes users.
[Chat with us on Slack](https://join.slack.com/t/fairwindscommunity/shared_invite/zt-2na8gtwb4-DGQ4qgmQbczQhtxqY~u_8Q)
or
[join the user group](https://www.fairwinds.com/open-source-software-user-group) to get involved!

<a href="https://insights.fairwinds.com/auth/register/">
  <img src="https://www.fairwinds.com/hubfs/Doc_Banners/Fairwinds_Insights_background.png" alt="Fairwinds Insights" />
</a>

## Other Projects from Fairwinds

Enjoying helm-release-pruner? Check out some of our other projects:
* [Polaris](https://github.com/FairwindsOps/Polaris) - Audit, enforce, and build policies for Kubernetes resources, including over 20 built-in checks for best practices
* [Goldilocks](https://github.com/FairwindsOps/Goldilocks) - Right-size your Kubernetes Deployments by comparing your memory and CPU settings against actual usage
* [Pluto](https://github.com/FairwindsOps/Pluto) - Detect Kubernetes resources that have been deprecated or removed in future versions
* [Nova](https://github.com/FairwindsOps/Nova) - Check to see if any of your Helm charts have updates available
* [rbac-manager](https://github.com/FairwindsOps/rbac-manager) - Simplify the management of RBAC in your Kubernetes clusters

Or [check out the full list](https://www.fairwinds.com/open-source-software?utm_source=helm-release-pruner&utm_medium=helm-release-pruner&utm_campaign=helm-release-pruner)
<!-- End boilerplate -->
