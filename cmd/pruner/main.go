package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/FairwindsOps/helm-release-pruner/pkg/pruner"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var opts pruner.Options

	var (
		interval                   time.Duration
		olderThan                  string
		releaseFilter              string
		namespaceFilter            string
		releaseExcludeFilter       string
		namespaceExclude           string
		orphanNamespaceFilter      string
		orphanNamespaceExclude     string
		healthAddr                 string
		deleteRateLimit            time.Duration
		additionalSystemNamespaces string
	)

	cmd := &cobra.Command{
		Use:   "helm-release-pruner",
		Short: "Automatically delete old Helm releases and orphan namespaces",
		Long: `A daemon for automatically deleting old Helm releases based on age,
count limits, and regex filters. Can also clean up orphaned namespaces that
have no Helm releases. Runs continuously and prunes at configurable intervals.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Set interval
			opts.Interval = interval

			// Set rate limit
			opts.DeleteRateLimit = deleteRateLimit

			// Parse additional system namespaces
			if additionalSystemNamespaces != "" {
				opts.AdditionalSystemNamespaces = strings.Split(additionalSystemNamespaces, ",")
				for i, ns := range opts.AdditionalSystemNamespaces {
					opts.AdditionalSystemNamespaces[i] = strings.TrimSpace(ns)
				}
			}

			// Parse duration string for older-than
			if olderThan != "" {
				d, err := parseDuration(olderThan)
				if err != nil {
					return fmt.Errorf("invalid --older-than value: %w", err)
				}
				opts.OlderThan = d
			}

			// Compile regex filters for release pruning
			if releaseFilter != "" {
				re, err := regexp.Compile(releaseFilter)
				if err != nil {
					return fmt.Errorf("invalid --release-filter regex: %w", err)
				}
				opts.ReleaseFilter = re
			}

			if namespaceFilter != "" {
				re, err := regexp.Compile(namespaceFilter)
				if err != nil {
					return fmt.Errorf("invalid --namespace-filter regex: %w", err)
				}
				opts.NamespaceFilter = re
			}

			if releaseExcludeFilter != "" {
				re, err := regexp.Compile(releaseExcludeFilter)
				if err != nil {
					return fmt.Errorf("invalid --release-exclude regex: %w", err)
				}
				opts.ReleaseExclude = re
			}

			if namespaceExclude != "" {
				re, err := regexp.Compile(namespaceExclude)
				if err != nil {
					return fmt.Errorf("invalid --namespace-exclude regex: %w", err)
				}
				opts.NamespaceExclude = re
			}

			// Compile regex filters for orphan namespace cleanup
			if orphanNamespaceFilter != "" {
				re, err := regexp.Compile(orphanNamespaceFilter)
				if err != nil {
					return fmt.Errorf("invalid --orphan-namespace-filter regex: %w", err)
				}
				opts.OrphanNamespaceFilter = re
			}

			if orphanNamespaceExclude != "" {
				re, err := regexp.Compile(orphanNamespaceExclude)
				if err != nil {
					return fmt.Errorf("invalid --orphan-namespace-exclude regex: %w", err)
				}
				opts.OrphanNamespaceExclude = re
			}

			// Validate orphan namespace cleanup - filter is REQUIRED for safety
			if opts.CleanupOrphanNamespaces && opts.OrphanNamespaceFilter == nil {
				fmt.Fprintln(os.Stderr, "WARNING: --cleanup-orphan-namespaces requires --orphan-namespace-filter for safety; orphan cleanup disabled")
				opts.CleanupOrphanNamespaces = false
			}

			// Validate configuration
			hasReleasePruning := opts.OlderThan > 0 || opts.MaxReleasesToKeep > 0 ||
				opts.ReleaseFilter != nil || opts.NamespaceFilter != nil ||
				opts.ReleaseExclude != nil || opts.NamespaceExclude != nil

			if !hasReleasePruning && !opts.CleanupOrphanNamespaces {
				return fmt.Errorf("at least one of release pruning filters or --cleanup-orphan-namespaces (with --orphan-namespace-filter) must be specified")
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := pruner.New(opts)
			if err != nil {
				return fmt.Errorf("failed to initialize pruner: %w", err)
			}

			// Set up context with signal handling for graceful shutdown
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// Handle shutdown signals
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				sig := <-sigCh
				fmt.Printf("\nReceived signal %v, shutting down...\n", sig)
				cancel()
			}()

			// Start health server with pruner for readiness checks
			healthServer := startHealthServer(healthAddr, p)

			// Run the daemon
			err = p.RunDaemon(ctx)

			// Shutdown health server
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if shutdownErr := healthServer.Shutdown(shutdownCtx); shutdownErr != nil {
				fmt.Fprintf(os.Stderr, "health server shutdown error: %v\n", shutdownErr)
			}

			if errors.Is(err, context.Canceled) {
				return nil // Normal shutdown
			}
			return err
		},
	}

	// Flags
	flags := cmd.Flags()

	// Daemon settings
	flags.DurationVar(&interval, "interval", 1*time.Hour,
		"How often to run the pruning cycle")
	flags.StringVar(&healthAddr, "health-addr", ":8080",
		"Address for health check and metrics endpoints")
	flags.DurationVar(&deleteRateLimit, "delete-rate-limit", 100*time.Millisecond,
		"Minimum duration between delete operations to avoid overwhelming the API server (0 to disable)")

	// Release pruning filters
	flags.IntVar(&opts.MaxReleasesToKeep, "max-releases-to-keep", 0,
		"Maximum number of releases to keep globally after filtering (0 = no limit)")
	flags.StringVar(&olderThan, "older-than", "",
		"Delete releases older than this duration (e.g., '336h' for 2 weeks, '2w', '30d')")
	flags.StringVar(&releaseFilter, "release-filter", "",
		"Regex filter for release names (only matching releases are considered)")
	flags.StringVar(&namespaceFilter, "namespace-filter", "",
		"Regex filter for namespaces (only matching namespaces are considered)")
	flags.StringVar(&releaseExcludeFilter, "release-exclude", "",
		"Regex filter to exclude releases (matching releases are skipped)")
	flags.StringVar(&namespaceExclude, "namespace-exclude", "",
		"Regex filter to exclude namespaces (matching namespaces are skipped)")
	flags.BoolVar(&opts.PreserveNamespace, "preserve-namespace", false,
		"Do not delete namespaces even when empty after release deletion")

	// Orphan namespace cleanup
	flags.BoolVar(&opts.CleanupOrphanNamespaces, "cleanup-orphan-namespaces", false,
		"Enable cleanup of namespaces that have no Helm releases (requires --orphan-namespace-filter)")
	flags.StringVar(&orphanNamespaceFilter, "orphan-namespace-filter", "",
		"Regex filter for namespaces to consider for orphan cleanup (REQUIRED when using --cleanup-orphan-namespaces)")
	flags.StringVar(&orphanNamespaceExclude, "orphan-namespace-exclude", "",
		"Regex filter to exclude namespaces from orphan cleanup (e.g., 'kube-system|default')")

	// System namespace configuration
	flags.StringVar(&additionalSystemNamespaces, "system-namespaces", "",
		"Comma-separated list of additional namespaces to treat as system namespaces (never deleted)")

	// General options
	flags.BoolVar(&opts.DryRun, "dry-run", false,
		"Show what would be deleted without actually deleting")
	flags.BoolVar(&opts.Debug, "debug", false,
		"Enable debug logging")

	return cmd
}

// startHealthServer starts an HTTP server for health checks and metrics.
// The pruner is used to check actual readiness (connectivity verified).
func startHealthServer(addr string, p *pruner.Pruner) *http.Server {
	mux := http.NewServeMux()

	// Liveness probe - always returns OK if the process is running
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			// Log but don't fail - client may have disconnected
			fmt.Fprintf(os.Stderr, "health endpoint write error: %v\n", err)
		}
	})

	// Readiness probe - returns OK after initialization and if we can connect
	// Uses a more lenient check: ready after first attempt (even if failed)
	// but verifies current connectivity
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Check if we've attempted at least one cycle
		if !p.Initialized() {
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte("not ready: initializing")); err != nil {
				fmt.Fprintf(os.Stderr, "health endpoint write error: %v\n", err)
			}
			return
		}

		// Verify we can still connect to the cluster
		if err := p.CheckConnectivity(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte("not ready: " + err.Error())); err != nil {
				fmt.Fprintf(os.Stderr, "health endpoint write error: %v\n", err)
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		// Include whether we've had a successful cycle in the response
		status := "ok"
		if !p.Ready() {
			status = "ok (no successful cycle yet)"
		}
		if _, err := w.Write([]byte(status)); err != nil {
			fmt.Fprintf(os.Stderr, "health endpoint write error: %v\n", err)
		}
	})

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "health server error: %v\n", err)
		}
	}()

	return server
}

// parseDuration parses duration strings like "336h" or "2w" or "30d"
func parseDuration(s string) (time.Duration, error) {
	// Try standard Go duration first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle custom suffixes: d (days), w (weeks)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	unit := s[len(s)-1]
	valueStr := s[:len(s)-1]

	var value int
	if _, err := fmt.Sscanf(valueStr, "%d", &value); err != nil {
		return 0, fmt.Errorf("invalid duration value: %s", s)
	}

	switch unit {
	case 'd':
		return time.Duration(value) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit: %c", unit)
	}
}
