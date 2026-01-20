package pruner

import (
	"io"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
)

// newTestPruner creates a Pruner with a no-op logger for testing.
func newTestPruner(opts Options) *Pruner {
	return &Pruner{
		opts:             opts,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		systemNamespaces: map[string]bool{"default": true, "kube-system": true, "kube-public": true, "kube-node-lease": true},
	}
}

// mockRelease creates a test release with the given parameters.
func mockRelease(name, namespace string, lastDeployed time.Time) *releasev1.Release {
	return &releasev1.Release{
		Name:      name,
		Namespace: namespace,
		Info: &releasev1.Info{
			LastDeployed: lastDeployed,
			Status:       common.StatusDeployed,
		},
	}
}

func TestFilterReleases(t *testing.T) {
	now := time.Now()
	releases := []*releasev1.Release{
		mockRelease("feature-abc-web", "feature-abc", now),
		mockRelease("feature-xyz-web", "feature-xyz", now),
		mockRelease("production-web", "production", now),
		mockRelease("staging-web", "staging", now),
		mockRelease("feature-permanent-web", "feature-permanent", now),
	}

	tests := []struct {
		name             string
		opts             Options
		expectedCount    int
		expectedReleases []string
	}{
		{
			name:             "no filters - returns all",
			opts:             Options{},
			expectedCount:    5,
			expectedReleases: []string{"feature-abc-web", "feature-xyz-web", "production-web", "staging-web", "feature-permanent-web"},
		},
		{
			name: "release filter - only feature releases",
			opts: Options{
				ReleaseFilter: regexp.MustCompile(`^feature-.+-web$`),
			},
			expectedCount:    3,
			expectedReleases: []string{"feature-abc-web", "feature-xyz-web", "feature-permanent-web"},
		},
		{
			name: "namespace filter - only feature namespaces",
			opts: Options{
				NamespaceFilter: regexp.MustCompile(`^feature-.+`),
			},
			expectedCount:    3,
			expectedReleases: []string{"feature-abc-web", "feature-xyz-web", "feature-permanent-web"},
		},
		{
			name: "release exclude - exclude permanent",
			opts: Options{
				ReleaseExclude: regexp.MustCompile(`-permanent-`),
			},
			expectedCount:    4,
			expectedReleases: []string{"feature-abc-web", "feature-xyz-web", "production-web", "staging-web"},
		},
		{
			name: "namespace exclude - exclude production",
			opts: Options{
				NamespaceExclude: regexp.MustCompile(`^production$`),
			},
			expectedCount:    4,
			expectedReleases: []string{"feature-abc-web", "feature-xyz-web", "staging-web", "feature-permanent-web"},
		},
		{
			name: "combined filters",
			opts: Options{
				NamespaceFilter: regexp.MustCompile(`^feature-.+`),
				ReleaseExclude:  regexp.MustCompile(`-permanent-`),
			},
			expectedCount:    2,
			expectedReleases: []string{"feature-abc-web", "feature-xyz-web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPruner(tt.opts)
			filtered := p.filterReleases(releases)

			if len(filtered) != tt.expectedCount {
				t.Errorf("expected %d releases, got %d", tt.expectedCount, len(filtered))
			}

			// Verify expected releases are present
			filteredNames := make(map[string]bool)
			for _, r := range filtered {
				filteredNames[r.Name] = true
			}
			for _, expected := range tt.expectedReleases {
				if !filteredNames[expected] {
					t.Errorf("expected release %q to be in filtered results", expected)
				}
			}
		})
	}
}

