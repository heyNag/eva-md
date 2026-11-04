//go:build !linux

package hook

import "syscall"

// getsid is the session id of the process pid, or of this one for 0.
func getsid(pid int) (int, error) { return syscall.Getsid(pid) }
