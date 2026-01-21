package pruner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Prometheus metrics
var (
	releasesDeletedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "helm_pruner_releases_deleted_total",
		Help: "Total number of Helm releases deleted",
	})
	namespacesDeletedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "helm_pruner_namespaces_deleted_total",
		Help: "Total number of namespaces deleted",
	})
	pruneCycleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "helm_pruner_cycle_duration_seconds",
		Help:    "Duration of prune cycles in seconds",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
	})
	pruneCycleFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "helm_pruner_cycle_failures_total",
		Help: "Total number of failed prune cycles",
	})
	releasesScannedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "helm_pruner_releases_scanned_total",
		Help: "Total number of releases scanned across all cycles",
	})
)

// defaultSystemNamespaces are namespaces that should never be deleted.
var defaultSystemNamespaces = []string{
	"default",
	"kube-system",
	"kube-public",
	"kube-node-lease",
}

// Pruner handles the deletion of old Helm releases.
type Pruner struct {
	opts             Options
	settings         *cli.EnvSettings
	k8s              kubernetes.Interface
	logger           *slog.Logger
	systemNamespaces map[string]bool

	// ready indicates whether the pruner has successfully completed at least one cycle.
	ready atomic.Bool
	// initialized indicates whether the pruner has attempted at least one cycle.
	initialized atomic.Bool
	// consecutiveFailures tracks repeated failures for backoff.
	consecutiveFailures int
	// mu protects consecutiveFailures
	mu sync.Mutex
}

// New creates a new Pruner instance.
func New(opts Options) (*Pruner, error) {
	settings := cli.New()

	// Set up logger
	logLevel := slog.LevelInfo
	if opts.Debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Initialize Kubernetes client
	k8sClient, err := newKubernetesClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Build system namespaces map
	systemNS := make(map[string]bool)
	for _, ns := range defaultSystemNamespaces {
		systemNS[ns] = true
	}
	// Add any additional system namespaces from options
	for _, ns := range opts.AdditionalSystemNamespaces {
		systemNS[ns] = true
	}

	return &Pruner{
		opts:             opts,
		settings:         settings,
		k8s:              k8sClient,
		logger:           logger,
		systemNamespaces: systemNS,
	}, nil
}

// Ready returns true if the pruner has successfully completed at least one cycle.
func (p *Pruner) Ready() bool {
	return p.ready.Load()
}

// Initialized returns true if the pruner has attempted at least one cycle.
// This is useful for readiness probes that want to pass after initialization
// even if the first cycle failed.
func (p *Pruner) Initialized() bool {
	return p.initialized.Load()
}

// CheckConnectivity verifies that we can connect to the Kubernetes cluster.
func (p *Pruner) CheckConnectivity(ctx context.Context) error {
	_, err := p.k8s.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

// RunOnce executes a single pruning cycle.
func (p *Pruner) RunOnce(ctx context.Context) error {
	if p.opts.DryRun {
		p.logger.Info("running in dry-run mode - nothing will be deleted")
	}

	// Phase 1: Prune old Helm releases (if filters are configured)
	if p.hasReleasePruningFilters() {
		if err := p.pruneReleases(ctx); err != nil {
			return fmt.Errorf("release pruning failed: %w", err)
		}
	}

	// Phase 2: Clean up orphaned namespaces (if enabled)
	if p.opts.CleanupOrphanNamespaces {
		if err := p.cleanupOrphanNamespaces(ctx); err != nil {
			return fmt.Errorf("orphan namespace cleanup failed: %w", err)
		}
	}

	return nil
}

// hasReleasePruningFilters returns true if any release pruning filters are configured.
func (p *Pruner) hasReleasePruningFilters() bool {
	return p.opts.OlderThan > 0 ||
		p.opts.MaxReleasesToKeep > 0 ||
		p.opts.ReleaseFilter != nil ||
		p.opts.NamespaceFilter != nil ||
		p.opts.ReleaseExclude != nil ||
		p.opts.NamespaceExclude != nil
}

// pruneReleases handles the release pruning logic.
func (p *Pruner) pruneReleases(ctx context.Context) error {
	// List all releases across all namespaces
	releases, err := p.listAllReleases(ctx)
	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	p.logger.Info("found releases", "count", len(releases))
	releasesScannedTotal.Add(float64(len(releases)))

	// Filter releases based on options
	candidates := p.filterReleases(releases)
	p.logger.Debug("releases after filtering", "count", len(candidates))

	// Apply age and count filters to determine which releases to delete
	toDelete := p.selectReleasesToDelete(candidates)

	if len(toDelete) == 0 {
		p.logger.Info("no stale Helm releases found")
		return nil
	}

	p.logger.Info("releases to delete", "count", len(toDelete))

	// Track namespaces that might become empty
	affectedNamespaces := make(map[string]bool)

	// Delete releases with rate limiting
	for i, rel := range toDelete {
		// Check for context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		affectedNamespaces[rel.Namespace] = true

		if p.opts.DryRun {
			p.logger.Info("would delete release",
				"name", rel.Name,
				"namespace", rel.Namespace,
				"last_deployed", rel.Info.LastDeployed,
				"status", rel.Info.Status)
		} else {
			p.logger.Info("deleting release",
				"name", rel.Name,
				"namespace", rel.Namespace)

			if err := p.deleteRelease(ctx, rel.Name, rel.Namespace); err != nil {
				p.logger.Error("failed to delete release",
					"name", rel.Name,
					"namespace", rel.Namespace,
					"error", err)
				continue
			}
			releasesDeletedTotal.Inc()

			// Apply rate limiting between deletions (not after the last one)
			if p.opts.DeleteRateLimit > 0 && i < len(toDelete)-1 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(p.opts.DeleteRateLimit):
				}
			}
		}
	}

	// Clean up empty namespaces if not preserving them
	if !p.opts.PreserveNamespace {
		for ns := range affectedNamespaces {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := p.deleteNamespaceIfEmpty(ctx, ns); err != nil {
				p.logger.Error("failed to check/delete namespace",
					"namespace", ns,
					"error", err)
			}
		}
	}

	return nil
}

