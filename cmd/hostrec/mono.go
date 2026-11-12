package main

import (
	"time"
	_ "unsafe" // for the monotonic clock's linkname
)

// nanotime is the system's monotonic clock, which every process on the
// machine reads alike: the recording orders hooks by it when the wall clock
// moves.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// parentProc is the hook's parent process: its name, whether it is a login
// shell, and when it was made.
type parentProc struct {
	name  string
	login bool
	born  time.Time
}

// loginArgs reports a shell started as a login shell: its argv[0] begins
// with "-", or it was given -l, --login or a group of short flags with l
// before its first word that is not a flag.
func loginArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if len(args[0]) > 1 && args[0][0] == '-' {
		return true
	}
	for _, a := range args[1:] {
		switch {
		case a == "--login" || a == "-l":
			return true
		case a == "--" || a == "" || a[0] != '-':
			return false
		case a[1] != '-':
			for _, r := range a[1:] {
				if r == 'l' {
					return true
				}
			}
		}
	}
	return false
}
