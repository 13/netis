// Package clock is where the web UI reads the time it renders pages at:
// relative times, day headings and the availability window. It is time.Now,
// except in the visual regression fixture (e2e/), which freezes it so every
// run renders the same page.
package clock

import (
	"sync/atomic"
	"time"
)

// frozen is the frozen time in Unix nanoseconds, or 0 when the clock runs.
var frozen atomic.Int64

// Now is the current time, or the frozen time while the clock is frozen.
func Now() time.Time {
	if n := frozen.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Now()
}

// Freeze stops the clock at t until the returned function restarts it.
func Freeze(t time.Time) (restore func()) {
	frozen.Store(t.UnixNano())
	return func() { frozen.Store(0) }
}