// cleanupOrphanNamespaces finds and deletes namespaces that have no Helm releases.
func (p *Pruner) cleanupOrphanNamespaces(ctx context.Context) error {
	p.logger.Info("starting orphan namespace cleanup")

	// List all namespaces
	namespaces, err := p.k8s.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	p.logger.Debug("found namespaces", "count", len(namespaces.Items))

	// First pass: identify orphan namespaces to delete
	var orphanNamespaces []string
	for _, ns := range namespaces.Items {
		// Check for context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		nsName := ns.Name

		// Never delete system namespaces regardless of filters
		if p.systemNamespaces[nsName] {
			p.logger.Debug("skipping system namespace",
				"namespace", nsName)
			continue
		}

		// Check if namespace matches the inclusion filter
		if p.opts.OrphanNamespaceFilter != nil {
			if !p.opts.OrphanNamespaceFilter.MatchString(nsName) {
				p.logger.Debug("skipping namespace (doesn't match orphan filter)",
					"namespace", nsName)
				continue
			}
		}

		// Check if namespace matches the exclusion filter
		if p.opts.OrphanNamespaceExclude != nil {
			if p.opts.OrphanNamespaceExclude.MatchString(nsName) {
				p.logger.Debug("skipping namespace (matches orphan exclude)",
					"namespace", nsName)
				continue
			}
		}

		// Check if namespace has any Helm releases
		hasReleases, err := p.namespaceHasReleases(ctx, nsName)
		if err != nil {
			p.logger.Error("failed to check releases in namespace",
				"namespace", nsName,
				"error", err)
			continue
		}

		if hasReleases {
			p.logger.Debug("namespace has releases, not orphaned",
				"namespace", nsName)
			continue
		}

		// Namespace is orphaned (no Helm releases)
		orphanNamespaces = append(orphanNamespaces, nsName)
	}

	if len(orphanNamespaces) == 0 {
		p.logger.Info("no orphan namespaces found")
		return nil
	}

	p.logger.Info("orphan namespaces to delete", "count", len(orphanNamespaces))

	// Second pass: delete orphan namespaces with rate limiting
	for i, nsName := range orphanNamespaces {
		// Check for context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if p.opts.DryRun {
			p.logger.Info("would delete orphan namespace",
				"namespace", nsName)
		} else {
			p.logger.Info("deleting orphan namespace",
				"namespace", nsName)

			if err := p.k8s.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{}); err != nil {
				p.logger.Error("failed to delete orphan namespace",
					"namespace", nsName,
					"error", err)
				continue
			}
			namespacesDeletedTotal.Inc()

			// Apply rate limiting between deletions (not after the last one)
			if p.opts.DeleteRateLimit > 0 && i < len(orphanNamespaces)-1 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(p.opts.DeleteRateLimit):
				}
			}
		}
	}

	p.logger.Info("orphan namespace cleanup complete", "count", len(orphanNamespaces))
	return nil
}

