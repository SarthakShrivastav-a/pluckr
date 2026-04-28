package types

import (
	"testing"
	"time"
)

func TestParseRefresh(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"manual", 0},
		{"never", 0},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1h", time.Hour},
		{"45m", 45 * time.Minute},
		{"garbage", 0},
	}
	for _, tc := range cases {
		got := ParseRefresh(tc.in)
		if got != tc.want {
			t.Errorf("ParseRefresh(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
