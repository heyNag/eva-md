package hook

import "syscall"

// getsid is the session id of the process pid, or of this one for 0. Go's
// syscall package has no Getsid on Linux, so it makes the system call.
func getsid(pid int) (int, error) {
	sid, _, errno := syscall.RawSyscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(sid), nil
}
