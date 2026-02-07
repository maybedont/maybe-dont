//go:build unix

package testsuite

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// acquireLock attempts to acquire an exclusive lock on the file using flock.
func acquireLock(f *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for lock")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
