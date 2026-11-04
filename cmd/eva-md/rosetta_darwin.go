package main

import "syscall"

// rosettaTranslated reports whether Rosetta runs this process: an amd64
// binary on an Apple silicon Mac, as sysctl.proc_translated says.
func rosettaTranslated() bool {
	v, err := syscall.SysctlUint32("sysctl.proc_translated")
	return err == nil && v == 1
}
