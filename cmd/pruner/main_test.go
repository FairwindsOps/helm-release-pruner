package main

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		// Standard Go durations
		{"1h", 1 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"336h", 336 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"500ms", 500 * time.Millisecond, false},

		// Custom day suffix
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"14d", 14 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},

		// Custom week suffix
		{"1w", 7 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"4w", 28 * 24 * time.Hour, false},

		// Edge cases
		{"0d", 0, false},
		{"0w", 0, false},

		// Errors
		{"", 0, true},
		{"d", 0, true},
		{"w", 0, true},
		{"abc", 0, true},
		{"1x", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDuration(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("parseDuration(%q) unexpected error: %v", tt.input, err)
				return
			}

			if got != tt.expected {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseDurationEquivalence(t *testing.T) {
	// Verify that our custom suffixes are equivalent to Go hour notation
	tests := []struct {
		custom   string
		standard string
	}{
		{"1d", "24h"},
		{"7d", "168h"},
		{"1w", "168h"},
		{"2w", "336h"},
	}

	for _, tt := range tests {
		t.Run(tt.custom+"="+tt.standard, func(t *testing.T) {
			custom, err := parseDuration(tt.custom)
			if err != nil {
				t.Fatalf("failed to parse custom duration %q: %v", tt.custom, err)
			}

			standard, err := parseDuration(tt.standard)
			if err != nil {
				t.Fatalf("failed to parse standard duration %q: %v", tt.standard, err)
			}

			if custom != standard {
				t.Errorf("parseDuration(%q) = %v, parseDuration(%q) = %v, want equal",
					tt.custom, custom, tt.standard, standard)
			}
		})
	}
}
