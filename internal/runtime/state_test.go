package runtime

import (
	"runtime"
	"testing"
	"time"
)

func TestSafeWaitGroupTimedWaitDoesNotLeakGoroutines(t *testing.T) {
	wg := newSafeWaitGroup()
	if _, err := wg.Add(1); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	before := runtime.NumGoroutine()
	for range 24 {
		if wg.Wait(1 * time.Millisecond) {
			t.Fatalf("Wait unexpectedly succeeded before the counter reached zero")
		}
	}

	after := runtime.NumGoroutine()
	if delta := after - before; delta > 4 {
		t.Fatalf("timed Wait leaked goroutines: before=%d after=%d delta=%d", before, after, delta)
	}

	if _, err := wg.Done(); err != nil {
		t.Fatalf("Done returned error: %v", err)
	}
	if !wg.Wait(50 * time.Millisecond) {
		t.Fatalf("Wait did not observe the counter reaching zero")
	}
}
