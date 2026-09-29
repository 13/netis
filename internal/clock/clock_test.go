package clock

import (
	"testing"
	"time"
)

func TestFreezeAndRestore(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	restore := Freeze(at)
	if got := Now(); !got.Equal(at) {
		t.Fatalf("frozen Now = %v, want %v", got, at)
	}
	restore()
	if got := Now(); time.Since(got) > time.Minute || got.Equal(at) {
		t.Fatalf("restored Now = %v, want the real time", got)
	}
}
