// Package mtime provides time operations using a monotonic time source, which
// is useful when you want to work with elapsed rather than wall time. Based on
// github.com/aristanetworks/monotime. mtime uses Instants. INSTANTS ARE NOT
// REAL TIMES, they are only useful for measuring elapsed time.
package mtime

import (
	"github.com/aristanetworks/goarista/monotime"
	"time"
)

// An Instant represents an instant in monotonically increasing time. INSTANTS
// ARE NOT REAL TIMES, they are only useful for measuring elapsed time.
type Instant uint64

// Add adds a duration to an Instant
func (i Instant) Add(d time.Duration) Instant {
	return Instant(uint64(time.Duration(i) + d))
}

// Sub subtracts an Instant from an Instant
func (i Instant) Sub(o Instant) time.Duration {
	return time.Duration(i - o)
}

// Now() returns an instant in monotonic time
func Now() Instant {
	return Instant(monotime.Now())
}

// Stopwatch starts a stopwatch and returns a function that itself returns the
// amount of time elapsed since the start of the stopwatch.
func Stopwatch() (elapsed func() time.Duration) {
	start := Now()
	return func() time.Duration {
		return Now().Sub(start)
	}
}
