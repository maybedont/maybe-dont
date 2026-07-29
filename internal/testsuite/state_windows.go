//go:build windows

package testsuite

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// acquireLock attempts to acquire an exclusive lock on the file using LockFileEx.
func acquireLock(f *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	handle := windows.Handle(f.Fd())

	// LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)

	for {
		// Lock the entire file (offset 0, length max)
		overlapped := &windows.Overlapped{}
		err := windows.LockFileEx(handle, flags, 0, 1, 0, overlapped)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for lock")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
