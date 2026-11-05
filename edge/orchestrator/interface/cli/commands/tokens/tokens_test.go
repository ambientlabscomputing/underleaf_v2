package tokens

import (
	"testing"
	"time"
)

func TestParseTTL(t *testing.T) {
	ok := map[string]time.Duration{
		"30d": 30 * 24 * time.Hour, "90m": 90 * time.Minute, "12h": 12 * time.Hour,
		"2w": 14 * 24 * time.Hour, "45s": 45 * time.Second, " 7d ": 7 * 24 * time.Hour,
	}
	for in, want := range ok {
		if got, err := ParseTTL(in); err != nil || got != want {
			t.Fatalf("ParseTTL(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "d", "30", "0d", "-5d", "1.5d", "30x", "abc", "30 d", "9999999999999999d", "d30"} {
		if got, err := ParseTTL(bad); err == nil {
			t.Fatalf("ParseTTL(%q) = %v, want error", bad, got)
		}
	}
}
