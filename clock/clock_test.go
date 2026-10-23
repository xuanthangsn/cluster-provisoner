package clock_test

import (
	"testing"
	"time"

	"sim.dev/engine/clock"
)

var epoch = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func TestFakeReadsWhatItWasGiven(t *testing.T) {
	c := clock.NewFake(epoch)
	if !c.Now().Equal(epoch) {
		t.Errorf("Now = %v, want %v", c.Now(), epoch)
	}
}

func TestFakeMovesOnlyWhenAdvanced(t *testing.T) {
	c := clock.NewFake(epoch)
	c.Advance(90 * time.Second)
	if got, want := c.Now(), epoch.Add(90*time.Second); !got.Equal(want) {
		t.Errorf("Now = %v, want %v", got, want)
	}
}

func TestAfterFiresOnlyOnceDue(t *testing.T) {
	c := clock.NewFake(epoch)
	ch := c.After(10 * time.Second)

	c.Advance(9 * time.Second)
	select {
	case <-ch:
		t.Fatal("fired before it was due")
	default:
	}

	c.Advance(time.Second)
	select {
	case at := <-ch:
		if want := epoch.Add(10 * time.Second); !at.Equal(want) {
			t.Errorf("fired with %v, want %v", at, want)
		}
	default:
		t.Fatal("did not fire once due")
	}
}

func TestAfterWithNonPositiveDurationFiresImmediately(t *testing.T) {
	c := clock.NewFake(epoch)
	for _, d := range []time.Duration{0, -time.Second} {
		select {
		case <-c.After(d):
		default:
			t.Errorf("After(%v) did not fire immediately", d)
		}
	}
}

func TestOneAdvanceFiresEveryDueWaiter(t *testing.T) {
	c := clock.NewFake(epoch)
	early, late, never := c.After(time.Second), c.After(2*time.Second), c.After(time.Hour)

	c.Advance(5 * time.Second)

	for name, ch := range map[string]<-chan time.Time{"early": early, "late": late} {
		select {
		case <-ch:
		default:
			t.Errorf("%s waiter did not fire", name)
		}
	}
	select {
	case <-never:
		t.Error("a waiter an hour away fired")
	default:
	}
	if got := c.Waiters(); got != 1 {
		t.Errorf("Waiters = %d, want 1", got)
	}
}

func TestRealClockSatisfiesTheInterface(t *testing.T) {
	var c clock.Clock = clock.Real{}
	before := time.Now()
	if c.Now().Before(before) {
		t.Error("Real.Now went backwards")
	}
}
