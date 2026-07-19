package eventtime

import (
	"testing"
	"time"
)

func TestFormatSortsChronologically(t *testing.T) {
	whole := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fractional := whole.Add(100 * time.Millisecond)
	if Format(whole) >= Format(fractional) {
		t.Fatalf("formatted timestamps do not sort chronologically: %q >= %q", Format(whole), Format(fractional))
	}
}
