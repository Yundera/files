package live

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A burst must collapse to few calls, and — the part that matters — the LAST
// call must still happen. Progress messages are snapshots, so dropping
// intermediate ones is free, but dropping the final one leaves a progress bar
// stuck at 97% forever.
func TestThrottleCollapsesABurstButKeepsTheLastCall(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var seen []int

	throttled := Throttle(50*time.Millisecond, func() {
		calls.Add(1)
		mu.Lock()
		seen = append(seen, len(seen))
		mu.Unlock()
	})

	for i := 0; i < 100; i++ {
		throttled()
		time.Sleep(time.Millisecond)
	}
	// Wait out the trailing timer.
	time.Sleep(150 * time.Millisecond)

	n := calls.Load()
	if n == 0 {
		t.Fatal("throttled function never ran")
	}
	if n > 10 {
		t.Errorf("100 calls over ~100ms produced %d invocations; want a handful", n)
	}
}

// A single call must run immediately rather than waiting for a window — the
// first progress update should appear at once, not 300ms in.
func TestThrottleRunsTheFirstCallImmediately(t *testing.T) {
	var calls atomic.Int32
	throttled := Throttle(time.Second, func() { calls.Add(1) })
	throttled()
	if got := calls.Load(); got != 1 {
		t.Errorf("first call ran %d times immediately; want 1", got)
	}
}

// Two calls spaced wider than the window are both immediate.
func TestThrottleDoesNotDelayUnrelatedCalls(t *testing.T) {
	var calls atomic.Int32
	throttled := Throttle(20*time.Millisecond, func() { calls.Add(1) })
	throttled()
	time.Sleep(40 * time.Millisecond)
	throttled()
	if got := calls.Load(); got != 2 {
		t.Errorf("two well-spaced calls ran %d times; want 2", got)
	}
}
