package sandbox

import (
	"testing"
	"time"
)

// The sweep ends every pid it reaches, and passes again only while a pass still reached one.
func TestSweepEndsEveryReachablePid(t *testing.T) {
	alive := map[int]bool{7: true, 40: true, 99: true}
	passes := 0
	sweep(func(pid int) bool {
		if pid == 2 {
			passes++
		}
		if !alive[pid] {
			return false
		}
		delete(alive, pid)
		return true
	}, 40, 100, time.Now().Add(time.Minute))
	if len(alive) != 1 || !alive[40] || passes != 2 {
		t.Fatalf("alive after the sweep %v, want only its own pid 40; passes %d, want 2", alive, passes)
	}
}

// A process that a kill reaches but that never goes, such as one stuck in the kernel, cannot hold
// the sweep past its deadline.
func TestSweepReturnsAtItsDeadline(t *testing.T) {
	done := make(chan struct{})
	go func() {
		sweep(func(int) bool { return true }, 1, 100, time.Now().Add(50*time.Millisecond))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sweep still runs 5s after its deadline")
	}
}
