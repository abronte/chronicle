package internal

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"0.2.2", "0.3.0", -1},
		{"0.3.0", "0.2.2", 1},
		{"10.0.0", "9.0.0", 1},
		{"1.10.0", "1.9.0", 1},
		{"1.0.10", "1.0.9", 1},
		{"1.0", "1.0.0", 0},
		{"1.0.0", "1.0", 0},
		{"1", "1.0.0", 0},
		{"2", "1.0.0", 1},
		{"1", "2.0.0", -1},
		{"1.0.0", "1", 0},
		{"1.0.0-beta", "1.0.0", 0},
		{"v1.0.0", "1.0.0", -1},
		{"1.0.0", "v1.0.0", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}
