package pruner

import (
	"regexp"
	"time"
)

// Options configures the pruner behavior.
type Options struct {
	// Interval is how often to run the pruning loop.
	// Only used in daemon mode.
	Interval time.Duration

	// MaxReleasesToKeep is the maximum number of releases to keep globally.
	// After applying all filters, releases beyond this count (sorted by date,
	// newest first) will be deleted. 0 means no limit based on count.
	MaxReleasesToKeep int

	// OlderThan specifies the age threshold.
	// Releases older than this duration will be deleted.
	// 0 means no age-based filtering.
	OlderThan time.Duration

	// ReleaseFilter is a regex that release names must match to be considered.
	// nil means all releases are considered.
	ReleaseFilter *regexp.Regexp

	// NamespaceFilter is a regex that namespaces must match to be considered.
	// nil means all namespaces are considered.
	NamespaceFilter *regexp.Regexp

	// ReleaseExclude is a regex that excludes matching releases.
	// nil means no releases are excluded by name.
	ReleaseExclude *regexp.Regexp

	// NamespaceExclude is a regex that excludes matching namespaces.
	// nil means no namespaces are excluded.
	NamespaceExclude *regexp.Regexp

	// PreserveNamespace prevents deletion of empty namespaces after release deletion.
	PreserveNamespace bool

	// CleanupOrphanNamespaces enables scanning for and deleting namespaces
	// that have no Helm releases but still exist in the cluster.
	CleanupOrphanNamespaces bool

	// OrphanNamespaceFilter is a regex that namespaces must match to be
	// considered for orphan cleanup. Required when CleanupOrphanNamespaces is true.
	OrphanNamespaceFilter *regexp.Regexp

	// OrphanNamespaceExclude is a regex that excludes matching namespaces
	// from orphan cleanup (e.g., system namespaces).
	OrphanNamespaceExclude *regexp.Regexp

	// DeleteRateLimit is the minimum duration to wait between delete operations.
	// This prevents overwhelming the Kubernetes API server.
	// 0 means no rate limiting.
	DeleteRateLimit time.Duration

	// AdditionalSystemNamespaces is a list of namespace names that should be
	// treated as system namespaces and never deleted. These are added to the
	// default list (default, kube-system, kube-public, kube-node-lease).
	AdditionalSystemNamespaces []string

	// DryRun shows what would be deleted without actually deleting.
	DryRun bool

	// Debug enables verbose logging.
	Debug bool
}