func TestSelectReleasesToDelete_MaxReleasesGlobal(t *testing.T) {
	now := time.Now()

	// Create releases from different apps with varying ages
	// The --max-releases-to-keep is applied GLOBALLY, keeping the N newest
	releases := []*releasev1.Release{
		mockRelease("app-a", "ns-a", now.Add(-1*time.Hour)),  // newest
		mockRelease("app-b", "ns-b", now.Add(-2*time.Hour)),  // 2nd newest
		mockRelease("app-c", "ns-c", now.Add(-3*time.Hour)),  // 3rd newest
		mockRelease("app-d", "ns-d", now.Add(-4*time.Hour)),  // 4th newest
		mockRelease("app-e", "ns-e", now.Add(-5*time.Hour)),  // oldest
	}

	tests := []struct {
		name              string
		maxReleasesToKeep int
		expectedDeleted   int
		expectedKept      []string // release names that should NOT be deleted
	}{
		{
			name:              "keep 2 globally - delete 3 oldest",
			maxReleasesToKeep: 2,
			expectedDeleted:   3,
			expectedKept:      []string{"app-a", "app-b"},
		},
		{
			name:              "keep 3 globally - delete 2 oldest",
			maxReleasesToKeep: 3,
			expectedDeleted:   2,
			expectedKept:      []string{"app-a", "app-b", "app-c"},
		},
		{
			name:              "keep 5 globally - delete nothing",
			maxReleasesToKeep: 5,
			expectedDeleted:   0,
			expectedKept:      []string{"app-a", "app-b", "app-c", "app-d", "app-e"},
		},
		{
			name:              "keep 10 globally (more than exist) - delete nothing",
			maxReleasesToKeep: 10,
			expectedDeleted:   0,
			expectedKept:      []string{"app-a", "app-b", "app-c", "app-d", "app-e"},
		},
		{
			name:              "keep 1 globally - delete 4",
			maxReleasesToKeep: 1,
			expectedDeleted:   4,
			expectedKept:      []string{"app-a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPruner(Options{
				MaxReleasesToKeep: tt.maxReleasesToKeep,
			})

			toDelete := p.selectReleasesToDelete(releases)

			if len(toDelete) != tt.expectedDeleted {
				t.Errorf("expected %d releases to delete, got %d", tt.expectedDeleted, len(toDelete))
			}

			// Verify that the expected releases are NOT in the delete list
			deletedNames := make(map[string]bool)
			for _, r := range toDelete {
				deletedNames[r.Name] = true
			}
			for _, kept := range tt.expectedKept {
				if deletedNames[kept] {
					t.Errorf("release %q should be kept but was marked for deletion", kept)
				}
			}
		})
	}
}

func TestSelectReleasesToDelete_OlderThan(t *testing.T) {
	now := time.Now()

	releases := []*releasev1.Release{
		mockRelease("app-1h", "default", now.Add(-1*time.Hour)),   // 1 hour old
		mockRelease("app-1d", "default", now.Add(-24*time.Hour)),  // 1 day old
		mockRelease("app-2d", "default", now.Add(-48*time.Hour)),  // 2 days old
		mockRelease("app-1w", "default", now.Add(-168*time.Hour)), // 1 week old
	}

	tests := []struct {
		name            string
		olderThan       time.Duration
		expectedDeleted int
	}{
		{
			name:            "older than 30 minutes - delete all",
			olderThan:       30 * time.Minute,
			expectedDeleted: 4,
		},
		{
			name:            "older than 2 hours - delete 3",
			olderThan:       2 * time.Hour,
			expectedDeleted: 3,
		},
		{
			name:            "older than 3 days - delete 1",
			olderThan:       72 * time.Hour,
			expectedDeleted: 1,
		},
		{
			name:            "older than 2 weeks - delete none",
			olderThan:       336 * time.Hour,
			expectedDeleted: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPruner(Options{
				OlderThan: tt.olderThan,
			})

			toDelete := p.selectReleasesToDelete(releases)

			if len(toDelete) != tt.expectedDeleted {
				t.Errorf("expected %d releases to delete, got %d", tt.expectedDeleted, len(toDelete))
			}
		})
	}
}

func TestSelectReleasesToDelete_CombinedFilters(t *testing.T) {
	now := time.Now()

	// 5 releases of varying ages
	releases := []*releasev1.Release{
		mockRelease("app-1", "default", now.Add(-1*time.Hour)),   // newest
		mockRelease("app-2", "default", now.Add(-2*time.Hour)),   // 2nd
		mockRelease("app-3", "default", now.Add(-3*time.Hour)),   // 3rd
		mockRelease("app-4", "default", now.Add(-48*time.Hour)),  // old
		mockRelease("app-5", "default", now.Add(-168*time.Hour)), // very old
	}

	// Keep 3 newest, but also delete anything older than 24 hours
	p := newTestPruner(Options{
		MaxReleasesToKeep: 3,
		OlderThan:         24 * time.Hour,
	})

	toDelete := p.selectReleasesToDelete(releases)

	// MaxReleasesToKeep=3 would keep app-1, app-2, app-3 and delete app-4, app-5
	// OlderThan=24h would delete app-4, app-5
	// Combined: delete app-4 and app-5 (union of both rules, no duplicates)
	if len(toDelete) != 2 {
		t.Errorf("expected 2 releases to delete, got %d", len(toDelete))
	}

	// Verify the oldest two are marked for deletion
	deletedNames := make(map[string]bool)
	for _, r := range toDelete {
		deletedNames[r.Name] = true
	}

	if !deletedNames["app-4"] {
		t.Error("expected app-4 to be deleted")
	}
	if !deletedNames["app-5"] {
		t.Error("expected app-5 to be deleted")
	}
}

