//go:build darwin

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// born gives a file's creation time.
func born(info os.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Birthtimespec.Unix())
	}
	return info.ModTime()
}

// sysctl reads one node of the kernel's tables into a buffer of size bytes.
func sysctl(mib []int32, size int) ([]byte, error) {
	buf := make([]byte, size)
	n := uintptr(size)
	_, _, e := syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), 0, 0)
	if e != 0 {
		return nil, e
	}
	return buf[:n], nil
}

// parent reads a process from the process table: kern.proc.pid gives its
// start time (extern_proc's first field) and its name (p_comm, at byte
// 243), and kern.procargs2 its arguments.
func parent(pid int) (parentProc, error) {
	var p parentProc
	info, err := sysctl([]int32{1, 14, 1, int32(pid)}, 648)
	if err != nil {
		return p, err
	}
	if len(info) < 260 {
		return p, errors.New("short process record")
	}
	sec := int64(binary.LittleEndian.Uint64(info[0:8]))
	usec := int64(int32(binary.LittleEndian.Uint32(info[8:12])))
	p.born = time.Unix(sec, usec*1000)
	comm := info[243:260]
	if i := bytes.IndexByte(comm, 0); i >= 0 {
		comm = comm[:i]
	}
	p.name = filepath.Base(string(comm))
	if raw, err := sysctl([]int32{1, 49, int32(pid)}, 256<<10); err == nil && len(raw) > 4 {
		argc := int(binary.LittleEndian.Uint32(raw[:4]))
		rest := raw[4:]
		// The executable's path, then padding, then the arguments.
		if i := bytes.IndexByte(rest, 0); i >= 0 {
			rest = bytes.TrimLeft(rest[i:], "\x00")
		}
		var args []string
		for len(args) < argc && len(rest) > 0 {
			i := bytes.IndexByte(rest, 0)
			if i < 0 {
				i = len(rest)
			}
			args = append(args, string(rest[:i]))
			rest = rest[min(i+1, len(rest)):]
		}
		p.login = loginArgs(args)
	}
	return p, nil
}