// namespaceHasReleases checks if a namespace contains any Helm releases.
func (p *Pruner) namespaceHasReleases(ctx context.Context, namespace string) (bool, error) {
	// Check for context cancellation before making API calls
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(p.settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER")); err != nil {
		return false, err
	}

	listAction := action.NewList(actionConfig)
	listAction.All = true // Include all statuses
	releases, err := listAction.Run()
	if err != nil {
		return false, err
	}

	return len(releases) > 0, nil
}

// maxBackoff is the maximum backoff duration between failed cycles.
const maxBackoff = 5 * time.Minute

// maxBackoffShift is the maximum bit shift to prevent overflow (2^10 seconds = ~17 minutes)
const maxBackoffShift = 10

// CalculateBackoff computes the exponential backoff duration for a given number
// of consecutive failures. Returns 0 for the first failure (no backoff needed).
// Uses exponential backoff: 2^(failures-1) seconds, capped at maxBackoff.
func CalculateBackoff(consecutiveFailures int) time.Duration {
	if consecutiveFailures <= 1 {
		return 0
	}

	// Cap the shift to prevent overflow
	shift := consecutiveFailures - 1
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}

	backoff := time.Duration(1<<uint(shift)) * time.Second
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	return backoff
}

// RunDaemon runs the pruner as a daemon, executing prune cycles at the configured interval.
func (p *Pruner) RunDaemon(ctx context.Context) error {
	p.logger.Info("starting daemon",
		"interval", p.opts.Interval,
		"dry_run", p.opts.DryRun,
		"cleanup_orphan_namespaces", p.opts.CleanupOrphanNamespaces)

	// Run immediately on startup
	p.runCycleWithBackoff(ctx)

	ticker := time.NewTicker(p.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("shutting down daemon")
			return ctx.Err()
		case <-ticker.C:
			p.runCycleWithBackoff(ctx)
		}
	}
}