func TestSelectReleasesToDelete_EmptyInput(t *testing.T) {
	p := newTestPruner(Options{
		MaxReleasesToKeep: 5,
		OlderThan:         24 * time.Hour,
	})

	toDelete := p.selectReleasesToDelete(nil)
	if toDelete != nil {
		t.Errorf("expected nil for empty input, got %v", toDelete)
	}

	toDelete = p.selectReleasesToDelete([]*releasev1.Release{})
	if len(toDelete) != 0 {
		t.Errorf("expected empty slice for empty input, got %d items", len(toDelete))
	}
}

func TestSystemNamespaces(t *testing.T) {
	// Verify default system namespaces are protected
	expectedProtected := []string{
		"default",
		"kube-system",
		"kube-public",
		"kube-node-lease",
	}

	for _, ns := range expectedProtected {
		found := false
		for _, defaultNS := range defaultSystemNamespaces {
			if defaultNS == ns {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to be a protected system namespace", ns)
		}
	}
}

func TestSystemNamespaces_Custom(t *testing.T) {
	opts := Options{
		AdditionalSystemNamespaces: []string{"monitoring", "logging"},
	}

	// Create pruner with custom system namespaces
	p := &Pruner{
		opts:             opts,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		systemNamespaces: make(map[string]bool),
	}

	// Initialize system namespaces like New() does
	for _, ns := range defaultSystemNamespaces {
		p.systemNamespaces[ns] = true
	}
	for _, ns := range opts.AdditionalSystemNamespaces {
		p.systemNamespaces[ns] = true
	}

	// Check that both default and custom are protected
	if !p.systemNamespaces["kube-system"] {
		t.Error("expected kube-system to be protected")
	}
	if !p.systemNamespaces["monitoring"] {
		t.Error("expected monitoring to be protected")
	}
	if !p.systemNamespaces["logging"] {
		t.Error("expected logging to be protected")
	}
}

func TestInitializedAndReady(t *testing.T) {
	p := &Pruner{
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		systemNamespaces: make(map[string]bool),
	}

	// Initially neither initialized nor ready
	if p.Initialized() {
		t.Error("expected Initialized() to be false initially")
	}
	if p.Ready() {
		t.Error("expected Ready() to be false initially")
	}

	// After first attempt (even failed), should be initialized but not ready
	p.initialized.Store(true)
	if !p.Initialized() {
		t.Error("expected Initialized() to be true after first attempt")
	}
	if p.Ready() {
		t.Error("expected Ready() to be false after failed attempt")
	}

	// After successful cycle, should be both initialized and ready
	p.ready.Store(true)
	if !p.Initialized() {
		t.Error("expected Initialized() to be true")
	}
	if !p.Ready() {
		t.Error("expected Ready() to be true after successful cycle")
	}
}

func TestHasReleasePruningFilters(t *testing.T) {
	tests := []struct {
		name     string
		opts     Options
		expected bool
	}{
		{
			name:     "no filters",
			opts:     Options{},
			expected: false,
		},
		{
			name:     "only OlderThan",
			opts:     Options{OlderThan: time.Hour},
			expected: true,
		},
		{
			name:     "only MaxReleasesToKeep",
			opts:     Options{MaxReleasesToKeep: 5},
			expected: true,
		},
		{
			name:     "only ReleaseFilter",
			opts:     Options{ReleaseFilter: regexp.MustCompile(".*")},
			expected: true,
		},
		{
			name:     "only NamespaceFilter",
			opts:     Options{NamespaceFilter: regexp.MustCompile(".*")},
			expected: true,
		},
		{
			name:     "only ReleaseExclude",
			opts:     Options{ReleaseExclude: regexp.MustCompile(".*")},
			expected: true,
		},
		{
			name:     "only NamespaceExclude",
			opts:     Options{NamespaceExclude: regexp.MustCompile(".*")},
			expected: true,
		},
		{
			name: "multiple filters",
			opts: Options{
				OlderThan:     time.Hour,
				ReleaseFilter: regexp.MustCompile(".*"),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPruner(tt.opts)
			if got := p.hasReleasePruningFilters(); got != tt.expected {
				t.Errorf("hasReleasePruningFilters() = %v, want %v", got, tt.expected)
			}
		})
	}
}
