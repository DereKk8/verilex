package sandbox

import "time"

// sweep calls kill on every pid from 2 to max except self. A pass repeats while kill still
// reached a process, so a child forked during a pass ends in the next, and no pass starts after
// deadline, so a process that never goes cannot hold the sweep.
func sweep(kill func(pid int) bool, self, max int, deadline time.Time) {
	for reached := true; reached && time.Now().Before(deadline); {
		reached = false
		for pid := 2; pid <= max; pid++ {
			if pid != self && kill(pid) {
				reached = true
			}
		}
	}
}