// runCycleWithBackoff executes a prune cycle with exponential backoff on repeated failures.
func (p *Pruner) runCycleWithBackoff(ctx context.Context) {
	p.logger.Info("starting prune cycle")

	// Mark as initialized after first attempt
	defer p.initialized.Store(true)

	start := time.Now()
	err := p.RunOnce(ctx)
	duration := time.Since(start)
	pruneCycleDuration.Observe(duration.Seconds())

	if err != nil {
		p.mu.Lock()
		p.consecutiveFailures++
		failures := p.consecutiveFailures
		p.mu.Unlock()

		pruneCycleFailuresTotal.Inc()
		p.logger.Error("prune cycle failed",
			"error", err,
			"duration", duration,
			"consecutive_failures", failures)

		// Apply exponential backoff if we have repeated failures
		if backoff := CalculateBackoff(failures); backoff > 0 {
			p.logger.Warn("applying backoff due to repeated failures",
				"backoff", backoff)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
		return
	}

	// Success - reset failure count and mark as ready
	p.mu.Lock()
	p.consecutiveFailures = 0
	p.mu.Unlock()

	p.ready.Store(true)
	p.logger.Info("prune cycle complete",
		"duration", duration,
		"next_run", time.Now().Add(p.opts.Interval))
}

// listAllReleases returns all Helm releases across all namespaces.
func (p *Pruner) listAllReleases(ctx context.Context) ([]*releasev1.Release, error) {
	// Check for context cancellation before making API calls
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	actionConfig := new(action.Configuration)

	// Initialize for all namespaces (empty string = all namespaces)
	if err := actionConfig.Init(p.settings.RESTClientGetter(), "", os.Getenv("HELM_DRIVER")); err != nil {
		return nil, err
	}

	listAction := action.NewList(actionConfig)
	listAction.AllNamespaces = true
	listAction.All = true // Include all statuses (deployed, failed, etc.)

	releaseList, err := listAction.Run()
	if err != nil {
		return nil, err
	}

	// Convert Releaser interface to concrete Release type
	releases := make([]*releasev1.Release, 0, len(releaseList))
	for _, r := range releaseList {
		if rel, ok := r.(*releasev1.Release); ok {
			releases = append(releases, rel)
		}
	}

	return releases, nil
}

// filterReleases applies regex filters to the release list.
func (p *Pruner) filterReleases(releases []*releasev1.Release) []*releasev1.Release {
	var filtered []*releasev1.Release

	for _, rel := range releases {
		// Check namespace inclusion filter
		if p.opts.NamespaceFilter != nil {
			if !p.opts.NamespaceFilter.MatchString(rel.Namespace) {
				p.logger.Debug("skipping release (namespace filter)",
					"name", rel.Name,
					"namespace", rel.Namespace)
				continue
			}
		}

		// Check namespace exclusion filter
		if p.opts.NamespaceExclude != nil {
			if p.opts.NamespaceExclude.MatchString(rel.Namespace) {
				p.logger.Debug("skipping release (namespace exclude)",
					"name", rel.Name,
					"namespace", rel.Namespace)
				continue
			}
		}

		// Check release name inclusion filter
		if p.opts.ReleaseFilter != nil {
			if !p.opts.ReleaseFilter.MatchString(rel.Name) {
				p.logger.Debug("skipping release (release filter)",
					"name", rel.Name,
					"namespace", rel.Namespace)
				continue
			}
		}

		// Check release name exclusion filter
		if p.opts.ReleaseExclude != nil {
			if p.opts.ReleaseExclude.MatchString(rel.Name) {
				p.logger.Debug("skipping release (release exclude)",
					"name", rel.Name,
					"namespace", rel.Namespace)
				continue
			}
		}

		filtered = append(filtered, rel)
	}

	return filtered
}

// selectReleasesToDelete applies age and count filters to determine which releases to delete.
// The --max-releases-to-keep limit is applied GLOBALLY across all filtered releases,
// keeping the N most recently deployed releases and marking the rest for deletion.
func (p *Pruner) selectReleasesToDelete(releases []*releasev1.Release) []*releasev1.Release {
	if len(releases) == 0 {
		return nil
	}

	// Sort by last deployed time (newest first) for count-based filtering
	sorted := make([]*releasev1.Release, len(releases))
	copy(sorted, releases)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Info.LastDeployed.After(sorted[j].Info.LastDeployed)
	})

	// Track releases to delete using a map to avoid duplicates
	toDeleteMap := make(map[*releasev1.Release]bool)
	now := time.Now()

	// Apply count-based filtering (keep N newest globally)
	if p.opts.MaxReleasesToKeep > 0 && len(sorted) > p.opts.MaxReleasesToKeep {
		for i := p.opts.MaxReleasesToKeep; i < len(sorted); i++ {
			rel := sorted[i]
			p.logger.Debug("release exceeds global max count",
				"name", rel.Name,
				"namespace", rel.Namespace,
				"position", i,
				"max", p.opts.MaxReleasesToKeep)
			toDeleteMap[rel] = true
		}
	}

	// Apply age-based filtering
	if p.opts.OlderThan > 0 {
		for _, rel := range releases {
			if toDeleteMap[rel] {
				continue // Already marked for deletion
			}
			age := now.Sub(rel.Info.LastDeployed)
			if age > p.opts.OlderThan {
				p.logger.Debug("release exceeds age limit",
					"name", rel.Name,
					"namespace", rel.Namespace,
					"age", age,
					"limit", p.opts.OlderThan)
				toDeleteMap[rel] = true
			}
		}
	}

	// Convert map to slice
	toDelete := make([]*releasev1.Release, 0, len(toDeleteMap))
	for rel := range toDeleteMap {
		toDelete = append(toDelete, rel)
	}

	return toDelete
}

// deleteRelease uninstalls a Helm release.
func (p *Pruner) deleteRelease(ctx context.Context, name, namespace string) error {
	// Check for context cancellation before making API calls
	if ctx.Err() != nil {
		return ctx.Err()
	}

	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(p.settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER")); err != nil {
		return err
	}

	uninstall := action.NewUninstall(actionConfig)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = 5 * time.Minute
	_, err := uninstall.Run(name)
	return err
}

// deleteNamespaceIfEmpty deletes a namespace if it has no Helm releases remaining.
func (p *Pruner) deleteNamespaceIfEmpty(ctx context.Context, namespace string) error {
	// Never delete system namespaces
	if p.systemNamespaces[namespace] {
		p.logger.Debug("not deleting system namespace",
			"namespace", namespace)
		return nil
	}

	hasReleases, err := p.namespaceHasReleases(ctx, namespace)
	if err != nil {
		return fmt.Errorf("failed to check releases in namespace: %w", err)
	}

	if hasReleases {
		p.logger.Debug("namespace still has releases, not deleting",
			"namespace", namespace)
		return nil
	}

	// Namespace is empty of Helm releases, delete it
	if p.opts.DryRun {
		p.logger.Info("would delete empty namespace", "namespace", namespace)
		return nil
	}

	p.logger.Info("deleting empty namespace", "namespace", namespace)
	if err := p.k8s.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{}); err != nil {
		return err
	}
	namespacesDeletedTotal.Inc()
	return nil
}

// newKubernetesClient creates a Kubernetes clientset.
func newKubernetesClient() (kubernetes.Interface, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
		}
	}

	return kubernetes.NewForConfig(config)
}
