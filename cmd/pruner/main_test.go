package main

import (
	"testing"
	"time"

	"github.com/spf13/cobra" //nolint:depguard
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

func TestParseLabelSelector(t *testing.T) {
	tests := []struct {
		input       string
		wantKey     string
		wantPattern string
		wantErr     bool
	}{
		{
			input:       "env=preview",
			wantKey:     "env",
			wantPattern: "preview",
		},
		{
			input:       "gc-policy=weekly",
			wantKey:     "gc-policy",
			wantPattern: "weekly",
		},
		{
			input:       "ephemeral",
			wantKey:     "ephemeral",
			wantPattern: ".*",
		},
		{
			input:       "key=",
			wantKey:     "key",
			wantPattern: "",
		},
		{
			input:       "env=prev.*",
			wantKey:     "env",
			wantPattern: "prev.*",
		},
		{
			input:   "",
			wantErr: true,
		},
		{
			input:   "=value",
			wantErr: true,
		},
		{
			input:   "key=[invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseLabelSelector(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseLabelSelector(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseLabelSelector(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Key != tt.wantKey {
				t.Errorf("parseLabelSelector(%q).Key = %q, want %q", tt.input, got.Key, tt.wantKey)
			}
			if got.Value.String() != tt.wantPattern {
				t.Errorf("parseLabelSelector(%q).Value = %q, want %q", tt.input, got.Value.String(), tt.wantPattern)
			}
		})
	}
}

func TestLabelFlagsValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "valid label-filter key=value",
			args:    []string{"--label-filter", "env=preview", "--older-than", "1h", "--once"},
			wantErr: false,
		},
		{
			name:    "valid label-filter existence only",
			args:    []string{"--label-filter", "ephemeral", "--older-than", "1h", "--once"},
			wantErr: false,
		},
		{
			name:    "repeated label-filter",
			args:    []string{"--label-filter", "env=preview", "--label-filter", "gc-policy=weekly", "--older-than", "1h", "--once"},
			wantErr: false,
		},
		{
			name:    "valid label-exclude",
			args:    []string{"--label-exclude", "protected", "--older-than", "1h", "--once"},
			wantErr: false,
		},
		{
			name:    "label-filter alone satisfies filter requirement",
			args:    []string{"--label-filter", "env=preview", "--once"},
			wantErr: false,
		},
		{
			name:    "label-exclude alone satisfies filter requirement",
			args:    []string{"--label-exclude", "env=production", "--once"},
			wantErr: false,
		},
		{
			name:    "invalid label-filter empty key",
			args:    []string{"--label-filter", "=badvalue", "--older-than", "1h", "--once"},
			wantErr: true,
		},
		{
			name:    "invalid label-exclude empty key",
			args:    []string{"--label-exclude", "=badvalue", "--older-than", "1h", "--once"},
			wantErr: true,
		},
		{
			name:    "invalid label-filter empty string",
			args:    []string{"--label-filter", "", "--older-than", "1h", "--once"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.RunE = func(c *cobra.Command, args []string) error { return nil }
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if tt.wantErr && err == nil {
				t.Errorf("expected error for args %v, got nil", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for args %v: %v", tt.args, err)
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
