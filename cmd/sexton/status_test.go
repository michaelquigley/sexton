package main

import (
	"testing"
	"time"
)

func TestFormatLastSync(t *testing.T) {
	now := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		lastSync string
		want     string
	}{
		{
			name:     "empty",
			lastSync: "",
			want:     "-",
		},
		{
			name:     "seconds",
			lastSync: now.Add(-45 * time.Second).Format(time.RFC3339),
			want:     "45s ago",
		},
		{
			name:     "minutes",
			lastSync: now.Add(-5 * time.Minute).Format(time.RFC3339),
			want:     "5m ago",
		},
		{
			name:     "hours",
			lastSync: now.Add(-2 * time.Hour).Format(time.RFC3339),
			want:     "2h ago",
		},
		{
			name:     "days",
			lastSync: now.Add(-72 * time.Hour).Format(time.RFC3339),
			want:     "3d ago",
		},
		{
			name:     "future clamps to zero",
			lastSync: now.Add(30 * time.Second).Format(time.RFC3339),
			want:     "0s ago",
		},
		{
			name:     "invalid falls back",
			lastSync: "not-a-time",
			want:     "not-a-time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatLastSync(tt.lastSync, now); got != tt.want {
				t.Fatalf("formatLastSync(%q) = %q, want %q", tt.lastSync, got, tt.want)
			}
		})
	}
}
