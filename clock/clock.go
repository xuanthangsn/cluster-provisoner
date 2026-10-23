// Package clock is the engine's only source of time above the Machine
// provider.
//
// Every operation that waits or measures takes a Clock instead of calling the
// time package, so that Probe intervals, timeouts and retry windows can be
// tested in microseconds rather than minutes (NFR-10). An architecture test
// fails the build if a package above the provider reads the system clock
// directly.
package clock

import (
	"sort"
	"sync"
	"time"
)

// Clock tells the time and waits.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// After returns a channel that receives the time once d has elapsed, like
	// time.After. A non-positive d fires immediately.
	After(d time.Duration) <-chan time.Time
}

// Real is the system clock.
type Real struct{}

// Now returns time.Now().
func (Real) Now() time.Time { return time.Now() }

// After returns time.After(d).
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Fake is a Clock that moves only when Advance is called. It is safe for
// concurrent use.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

// NewFake returns a Fake reading start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now returns the fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// After returns a channel that fires once the fake has been advanced by d.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- f.now
		return ch
	}
	f.waiters = append(f.waiters, waiter{at: f.now.Add(d), ch: ch})
	return ch
}

// Advance moves the fake forward by d, firing every waiter that has become
// due, in the order they fall due.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.now = f.now.Add(d)

	sort.SliceStable(f.waiters, func(i, j int) bool { return f.waiters[i].at.Before(f.waiters[j].at) })
	remaining := f.waiters[:0]
	for _, w := range f.waiters {
		if w.at.After(f.now) {
			remaining = append(remaining, w)
			continue
		}
		w.ch <- w.at // buffered; never blocks
	}
	f.waiters = remaining
}

// Waiters reports how many After channels have not fired yet, so a test can
// wait for the code under test to block before advancing.
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.waiters)
}
